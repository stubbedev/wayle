# Go rewrite

The `go-rewrite` branch carries wayle's port from Rust/GTK4 to Go on top
of [gelm](https://github.com/stubbedev/gelm), the pure-Go Wayland widget
kit (issues #18, #19). Direction of travel: the toolkit already exists;
this branch is the consumer side — port order bar → OSD/toasts →
launcher → lock screen → settings.

## Layout

| Go package | Ports | Notes |
| --- | --- | --- |
| `config` | wayle-config | One cfg-tag reflection layer derives loading, the runtime layer, `wayle config`, and the JSON Schema; schema_test holds every def to schema/wayle-config.schema.json. Same discovery, imports, YAML, and failure behavior as Rust: a bad value is a per-field diagnostic that keeps that field's default. |
| `strftime` | chrono strftime formatting | Format strings are validated at load; an unsupported specifier is an error, not garbage at runtime. |
| `styling` | wayle-styling | theme_css, the palette color math, the matugen/pywal/wallust providers, the user stylesheet (`internal/scss` compiles its SCSS subset), and the embedded compiled Rust stylesheet bundle (`just go-css` refreshes it). |
| `shell/*` | wayle-shell | bar, osd, popups, lock, credential (the lock/greeter prompt), screenshot, regionoverlay, colorpicker, sharepicker, wallpaper. |
| `greeter`, `cmd/wayle-greeter` | wayle-greeter | the greetd login screen, apply-config, cursor detection. |
| `cmd/wayle` | wayle bin | The full clap tree on `internal/cli` (help, errors, and completions byte-for-byte with clap). |
| `cmd/wayle-lock` | wayle-lock | One-token `wayle lock`. |

## Running

```sh
nix develop .#go -c just go-check   # fmt + vet + golangci-lint + test (pinned toolchain)
go run ./cmd/wayle shell            # the bar, from ~/.config/wayle/config.toml
```

The `.#go` devShell is the Go counterpart of the Rust devShell: `go_1_27`
pinned to the go.mod version (no toolchain downloads), the gelm lint stack,
fontconfig with the config's default fonts wired over the host fontconfig
(FONTCONFIG_FILE), sway for a headless compositor session, and dbus for the
private test buses. gelm is pure Go, so the shell sets `CGO_ENABLED=0` and
needs no native libraries (PAM goes through purego).

Lint enforcement is gelm's `.golangci.yml` verbatim (gci section
repointed), wired into the justfile as `go-lint` / `go-check`.

## Status

- [x] config: every root section of the Rust schema at its keys and
      defaults (oracle-tested), YAML and imports, the runtime layer
      (runtime.toml), hot reload (bars rebuild, OSD and popups take
      their sections), `wayle config get/set/reset/schema/default`
- [x] i18n: the Fluent runtime, locale negotiation, both embedded
      domains; bar labels, dropdowns, and the OSD brightness label
- [x] styling: theme_css with Rust goldens, providers, the user
      stylesheet, the compiled Rust stylesheet; the bar is the Rust
      BarButton tree styled by that bundle (the inset/border
      workarounds are gone); threshold colors through the bar-button
      variables
- [x] bar: every default-layout module plus dashboard, user-session,
      screenshot, media (stateful MPRIS service, icon modes, the player
      dropdown with transport controls), systray (full watcher,
      DBusMenu, IconPixmap), hyprsunset (solar auto-schedule over
      GeoClue, persisted override), volume icon-muted, popup
      hover-pause
- [x] workspaces: hyprland, sway, niri, mango
- [x] services: upower, brightness (sysfs, logind, DDC/CI, uevent
      hotplug), pulse (native protocol in pure Go, com.wayle.Audio1),
      mpris (com.wayle.Media1 on the bar's service), network (secret
      agent, native VPNs, openconnect sign-ins incl. GlobalProtect SAML,
      wifi controls), bluetooth (full BlueZ model, pairing agent),
      sysinfo, weather, mail (inotify), clipboard (data-control
      history), wallpaper (cycling, com.wayle.Wallpaper1, extractor),
      launcher engine and modes, auth (PAM via purego, greetd),
      shellipc (com.wayle.Shell1: bar visibility, Lock, VPN callback)
- [x] surfaces: lock screen (ext-session-lock, logind triggers,
      LockedHint recovery), screenshot + freeze-frame region overlay,
      color picker, share picker (com.wayle.SharePicker1), wallpaper
      layers with transitions
- [x] CLI: audio, idle, launcher (rofi flags and the rofi symlink, the
      socket lifetime contract), lock, media, notify, panel, power,
      recorder, screenshot, systray, toast, vpn, wallpaper, widget,
      shell; daily log files

## Remaining for feature parity

### Parked work (local branches)

- [x] `collect/launcher`: superseded by the landed launcher surface
- [x] `collect/icons`: `wayle icons` (every subcommand, replayed against
      the Rust binary), the usvg/tiny-skia-path port behind the symbolic
      transform (byte-identical to Rust over the four icon sets),
      migration, the shell's icon registry (search path, live refresh),
      and the mail provider icons
- [x] the icon SVG parser's XML layer is a roxmltree port: the same
      documents accepted, the same trees, the same error messages

### Bar

- [x] dashboard dropdown: quick actions, volume, now playing, battery,
      network, system rings, user session
- [x] calendar, mail, brightness, battery (with power profiles and
      the charge limit), audio (default devices, app volumes, device
      pickers), and notification dropdowns, section by section
- [x] recorder: the Rust engine (ScreenCast portal, in-process
      GStreamer through purego) and its dropdown
- [x] the dropdowns still thin stand-ins: treeman and network
- [x] notification history persistence: the Rust notifications.db,
      schema and zvariant hint JSON, read and written both ways; the
      raw hints ride on each notification (the sound, category and
      position hints are decoded by Rust and consumed nowhere)
- [x] notification popup cards: close and action buttons, the default
      action, the urgency bar, popup layer/margins/shadow keys (cards
      styled by the shell stylesheet; layers through shell/layering with
      tearing mode)
- [x] notification popup enter/exit animations (gelm Revealer through
      shell/reveal; a leaving card keeps its place until its exit lands)
- [x] format strings render through internal/jinja (minijinja-checked)
- [x] the custom module: icon-map/icon-names, color-map, class-format,
      tooltips, on-action, restart policies
- [x] cpu/ram/storage: frequencies, temperatures, and byte columns
- [x] systray: nested submenu popups (gelm OpenMenuPopover, the NESTED
      PopoverMenu)
- [x] systray: live accelerators (the menu's shortcuts fire while it is
      open), popovers that grow with their content (gelm reposition)
- [x] cava: the wave style and the stereo split, held frame for frame
      to the vendored cavacore.c (cava/testdata/oracle.c fixtures);
      `waves` and `monstercat` change nothing in either shell (only
      libcava's output stage reads them, which Rust never runs)
- [x] cava inputs: fifo and shmem (squeezelite) join pipe-wire and pulse,
      the set the Rust libcava build compiles in; the rest fail the module
      with the reason (Rust exits the shell over them)
- [x] network dialogs: secret prompt, wifi password, wifi list, the VPN
      rows and the VPN editor (typed and raw fields, WireGuard key
      generation and wg-quick import through the portal, inline delete
      confirm; a profile whose plugin is gone opens raw and keeps its type)
- [x] bluetooth: the pairing notifier without a placed module (started
      with the bar, as every Rust bar warms its bluetooth dropdown)
- [x] brightness dropdown: friendly names for DDC monitors (the Rust
      friendly_device_name; neither reads EDID model names)
- [x] weather: the three providers, retrying poll service, condition
      icons, the full dropdown; `[dropdowns.*]` sizes for every dropdown

### Config

- [x] `wayle config docs` (the VitePress page generator; byte-identical to the Rust pages)
- [x] hot reload beyond the bar/OSD/popups: wallpaper (wallpaper.Shell
      SetConfig) and the lock (read live, as the Rust lock does)
- [x] the launcher's hot reload: every session reads the live config

### Shell surfaces

- [x] the power menu (shell/power_menu): the dimmed overlay row the
      power module's `:menu` opens, entering and leaving through the
      power transition, commands run once it is gone (it was a stand-in
      dropdown)
- [x] launcher views: the surface on the launcher socket (dmenu and
      every mode, filtering, keys and pointer bindings, multi-select,
      -dump, -e dialogs, hooks, -select/-selected-row/-auto-select,
      sidebar tabs, -width/-location/-lines, session -font/-style CSS),
      styled by the shell stylesheet's launcher rules
- [x] greeter: the login state machine under test (prompts, OTP, failure
      and retry, stale conversations, remembered user and session), the
      card's fade, the configured palette, the greeter's own FTL domain
- [x] portal dialogs (shell/portaldialogs): access / account / app
      chooser / launcher / wallpaper, behind com.wayle.PortalDialogs1
- [x] the file chooser (shell/filechooser, com.wayle.FileChooser1):
      open / multiple / folder / save; places, mounts, breadcrumbs,
      history and thumb buttons, in-folder and streamed subfolder
      search, hidden files, type filters (GTK's class globs, MIME by
      shared-mime-info with subclasses), sort persisted with the
      list/grid layout, resizable columns, thumbnails, quick look and
      the preview pane, a movable and resizable sheet, file drops.
      Deliberate: a folder pick answers the selected folder (GTK), space
      in a text field types
- [x] the print dialog and its spooler (shell/printdialog, com.wayle.Print1):
      the printer list and settings form; jobs spool to CUPS over IPP
      (internal/ipp: CUPS-Get-Printers, Print-Job, the scheduler's domain
      socket or CUPS_SERVER; GtkPrintJob in Rust), GTK's Print to File
      writes output.pdf in the documents folder. The settings carry the
      printer, as GTK's dialog records it
- [x] the ScreenCast PipeWire producer (internal/pipewire, libpipewire
      through purego, the loop on a locked goroutine)
- [ ] the share-preview gbm dmabuf allocator (screencast streams SHM)
- [x] enter/exit animations for every ported transient surface (OSD and
      toasts, notification cards, dropdowns, the launcher, the lock card,
      the power menu; the wallpaper crossfades); the portal surfaces get
      theirs as they are ported. [animations]' CSS overrides reach the
      stylesheet
- [x] dropdown page stacks switch as Rust's do: audio, media and treeman
      slide at interaction-duration, network slides and weather crossfades at
      GTK's 200ms, the network hover swaps crossfade at 150ms
- [x] the bluetooth row's hover swap is a crossfading stack, as wide as
      its wider page
- [ ] the card's animated height between pages of different heights
      (dropdown_resize::animate_height)

### Binaries

- [ ] wayle-settings (the settings GUI; the largest single item)
- [x] xdg-desktop-portal-wayle (portal/): all 21 interfaces, the
      manifests pinned to them; godbus carried patched (fd arrays, struct
      variants, replied files; third_party/godbus-dbus/WAYLE-PATCHES.md);
      ConnectToEIS over a pure-Go EIS server held to libei; `wayle portal`,
      `portal run`, `portal share-picker` and every `portal show` dialog
      preview; with it no `wayle` subcommand is left unported

### Cross-cutting

- [x] a live compositor session smoke test (headless sway: start, three
      config reloads incl. a side bar and a bad value, no fatal exits)
- [x] `nix build .#wayle-go`: the Go shell built with the pinned
      toolchain, wrapped for its dlopens (libgstreamer, libglib, libpam,
      libpipewire)
      and the GStreamer plugin path; the bundled icons and the unit ship
      with it
- [ ] the NixOS/home-manager modules, the portal files and the systemd
      unit still point at the Rust package
- [x] go.mod pins gelm by its pushed commit (no local replace)
- [x] module lifetime: each bar is a mount generation; `follow` and
      `ModuleContext.Life` end subscriptions and tickers with it

## gelm gaps and the shortcuts they force

Each entry is a workaround wayle carries because gelm lacks the
capability. Remedy it in gelm, then delete the workaround in wayle.

| gelm lacks | wayle workaround (where) | remedy in gelm |
| --- | --- | --- |
| an fd/file watcher on the event loop | inotify goroutines + Invoke (internal/fswatch) | optional: `app.WatchFD` |

Closed in gelm since the inventory: the Revealer (every enter/exit
transition, through offscreen layers and affine composites), nested popovers (OpenMenuPopover), filled paths (Canvas.FillPath), popovers driven by the application loop (focused Entry input, loop-driven repaint, layer get_popup, rect-anchored placement, clicks and the wheel inside them), GTK-style give-way layout (a column shrinks its expanding Scroll; Scroll.VerticalOnly and SetMaxContentHeight), the CSS engine (var(), calc(),
color-mix(), :not(), structural selectors, the box model, per-side
padding and borders, rounded rings, gradients, box shadows), raw-pixel
icons, ext-session-lock, the capture protocols and dmabuf import,
data-control, Button.BgExplicit, menu row icons and MenuStack.

## Decisions

- **Audio speaks the PulseAudio native protocol in pure Go.**
  `service/pulse/native` is a focused client (protocol 32, no shared
  memory, servers from 24), so it serves PulseAudio and pipewire-pulse
  alike. It is used instead of github.com/jfreymuth/pulse, whose reader
  drops decode errors, reads past frame boundaries, and can panic on an
  empty blob. It is tested against `service/pulse/pulsetest`, a fake
  server speaking real frames. Like the Rust backend, it does not
  reconnect.
- **cava is a cavacore.c translation fed by the native client**: a
  record stream on the default sink's monitor (s16le mono, 44.1 kHz,
  512-frame fragments) that follows default-device changes.
- **One implementation of each shared piece**: `internal/dbuscli`
  (dbus.rs format_error, method vs property), `internal/dbustest`
  (private buses), `internal/dbusx` (zbus-style exports),
  `service/shellipc` (the only com.wayle.Shell1), `internal/desktopentry`
  (gio's key file and app list).

## Known deviations from the Rust shell

- The launcher's message line shows its markup's text, wrapped (gelm's
  wrapping label is plain); a Super+ key binding does not parse (gelm
  tracks no Super modifier).
- Workspace modules re-query the compositor on each relevant event
  instead of folding events into a local state; the rendered result is
  the same.
- Audio setters return server errors instead of firing and forgetting;
  the default device is looked up by name on every read.
- hyprsunset's solar math uses the correct longitude sign; Rust's
  solar.rs mirrors every schedule about Greenwich (fix there:
  `jStar = n + lW/360`).
- The clipboard history keeps recording after a restore (Rust stops for
  the session: its skip-own-offer slot is never cleared).
- Bluetooth refusals answer Rejected/Canceled rather than a generic
  Failed, and passkey entry sends a passkey (Rust sent it as a PIN).
- openconnect keeps cookies set on a redirect (Rust drops them, which
  breaks Juniper/Pulse gateways).
- A config reload rebuilds a custom module and restarts its command;
  the Rust module keeps a watch command running when only its look
  changed. The last output carries over, so the label does not blank.
- The weather dropdown's refresh and retry refetch with the current
  settings; Rust re-sets the location as a city, so coordinates were
  geocoded as a place name on retry.
- Screenshot frames decode by their real pixel format (24- and 10-bit
  included) and honor y-invert; Rust assumed XRGB.
