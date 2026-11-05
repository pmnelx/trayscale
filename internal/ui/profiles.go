package ui

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"
	"deedles.dev/trayscale/internal/gobj"
	"deedles.dev/trayscale/internal/gutil"
	"deedles.dev/trayscale/internal/locale"
	"deedles.dev/trayscale/internal/tsutil"
	"tailscale.com/ipn"
)

const (
	// profileSwitchTimeout limits the request to switch profiles.
	profileSwitchTimeout = 30 * time.Second

	// profileSettleTimeout limits how long the UI hides the daemon's
	// state after it has accepted a switch. A control server that can't
	// be reached leaves the daemon connecting indefinitely, and the UI
	// needs to show that eventually.
	profileSettleTimeout = 10 * time.Second

	// profileButtonChars is the width of the name on the dropdown's button.
	profileButtonChars = 16
)

// profileSwitch tracks a switch from the time it's requested until the
// daemon has settled on the new profile. While it's in progress, the
// app holds back status updates, as the daemon briefly looks offline
// during every switch.
type profileSwitch struct {
	target ipn.ProfileID
	// from is the state that the daemon was last seen in before the
	// switch.
	from ipn.State
	// returned is whether the daemon has accepted the switch.
	returned bool
	// reset is whether the daemon has been seen resetting for the switch.
	reset bool
	// held is the latest status held back.
	held  *tsutil.IPNStatus
	timer *time.Timer
}

func (s *profileSwitch) hold(status *tsutil.IPNStatus) {
	s.held = status
	if status.State == ipn.NoState {
		s.reset = true
	}
}

// settled reports whether the held status belongs to the new profile
// and is more than a step on the way to connecting. Bus updates race
// the response to the switch request, so a status that hasn't changed
// since before the switch may be left over from the old profile.
func (s *profileSwitch) settled() bool {
	if !s.returned || s.held == nil {
		return false
	}
	if !s.reset && s.held.State == s.from {
		return false
	}
	switch s.held.State {
	case ipn.NoState, ipn.Starting:
		return false
	}
	return true
}

// holdForProfileSwitch keeps status from the UI if a switch is in
// progress, releasing the latest one if status finishes the switch. It
// reports whether it took status.
func (a *App) holdForProfileSwitch(status *tsutil.IPNStatus) bool {
	s := a.profileSwitch
	if s == nil {
		return false
	}
	s.hold(status)
	if s.settled() {
		a.finishProfileSwitch(s)
	}
	return true
}

// switchProfile switches to the profile id unless it's already active
// or another switch is in progress. Its result shows up in the UI. It
// doesn't block, but it must be called from the main thread.
func (a *App) switchProfile(ctx context.Context, id ipn.ProfileID) {
	if a.profileSwitch != nil || id == a.activeProfile {
		return
	}

	s := &profileSwitch{target: id, from: a.ipnState}
	a.profileSwitch = s
	if a.win != nil {
		a.win.setProfileTarget(id)
	}

	do := a.switchProfileFunc
	if do == nil {
		do = switchDaemonProfile
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, profileSwitchTimeout)
		defer cancel()

		status, err := do(ctx, id)
		gutil.IdleAdd(func() { a.profileSwitchReturned(s, status, err) })
	}()
}

// switchDaemonProfile switches profiles and returns the resulting
// profile status, if available, so that the UI needn't wait for the next
// poll to show it.
func switchDaemonProfile(ctx context.Context, id ipn.ProfileID) (*tsutil.ProfileStatus, error) {
	err := tsutil.SwitchProfile(ctx, id)
	if err != nil {
		return nil, err
	}

	profile, profiles, err := tsutil.GetProfileStatus(ctx)
	if err != nil {
		// The switch worked and the poller will catch up.
		slog.Error("get profile status after switch", "err", err)
		return nil, nil
	}
	return &tsutil.ProfileStatus{Profile: profile, Profiles: profiles}, nil
}

