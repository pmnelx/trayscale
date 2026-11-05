package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"deedles.dev/trayscale/internal/tsutil"
	"tailscale.com/ipn"
)

func profileStatusState(state ipn.State) *tsutil.IPNStatus {
	return &tsutil.IPNStatus{State: state, Prefs: ipn.NewPrefs().View()}
}

func TestProfileSwitchSettled(t *testing.T) {
	tests := []struct {
		name     string
		from     ipn.State
		returned bool
		seen     []ipn.State
		want     bool
	}{
		{name: "nothing held", from: ipn.Running, returned: true},
		{name: "not accepted yet", from: ipn.Running, seen: []ipn.State{ipn.NoState, ipn.Running}},
		{name: "leftover from old profile", from: ipn.Running, returned: true, seen: []ipn.State{ipn.Running}},
		{name: "resetting", from: ipn.Running, returned: true, seen: []ipn.State{ipn.NoState}},
		{name: "connecting", from: ipn.Running, returned: true, seen: []ipn.State{ipn.NoState, ipn.Starting}},
		{name: "connected", from: ipn.Running, returned: true, seen: []ipn.State{ipn.NoState, ipn.Starting, ipn.Running}, want: true},
		{name: "needs login", from: ipn.Running, returned: true, seen: []ipn.State{ipn.NoState, ipn.NeedsLogin}, want: true},
		{name: "stopped profile", from: ipn.Running, returned: true, seen: []ipn.State{ipn.NoState, ipn.Stopped}, want: true},
		{name: "stopped to stopped", from: ipn.Stopped, returned: true, seen: []ipn.State{ipn.NoState, ipn.Stopped}, want: true},
		{name: "leftover stopped", from: ipn.Stopped, returned: true, seen: []ipn.State{ipn.Stopped}},
		{name: "no reset from reset state", from: ipn.NoState, returned: true, seen: []ipn.State{ipn.Running}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &profileSwitch{from: tt.from, returned: tt.returned}
			for _, state := range tt.seen {
				s.hold(profileStatusState(state))
			}
			if got := s.settled(); got != tt.want {
				t.Fatalf("settled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func testProfiles(ids ...ipn.ProfileID) []ipn.LoginProfile {
	profiles := make([]ipn.LoginProfile, len(ids))
	for i, id := range ids {
		profiles[i] = ipn.LoginProfile{
			ID:             id,
			Name:           "user@" + string(id),
			NetworkProfile: ipn.NetworkProfile{DomainName: string(id) + ".example"},
		}
	}
	return profiles
}

// profileTestApp returns an app with a window whose switches are
// answered by do, so that a test can't switch the real daemon's profile.
func profileTestApp(t *testing.T, do func(context.Context, ipn.ProfileID) (*tsutil.ProfileStatus, error)) *App {
	t.Helper()
	requireGTK(t)

	a := &App{operatorCheck: true, online: true, ipnState: ipn.Running, switchProfileFunc: do}
	a.app = adw.NewApplication("dev.deedles.Trayscale.test.profiles", gio.GApplicationNonUniqueValue)
	t.Cleanup(a.app.Unref)
	a.win = NewMainWindow(a)
	t.Cleanup(func() {
		a.win.MainWindow.Destroy()
		a.win.Widget().Unref()
	})
	return a
}

func profileStatus(active ipn.ProfileID, ids ...ipn.ProfileID) *tsutil.ProfileStatus {
	all := testProfiles(ids...)
	var profile ipn.LoginProfile
	for _, p := range all {
		if p.ID == active {
			profile = p
		}
	}
	profile.ID = active
	return &tsutil.ProfileStatus{Profile: profile, Profiles: all}
}

func selectedProfile(win *MainWindow) ipn.ProfileID {
	pos := win.ProfileDropDown.GetSelected()
	var i uint32
	for item := range win.profiles.list.All() {
		if i == pos {
			return item.ID
		}
		i++
	}
	return ""
}

func TestProfileDropDownStates(t *testing.T) {
	a := profileTestApp(t, nil)
	dd := a.win.ProfileDropDown

	if dd.GetSensitive() {
		t.Fatal("sensitive before any profiles are known")
	}

	a.update(profileStatus("a", "a"))
	if dd.GetSensitive() {
		t.Fatal("sensitive with one profile")
	}
	if got := selectedProfile(a.win); got != "a" {
		t.Fatalf("selected %q with one profile, want a", got)
	}

	a.update(profileStatus("b", "a", "b"))
	if !dd.GetSensitive() {
		t.Fatal("insensitive with two profiles")
	}
	if got := selectedProfile(a.win); got != "b" {
		t.Fatalf("selected %q, want b", got)
	}
	for item := range a.win.profiles.list.All() {
		if want := item.ID == "b"; item.Active != want {
			t.Fatalf("%q active = %v, want %v", item.ID, item.Active, want)
		}
	}

	a.update(&tsutil.ProfileStatus{Err: errors.New("tailscaled is down")})
	if dd.GetSensitive() {
		t.Fatal("sensitive while profiles can't be listed")
	}
	if got := selectedProfile(a.win); got != "b" {
		t.Fatalf("selected %q while down, want b", got)
	}

	a.update(profileStatus("b", "a", "b"))
	if !dd.GetSensitive() {
		t.Fatal("still insensitive after profiles came back")
	}

	// A new profile that hasn't logged in isn't listed yet, but the
	// others can still be switched to.
	a.update(profileStatus("new", "a"))
	if !dd.GetSensitive() {
		t.Fatal("insensitive with an unlisted active profile")
	}
	if got := selectedProfile(a.win); got != "new" {
		t.Fatalf("selected %q with an unlisted active profile, want new", got)
	}
	if got := a.win.profiles.list.Len(); got != 2 {
		t.Fatalf("%d rows with one listed profile and an unlisted active one, want 2", got)
	}

	// Without any profiles there's nothing to switch to or show.
	a.update(profileStatus("new"))
	if dd.GetSensitive() {
		t.Fatal("sensitive with no profiles")
	}
	if got := a.win.profiles.list.Len(); got != 0 {
		t.Fatalf("%d rows with no profiles, want 0", got)
	}
}

func TestProfileItemNames(t *testing.T) {
	tests := []struct {
		name            string
		item            profileItem
		title, subtitle string
	}{
		{"both", profileItem{Tailnet: "t", Account: "a"}, "t", "a"},
		{"account only", profileItem{Account: "a"}, "a", ""},
		{"id only", profileItem{ID: "i"}, "i", ""},
		{"not logged in", profileItem{}, "Not connected", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.title(); got != tt.title {
				t.Errorf("title() = %q, want %q", got, tt.title)
			}
			if got := tt.item.subtitle(); got != tt.subtitle {
				t.Errorf("subtitle() = %q, want %q", got, tt.subtitle)
			}
		})
	}
}

// selectProfile picks id the way that the user does, and waits for the
// fake switch to return.
func selectProfile(t *testing.T, a *App, id ipn.ProfileID, returned <-chan struct{}) {
	t.Helper()
	var i uint32
	for item := range a.win.profiles.list.All() {
		if item.ID == id {
			a.win.ProfileDropDown.SetSelected(i)
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Fatal("switch didn't run")
			}
			iterateFor(50 * time.Millisecond)
			return
		}
		i++
	}
	t.Fatalf("no profile %q", id)
}

func TestProfileSwitchHidesTransition(t *testing.T) {
	returned := make(chan struct{}, 1)
	var got []ipn.ProfileID
	a := profileTestApp(t, func(_ context.Context, id ipn.ProfileID) (*tsutil.ProfileStatus, error) {
		got = append(got, id)
		defer func() { returned <- struct{}{} }()
		return profileStatus(id, "a", "b"), nil
	})
	win := a.win
	a.update(profileStatus("a", "a", "b"))
	a.update(profileStatusState(ipn.Running))

	selectProfile(t, a, "b", returned)

	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("switched to %v, want [b]", got)
	}
	if a.profileSwitch == nil {
		t.Fatal("switch finished before the daemon settled")
	}
	if page := win.ProfileStack.GetVisibleChildName(); page != "switching" {
		t.Fatalf("visible page %q during switch", page)
	}
	if win.ProfileDropDown.GetSensitive() {
		t.Fatal("dropdown sensitive during switch")
	}
	if a.activeProfile != "b" {
		t.Fatalf("active profile %q after daemon accepted, want b", a.activeProfile)
	}

	for _, state := range []ipn.State{ipn.NoState, ipn.Starting} {
		a.update(profileStatusState(state))
		if !a.online {
			t.Fatalf("app went offline on %v during switch", state)
		}
		if win.ipn != nil && !win.ipn.Online() {
			t.Fatalf("window showed %v during switch", state)
		}
		if _, ok := win.pages["offline"]; ok {
			t.Fatalf("offline page added on %v during switch", state)
		}
	}

	a.update(profileStatusState(ipn.Running))
	if a.profileSwitch != nil {
		t.Fatal("switch still in progress after settling")
	}
	if page := win.ProfileStack.GetVisibleChildName(); page != "profiles" {
		t.Fatalf("visible page %q after switch", page)
	}
	if !win.ProfileDropDown.GetSensitive() {
		t.Fatal("dropdown insensitive after switch")
	}
	if got := selectedProfile(win); got != "b" {
		t.Fatalf("selected %q after switch, want b", got)
	}
	if win.ipn == nil || !win.ipn.Online() {
		t.Fatal("window didn't get the final status")
	}
}

func TestProfileSwitchToLoggedOutProfile(t *testing.T) {
	returned := make(chan struct{}, 1)
	a := profileTestApp(t, func(_ context.Context, id ipn.ProfileID) (*tsutil.ProfileStatus, error) {
		defer func() { returned <- struct{}{} }()
		return profileStatus(id, "a", "b"), nil
	})
	a.update(profileStatus("a", "a", "b"))
	a.update(profileStatusState(ipn.Running))

	selectProfile(t, a, "b", returned)
	a.update(profileStatusState(ipn.NoState))
	a.update(profileStatusState(ipn.NeedsLogin))

	if a.profileSwitch != nil {
		t.Fatal("switch still in progress after settling")
	}
	if a.online {
		t.Fatal("app still online for a logged-out profile")
	}
	if _, ok := a.win.pages["offline"]; !ok {
		t.Fatal("logged-out profile didn't show the offline page")
	}
}

func TestProfileSwitchFailure(t *testing.T) {
	returned := make(chan struct{}, 1)
	a := profileTestApp(t, func(context.Context, ipn.ProfileID) (*tsutil.ProfileStatus, error) {
		defer func() { returned <- struct{}{} }()
		return nil, errors.New("Access denied: profiles access denied <&>")
	})
	win := a.win
	a.update(profileStatus("a", "a", "b"))
	a.update(profileStatusState(ipn.Running))

	selectProfile(t, a, "b", returned)

	if a.profileSwitch != nil {
		t.Fatal("switch still in progress after failing")
	}
	if got := selectedProfile(win); got != "a" {
		t.Fatalf("selected %q after failure, want a", got)
	}
	if a.activeProfile != "a" {
		t.Fatalf("active profile %q after failure, want a", a.activeProfile)
	}
	if page := win.ProfileStack.GetVisibleChildName(); page != "profiles" {
		t.Fatalf("visible page %q after failure", page)
	}
	if !win.ProfileDropDown.GetSensitive() {
		t.Fatal("dropdown insensitive after failure")
	}

	// It didn't hold anything back from now on.
	a.update(profileStatusState(ipn.Stopped))
	if a.online {
		t.Fatal("status held back after the switch failed")
	}
}

func TestProfileUpdatesDontStartSwitches(t *testing.T) {
	calls := 0
	a := profileTestApp(t, func(context.Context, ipn.ProfileID) (*tsutil.ProfileStatus, error) {
		calls++
		return nil, nil
	})
	a.update(profileStatus("a", "a", "b", "c"))
	a.update(profileStatus("c", "a", "b", "c"))
	a.update(profileStatus("b", "b", "c"))
	a.update(&tsutil.ProfileStatus{Err: errors.New("down")})
	a.update(profileStatus("c", "a", "b", "c"))
	iterateFor(50 * time.Millisecond)
	if calls != 0 || a.profileSwitch != nil {
		t.Fatalf("status updates started %d switches", calls)
	}
}

func TestFinishedProfileSwitchIgnoresLateResponse(t *testing.T) {
	s := &profileSwitch{from: ipn.Running, returned: true}
	a := &App{profileSwitch: s}
	a.finishProfileSwitch(s)
	if a.profileSwitch != nil {
		t.Fatal("finish left the switch in progress")
	}
	// A late timer or response for a finished switch does nothing.
	a.profileSwitchReturned(s, nil, errors.New("late"))
}

func TestRestartDaemon(t *testing.T) {
	for _, want := range []error{nil, errors.New("Authentication failed <&>")} {
		release := make(chan struct{})
		calls := 0
		a := profileTestApp(t, nil)
		a.restartDaemonFunc = func(context.Context) error {
			calls++
			<-release
			return want
		}

		a.restartDaemon(t.Context())
		a.restartDaemon(t.Context()) // Ignored, as one is in progress.
		iterateFor(50 * time.Millisecond)
		if !a.restartingDaemon || !a.win.WorkSpinner.GetVisible() {
			t.Fatal("restart doesn't show as in progress")
		}

		close(release)
		iterateFor(50 * time.Millisecond)
		if calls != 1 {
			t.Fatalf("restarted %d times, want 1", calls)
		}
		if a.restartingDaemon || a.win.WorkSpinner.GetVisible() {
			t.Fatal("restart still shows as in progress")
		}
	}
}
