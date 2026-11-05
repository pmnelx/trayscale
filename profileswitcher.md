# Trayscale Profile Switcher — Claude Code Spec

## Goal

Fork [DeedleFake/trayscale](https://github.com/DeedleFake/trayscale) and add a **profile switcher** to the GTK4/Libadwaita GUI. This lets the user switch between multiple Tailscale profiles (different control servers / tailnets) without dropping to the CLI.

## Context

Tailscale on Linux supports multiple profiles via `tailscale switch`. Each profile connects to a different control server (e.g. Tailscale cloud vs a self-hosted Headscale instance). Currently the only way to switch is via CLI:

```bash
sudo tailscale switch --list
# ID    Tailnet                  Account
# d7b3  itselfhelp.com           paul@itselfhelp.com
# 8d01  platform.mynitro.africa  tagged-devices*

sudo tailscale switch itselfhelp.com
```

Trayscale has no UI for this. We want to add one.

## Project tech stack

- **Language:** Go (1.24+)
- **UI framework:** GTK4 + Libadwaita (via gotk4 Go bindings)
- **Build:** `go mod` (module path: `deedles.dev/trayscale`)
- **Entry point:** `cmd/trayscale/main.go`
- **Internal packages:** `internal/` (contains tsutil, tray, ui packages)
- **Tailscale integration:** uses the Tailscale Go client library to communicate with `tailscaled` via the local API socket
- **License:** MIT

## Requirements

### 1. Fork and clone

- Fork `DeedleFake/trayscale` to my GitHub account
- Create a branch named `feature/profile-switcher`
- All work on this branch

### 2. Profile list and switch UI

Add a profile switcher to the main window. Location: in the header bar area or as a new section at the top of the sidebar/main view.

**UI elements needed:**

- A dropdown or list showing all available profiles from `tailscale switch --list`
- Each profile entry shows: Tailnet name, Account, and an indicator for the active profile (the one marked with `*`)
- Clicking/selecting a different profile triggers `tailscale switch <tailnet>` (requires root/operator permissions)
- Show a brief "Switching..." state indicator while the switch is in progress
- After a successful switch, refresh the entire UI to reflect the new tailnet's status (peers, routes, exit nodes)
- If the switch fails, show an error notification using Libadwaita's toast system

### 3. Implementation approach

**Data source:** Use the Tailscale LocalAPI to get profile information. The relevant endpoints are:
- `GET /localapi/v0/profiles/` — lists all profiles
- `PUT /localapi/v0/profiles/<profile-id>` — switches to a profile
- Or shell out to `tailscale switch --list` and `tailscale switch <name>` if the LocalAPI doesn't expose profiles directly

**Check how Trayscale currently communicates with tailscaled** — look in `internal/` for the existing pattern (likely uses `tailscale.com/client/tailscale` or the local socket). Follow the same pattern for profile operations.

**Permissions:** Profile switching requires operator or root access. Trayscale already handles this for other operations (start/stop, exit node selection). Follow the same permission pattern — the app should already have `sudo tailscale set --operator=$USER` configured.

### 4. System tray integration

If Trayscale has a system tray menu (right-click context menu on the tray icon):
- Add a "Switch Profile" submenu listing the available profiles
- The active profile should be indicated (checkmark or bold)
- Selecting a profile from the tray menu triggers the switch

### 5. Error handling

- If `tailscaled` is not running, disable the profile switcher
- If only one profile exists, show it but disable the dropdown (nothing to switch to)
- If the switch command fails (permissions, daemon error), show the error message in a toast notification
- Handle the brief disconnection during switch gracefully (the UI should not show error states for the ~2 second transition)

### 6. Do NOT change

- The existing UI layout and functionality — this is additive only
- The build system or module path
- The Flatpak configuration
- The license
- Any existing features or behavior

### 7. Build and test

```bash
# Build
go build ./cmd/trayscale

# Run (requires tailscaled running and operator set)
sudo tailscale set --operator=$USER
./trayscale
```

Verify:
- The profile switcher appears in the UI
- It lists all profiles from `tailscale switch --list`
- The active profile is indicated
- Switching works and the UI refreshes
- The tray icon menu includes the profile submenu (if tray is supported)

### 8. Push

- Commit with a clear message: "feat: add profile switcher for multiple tailnets"
- Push the `feature/profile-switcher` branch to my fork
- Do not open a PR against upstream (this is a personal fork with custom functionality)

## Dependencies

- Go 1.24+
- GTK4 development libraries (`libgtk-4-dev`)
- Libadwaita development libraries (`libadwaita-1-dev`)
- A running `tailscaled` with at least two profiles configured
- The current user set as operator: `sudo tailscale set --operator=$USER`

## Reference

- Trayscale source: https://github.com/DeedleFake/trayscale
- Tailscale switch docs: https://tailscale.com/kb/1141/switch/
- Tailscale LocalAPI: https://github.com/tailscale/tailscale/blob/main/client/tailscale/localclient.go
- gotk4 (GTK4 Go bindings): https://github.com/diamondburned/gotk4