// profileSwitchReturned handles the daemon's response to s.
func (a *App) profileSwitchReturned(s *profileSwitch, status *tsutil.ProfileStatus, err error) {
	if a.profileSwitch != s {
		return
	}

	if err != nil {
		slog.Error("switch profile", "id", s.target, "err", err)
		a.finishProfileSwitch(s)
		if a.win != nil {
			a.win.Toast(locale.Get("Failed to switch profile: %v", err))
		} else {
			a.notify(locale.Get("Switch account"), err.Error())
		}
		return
	}

	s.returned = true
	if status != nil {
		a.update(status)
	}
	if s.settled() {
		a.finishProfileSwitch(s)
		return
	}
	s.timer = time.AfterFunc(profileSettleTimeout, func() {
		gutil.IdleAdd(func() {
			if a.profileSwitch == s {
				a.finishProfileSwitch(s)
			}
		})
	})
}

// finishProfileSwitch ends s and shows the status that it held back.
func (a *App) finishProfileSwitch(s *profileSwitch) {
	a.profileSwitch = nil
	if s.timer != nil {
		s.timer.Stop()
	}
	if a.win != nil {
		a.win.setProfileTarget("")
	}
	if s.held != nil {
		a.update(s.held)
	}
}

// profileItem is one entry of the profile dropdown. Active is included
// so that a change of active profile rebinds the affected rows.
type profileItem struct {
	ID      ipn.ProfileID
	Tailnet string
	Account string
	Label   string
	Active  bool
}

func newProfileItem(p ipn.LoginProfile, active ipn.ProfileID) profileItem {
	return profileItem{
		ID:      p.ID,
		Tailnet: tsutil.ProfileTailnet(p),
		Account: tsutil.ProfileAccount(p),
		Label:   tsutil.ProfileLabel(p),
		Active:  p.ID == active,
	}
}

// title returns the item's name when there's only room for one.
func (item profileItem) title() string {
	return cmp.Or(item.Tailnet, item.Account, string(item.ID), locale.Get("Not connected"))
}

// subtitle returns the part of the item's name that title leaves out.
func (item profileItem) subtitle() string {
	if item.Tailnet == "" {
		return ""
	}
	return item.Account
}

func compareProfileItems(a, b profileItem) int {
	return cmp.Or(
		cmp.Compare(a.Tailnet, b.Tailnet),
		cmp.Compare(a.Account, b.Account),
		cmp.Compare(a.ID, b.ID),
	)
}

// profileSelector is the state of the main window's profile dropdown.
type profileSelector struct {
	list     *gobj.List[profileItem]
	all      []ipn.LoginProfile
	current  ipn.LoginProfile
	active   ipn.ProfileID
	target   ipn.ProfileID // The profile being switched to, if any.
	loaded   bool          // Whether the latest attempt to list profiles worked.
	updating bool          // Whether the selection is being set by the app.
}

// canSwitch reports whether there's a profile to switch to.
func (p *profileSelector) canSwitch() bool {
	if !p.loaded || p.target != "" {
		return false
	}
	return slices.ContainsFunc(p.all, func(profile ipn.LoginProfile) bool {
		return profile.ID != p.active
	})
}

func (win *MainWindow) initProfiles() {
	win.profiles.list = gobj.NewList(compareProfileItems)
	win.ProfileDropDown.SetModel(win.profiles.list.Model())

	button := newProfileFactory(false)
	win.ProfileDropDown.SetFactory(&button.ListItemFactory)
	button.Unref()

	popup := newProfileFactory(true)
	win.ProfileDropDown.SetListFactory(&popup.ListItemFactory)
	popup.Unref()

	gutil.ConnectNotify(win.ProfileDropDown, "selected-item", func() {
		if win.profiles.updating {
			return
		}

		sel := win.ProfileDropDown.GetSelectedItem()
		if sel == nil {
			return
		}
		defer sel.Unref()

		win.app.switchProfile(context.Background(), gobj.Item[profileItem](sel).ID)
		// If it didn't start a switch, the selection has to go back.
		win.syncProfiles()
	})

	win.syncProfiles()
}

func (win *MainWindow) updateProfiles(status *tsutil.ProfileStatus) {
	p := &win.profiles
	p.loaded = status.Err == nil
	if p.loaded {
		p.all = status.Profiles
		p.current = status.Profile
		p.active = status.Profile.ID
	}
	win.syncProfiles()
}

// setProfileTarget shows that a switch to id is in progress, or that
// none is if id is empty.
func (win *MainWindow) setProfileTarget(id ipn.ProfileID) {
	win.profiles.target = id
	win.syncProfiles()
}

