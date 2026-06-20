# Wayle xdg-desktop-portal backend

Wayle ships its own `xdg-desktop-portal` backend (`wayle portal`), so screen
sharing, screenshots, remote control, global shortcuts, appearance settings and
more work on **any** Wayland compositor — niri, mango, Hyprland, sway, river —
not just the one whose portal happens to be installed.

Unlike `wayle share-picker` (an xdg-desktop-portal-hyprland *plugin* that only
runs under Hyprland), this backend plugs into the compositor-independent
`xdg-desktop-portal` **frontend**, which routes every app's portal request to
whichever backend `portals.conf` selects.

## Interfaces

Implemented natively by `wayle portal`:

| Interface | Backed by |
|---|---|
| `ScreenCast` | native PipeWire producer + wlr-screencopy / ext-image-copy capture |
| `RemoteDesktop` | `zwlr_virtual_pointer_v1` + `zwp_virtual_keyboard_v1` |
| `Screenshot` (+ PickColor) | the shell's `com.wayle.Screenshot1` overlay |
| `GlobalShortcuts` | `hyprland-global-shortcuts-v1` (where the compositor implements it) |
| `Settings` | `wayle-config` (`org.freedesktop.appearance`: color-scheme, accent, contrast) |
| `Notification` | the shell's notification daemon |
| `Wallpaper` | `com.wayle.Wallpaper1` |
| `Inhibit` | a systemd-logind inhibitor lock |
| `Lockdown` | static policy (nothing locked down) |

Generic desktop-independent dialogs — `FileChooser`, `Print`, `Account`,
`AppChooser`, `Email`, and the permission `Access` dialog — are **delegated to
`xdg-desktop-portal-gtk`** via `portals.conf`. This is the standard wlroots-world
arrangement (xdg-desktop-portal-hyprland delegates everything except three
interfaces too); reimplementing GTK's file/print dialogs would duplicate it for
no gain.

## Installing (non-Nix)

```sh
# 1. The backend binary is just `wayle portal`; make sure `wayle` is on PATH.
# 2. Declare the backend + its interfaces to the frontend:
install -Dm644 resources/wayle.portal \
  /usr/share/xdg-desktop-portal/portals/wayle.portal
# 3. Let the frontend D-Bus-activate it (edit Exec= if wayle is elsewhere):
install -Dm644 resources/org.freedesktop.impl.portal.desktop.wayle.service \
  /usr/share/dbus-1/services/org.freedesktop.impl.portal.desktop.wayle.service
# 4. Route interfaces to wayle (and the dialog tier to gtk):
install -Dm644 resources/wayle-portals.conf \
  /usr/share/xdg-desktop-portal/wayle-portals.conf
```

Then start a session with `XDG_CURRENT_DESKTOP=wayle` (so the frontend reads
`wayle-portals.conf`), restart `xdg-desktop-portal`, and confirm:

```sh
busctl --user introspect org.freedesktop.impl.portal.desktop.wayle \
  /org/freedesktop/portal/desktop
```

The shell (`wayle shell`) must be running for the interfaces that delegate to it
(Screenshot, Wallpaper, Notification, and the ScreenCast picker).

## Installing (Nix)

`nix/package.nix` installs `wayle.portal`, the D-Bus activation file (with the
store path substituted in), and reference copies of the systemd unit and
`wayle-portals.conf` under `$out/share/wayle`. Enable the backend through the
NixOS / home-manager module alongside the shell.

## Compositor notes

- **ScreenCast / Screenshot / RemoteDesktop / Settings / Notification /
  Wallpaper / Inhibit**: work on any compositor implementing the standard
  capture (`ext-image-copy-capture` or `wlr-screencopy`) and virtual-input
  (`wlr-virtual-pointer`, `virtual-keyboard`) protocols.
- **GlobalShortcuts**: requires `hyprland-global-shortcuts-v1`. Compositors that
  do not implement it (currently niri, mango) accept binds but never deliver
  activations — there is no portal-agnostic global-shortcut mechanism.