// syncProfiles makes the widgets show the state in win.profiles.
func (win *MainWindow) syncProfiles() {
	p := &win.profiles
	p.updating = true
	defer func() { p.updating = false }()

	items := make([]profileItem, 0, len(p.all)+1)
	listed := false
	for _, profile := range p.all {
		items = append(items, newProfileItem(profile, p.active))
		listed = listed || profile.ID == p.active
	}
	// The daemon doesn't list a profile until it has logged in. A
	// dropdown can't show no selection, and it can't select what's already
	// selected, so without this the first profile would look active and
	// couldn't be switched to.
	if !listed && len(p.all) > 0 {
		items = append(items, newProfileItem(p.current, p.active))
	}
	p.list.Set(items)

	tooltip := ""
	if len(p.all) > 0 {
		tooltip = tsutil.ProfileLabel(p.current)
	}

	// The dropdown shows where the daemon is headed during a switch so
	// that it doesn't flip back when a poll sees the old profile.
	want := cmp.Or(p.target, p.active)
	var i uint32
	for item := range p.list.All() {
		if item.ID == want {
			win.ProfileDropDown.SetSelected(i)
			break
		}
		i++
	}

	win.ProfileDropDown.SetSensitive(p.canSwitch())
	if !p.loaded {
		tooltip = locale.Get("Profiles are unavailable")
	}
	win.ProfileDropDown.SetTooltipText(tooltip)

	page := "profiles"
	if p.target != "" {
		page = "switching"
	}
	win.ProfileStack.SetVisibleChildName(page)
}

// profileRow holds a list item's widgets, borrowed from the item.
type profileRow struct {
	title    *gtk.Label
	subtitle *gtk.Label
	check    *gtk.Image
}

func newProfileLabel() *gtk.Label {
	label := gtk.NewLabel("")
	label.SetXalign(0)
	return label
}

// newProfileFactory returns a new factory, owned by the caller, for
// profile dropdown rows. Rows in the popup show the whole profile and
// mark the active one, while the row on the button only has room for
// the title.
func newProfileFactory(popup bool) *gtk.SignalListItemFactory {
	rows := make(map[uintptr]*profileRow)

	f := gtk.NewSignalListItemFactory()
	gutil.Connect(f, f.ConnectSetup, func(_ gtk.SignalListItemFactory, p uintptr) {
		row := new(profileRow)
		rows[p] = row

		item := gtk.ListItemNewFromInternalPtr(p)
		row.title = newProfileLabel()
		if !popup {
			// Keep enough of the name to tell tailnets apart. An ellipsized
			// label would otherwise shrink to almost nothing in the header.
			row.title.SetEllipsize(pango.EllipsizeEndValue)
			row.title.SetWidthChars(profileButtonChars)
			row.title.SetMaxWidthChars(profileButtonChars)
			item.SetChild(&row.title.Widget)
			row.title.Unref()
			return
		}

		row.subtitle = newProfileLabel()
		row.subtitle.AddCssClass("caption")
		row.subtitle.AddCssClass("dim-label")

		names := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		names.SetHexpand(true)
		names.Append(&row.title.Widget)
		names.Append(&row.subtitle.Widget)
		row.title.Unref()
		row.subtitle.Unref()

		row.check = gtk.NewImageFromIconName("object-select-symbolic")
		row.check.SetTooltipText(locale.Get("Active"))

		box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		box.Append(&names.Widget)
		box.Append(&row.check.Widget)
		names.Unref()
		row.check.Unref()

		item.SetChild(&box.Widget)
		box.Unref()
	})
	gutil.Connect(f, f.ConnectBind, func(_ gtk.SignalListItemFactory, p uintptr) {
		obj := gtk.ListItemNewFromInternalPtr(p).GetItem()
		defer obj.Unref()
		item := gobj.Item[profileItem](obj)

		row := rows[p]
		row.title.SetText(item.title())
		if !popup {
			return
		}
		row.subtitle.SetText(item.subtitle())
		row.subtitle.SetVisible(item.subtitle() != "")
		row.check.SetVisible(item.Active)
	})
	gutil.Connect(f, f.ConnectTeardown, func(_ gtk.SignalListItemFactory, p uintptr) {
		delete(rows, p)
	})
	return f
}
