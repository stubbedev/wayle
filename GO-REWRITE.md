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

State pins for picking the work back up (2026-10-09): wayle
`go-rewrite` on gelm `main` @ `f14e5c6`, both gates green
(`nix develop .#go -c just go-check` here; `just check` in
~/git/private/gelm). gelm's AT-SPI bridge ships in every build and
Run serves it while the desktop asks for assistive technologies
(`GELM_A11Y=atspi` forces it), so wayle carries no `atspi` tag. The
next work is the dropdown-parity list under "Remaining for feature
parity → Dropdown parity — still open", item 1 (the live pass; the
tooling for it now exists — see the pinned harness note there).
Every behavior lands with revert-provable tests (CLAUDE.md); when a
fix needs a capability gelm lacks, extend gelm first — nothing is on
the workaround list.
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

### Open (as of 2026-10-03)

**CSS engine completeness** — the compiled stylesheet now parses
through gelm's engine with zero warnings (`styling`
`TestStaticCSSParsesClean` fails naming anything the engine cannot
honor), and the engine is feature complete for animation and
transition work: gelm 8fe71f2 landed the full 2-D transform set
(matrix, translate, scale, rotate, skew and their single-axis forms,
lengths and all four angle units, composed left to right) painted
through the affine about a `transform-origin`, transitions over every
animatable channel (the colors, opacity, filter brightness, letter
spacing, padding, margin, border widths, min sizes, outline, corner
radii, and both transforms — one tween, layout channels relaying
per frame), keyframes over the same channels, and the animation
shorthand's complete grammar: comma lists, direction (normal, reverse,
alternate, alternate-reverse), fill mode (none, forwards, backwards,
both), negative delays starting mid-flight, steps() and the bezier
curves, iteration counts, and play-state. Transform keyframes
interpolate per function when the lists match, so a 0→360 spin turns
instead of standing still. What landed earlier for it: `@keyframes` +
`animation` + `animation-play-state` (the spins and pulses run),
`-gtk-icon-source` (`-gtk-icontheme()` names), `-gtk-icon-palette`,
`caret-color`, `text-decoration: underline`, and the earlier
transitions. Nothing is silently dropped: a property the SCSS starts
using that the engine cannot honor fails `TestStaticCSSParsesClean`
naming it.

**The one sanctioned deviation**: gelm's widget Dropdown closes with
its animated exit on a click anywhere outside it (the router's
press-away notice); the Rust combobox just closes.

**gelm b96e236 (2026-10-07): the bridge meets a real accessibility
bus** — putting gelm's AT-SPI bridge on at-spi2-core (not the test
fake) surfaced four bugs, each fixed with the fake upgraded to pin
it: the default bus name `org.a11y.atspi.gelm.<pid>` is invalid D-Bus
(an element may not lead with a digit) so Serve died on every real
desktop (`gelm.p<pid>` now); Socket.Embed was called at
/org/a11y/atspi/registry where the daemon only serves the Registry
interface instead of the desktop root where GTK calls it, so no app
ever entered the desktop tree; DoAction went straight to ClickAt, so
controls that act on the press (the bar toggle opens its dropdown in
SetPressed) did nothing when activated; and A11YOptions is now
exported beside the bridge so consumers outside gelm can name it
(wayle's tagged build passes the zero value).

**gelm f215943 (2026-10-03): the dropdown-parity toolkit batch** —
every capability the structural sweep found missing, each its own
commit with positive+negative tests: the hover pointer cursor on the
clickable primitives (Button, Switch, CheckButton, Slider carry
`cursorName "pointer"`, the way wayle-widgets sets it per control);
`widget.CenterBox` (gtk::CenterBox); `PopoverConfig.MaxHeight` (the
registry's monitor-100 ceiling); `Box.SetHomogeneous`;
`Entry.SetLeadingIcon` (GtkEntry's primary icon);
`KeyInterceptor` (an ancestor's key controller consulted before the
focused widget — the passkey row's handle\_pin\_key);
`Scroll.PropagateNaturalHeight`; `RichLabel` word wrap, max lines,
max-width-chars, and drag selection (the notification bodies'
wrapped styled markup, the VPN readout users copy); CSS
`background-image: url()` painted cover-fit inside the rounded
border (the dashboard avatar's inline url()); and
`app.FontVariantsOf`. wayle `7d7d4167` consumes all of it.

**wayle: stylesheet parity**

- [x] The bar dropdown panels' hand-matched colors moved onto the
      stylesheet. Landed: the popover carries the GTK class chain
      (`popover.dropdown.shadow.position-* > contents` around the
      `.dropdown` panel) and the theme sheet attaches to it —
      popovers are their own tree, so until now no CSS reached them
      and every panel hand-painted everything. The shared templates
      paint from the rules (header strip and title, empty states, the
      flat panel buttons through their classes' hover and the
      ghost/primary/danger primitives), and every panel migrated:
      audio and network (rows, active connections, password and
      secret cards, VPN list and form) first, then battery,
      brightness, calendar, weather, treeman, mail, media, dashboard,
      notification, recorder, and bluetooth (list and pairing).
      Nothing is hand-painted anymore — the last holdouts went over
      too: the battery gauge is gelm's LevelBar (levelbar > trough >
      block.filled, the node tree the gauge rules select), the
      dashboard rings read their ink and stroke from the cascade
      (CascadeColor/CascadeBorder; the success/warning/error class
      recolors), the bluetooth row is the styled box itself (the
      :hover rule, SetOnClickWithin and SetOnHoverWithin instead of
      the wrapper's custom paint), the webcam preview is two classed
      boxes (recorder-position-preview > recorder-position-cam), the
      treeman confirm page sits on the alert primitive (warning
      variant, danger accept), the weather nil-service fallback is the
      error-weather page, and the password peek is the entry's own
      trailing icon (entry > image, what the fg-subtle rule colors).
      `TestDropdownPanelsPaintFromTheStylesheet`, the per-panel paint
      tests, and the levelbar/ring paint tests pin it. Live
      comparison against the Rust shell per panel is the remaining
      visual pass.
- [x] Settings: the slider value label is fixed-width (gelm Label
      width-chars; the label floors at its widest rendering over the
      range, so a section's sliders end at the same x).

### Parked work (local branches)

- [x] `collect/launcher`: superseded by the landed launcher surface
- [x] `collect/icons`: `wayle icons` (every subcommand, replayed against
      the Rust binary), the usvg/tiny-skia-path port behind the symbolic
      transform (byte-identical to Rust over the four icon sets),
      migration, the shell's icon registry (search path, live refresh),
      and the mail provider icons
- [x] the icon SVG parser's XML layer is a roxmltree port: the same
      documents accepted, the same trees, the same error messages

### Dropdown parity — still open (found in the 2026-10-03 sweep)

The structural sweep matched every dropdown's widget tree, classes,
spacing model, and cursors against the Rust code, and the toolkit gaps
that blocked it closed in gelm (wrapping markup labels, selectable
labels, `background-image: url()`, the entry's primary icon,
homogeneous boxes, ancestor key interceptors, natural-height
propagation). The sweep's fixes landed as wayle `22eec8ef` (all 15
dropdowns, container, bar buttons) and `7d7d4167` (the primitives
wired in); both gates green. What remains, in the order to work it:

1. **Live visual comparison** — the only check no test covers. Run
   both shells side by side, panel by panel, and file every
   difference as a fix:

   ```sh
   systemctl --user stop wayle.service
   cd ~/git/private/wayle && nix develop .#go -c go run ./cmd/wayle shell
   ```

   then per panel open each dropdown in both shells and compare.
   When done, restore `systemctl --user start wayle.service` and
   tick this item off.

   **The 2026-10-07 pass set up the comparison rig** (nothing of the
   item itself is ticked yet):

   - Both shells run in a private kiosk — headless sway at
     2560x1440 in /tmp/parity-kiosk with its own D-Bus session bus
     and a11y stack (dbus-daemon at a11y/bus, an org.a11y.Bus
     stand-in at /tmp/parity/a11ybusd, at-spi2-registryd) — never on
     the live session. Config is a copy of the user's at
     /tmp/parity/xdg (layout extended to every panel module,
     `[modules.screenshot] notify=false` so captures don't cover the
     panels with their own toasts). PULSE_SERVER points at the real
     user server for both.
   - The shells' trees are read over AT-SPI: the Rust shell under
     `GTK_A11Y=atspi NO_AT_BRIDGE=0` (the devshell defaults them
     off), the Go shell under `GELM_A11Y=atspi` (built with
     `-tags gelmdebug`). /tmp/parity/a11ydump
     walks the registry's desktop and `a11ydump click #N` activates
     the Nth bar button through Action.DoAction (works on both:
     gelm b96e236 drives the router's press-release, GTK's menu
     buttons need the harness below instead).
   - The Rust reference is driven by a LOCAL, never-committed
     harness (GtkMenuButton ignores programmatic activation, so
     synthetic input was the only other route and the kiosk rejects
     it): /tmp/parity/rust-parity-harness.patch plus
     /tmp/parity/parity.rs (helpers/parity.rs) — set_active(true)
     per MenuButton with a marker/ack handshake in
     /tmp/parity/kiosk. /tmp/parity/drive.sh consumes it into
     /tmp/parity/shots/rust; /tmp/parity/drive-go.sh does the Go
     side through DoAction.
   - Captured so far (in /tmp/parity/shots, gone at reboot): 14
     Rust panels + 15 Go panels, each a PNG plus the bar's tree.
   - Closed since: bar buttons carry their label as the accessible
     name (gelm 7634ef3: embedded controls keep the derived name);
     keyboard-input follows sway, niri, and mango as
     the Rust sources do; the pulse "needs PULSE_SERVER" finding was
     the kiosk's private XDG_RUNTIME_DIR — libpulse (pactl) fails
     there identically — and the native client now tries libpulse's
     whole default list (PULSE_RUNTIME_PATH, the per-user socket,
     the system socket).
   - **Findings already on file from the trees** (fix with tests,
     then re-capture): bar geometry differs broadly
     (Rust center group 229px — clock 167 + 58 — against Go's 176 —
     123 + 53; the right group 542px against ~640), which needs the
     per-button/per-panel diff pass to split into font resolution,
     padding, and label-width causes.
   - Remaining for the item: pair panels by module (the Go trees
     carry no panel content — gelm popovers are not window roots —
     so the pairing is anchor-x from the bar trees plus the PNG
     crops), pixel-diff each pair's cropped panel region, and file
     every difference as a fix; then launcher/OSD/popups (CLI-driven:
     `wayle launcher`, volume/brightness changes, `wayle notify`/
     `toast`).
2. [x] **Dropdown instance caching** — the registry keeps one built
   instance per name (`dropdownRegistry.instance`): page, scroll,
   form state, and subscriptions persist across open and close, and
   the instance moves to whichever output opens it. Built on first
   open rather than warmed at startup (a first-open cost, no
   behavior difference); `dropdownOpened` carries the per-open work
   (the network list's scan-if-empty). A config change or a bars
   rebuild releases the instances.
3. **Registry behaviors** — same Rust file:
   - [x] `dropdown-freeze-label`: the anchor's label holds still
     while open (a stand-in copy; the module's updates land on thaw).
   - [x] animated dismissal on the transparent-area click (the panel
     plays the exit, as the re-click does).
   - [ ] `dropdown-autohide = false` (set_autohide's no-grab mode:
     only surface-empty click or re-click dismisses) — needs a gelm
     no-grab popover whose keys still reach it.
   - [ ] the card-height tween on stack page switches
     (dropdown\_resize.rs; only the network stack opts in today via
     interpolate-size).
4. [x] **Modal AlertDialog** for network VPN delete and treeman
   reset: gelm's modal Dialog (shell/bar/alertdialog.go), cancel the
   default and the Esc answer; the open dropdown closes first, as
   GTK's popover loses its grab to the dialog.
5. [x] **Icon-theme reload reaching dropdowns** — a config change
   releases the cached instances (an open one closes), so the next
   open builds with the new `icon-source`; Rust rebuilds an open
   dropdown in place.

Sweep methodology, for re-checking any panel: read the Rust tree
(`crates/wayle-bar-*/src/dropdowns/<name>/`, the shared templates in
`crates/wayle-widgets/src/primitives/`) against the Go file
(`shell/bar/<name>dropdown.go`) and assert structure, classes,
spacing-0 (gelm box gap ADDS the stylesheet's border-spacing —
gtkboxlayout.c `css_spacing + self->spacing` — so any box Rust
builds spacing-0 is spacing-0 here), valign sites (AppendAligned),
cursors, and i18n keys; every fix lands with a test that fails when
reverted.

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
- [x] zero-copy screencast (internal/gbm over libgbm, the pipewire
      producer's DmaBuf mode): a whole output screencopies into gbm
      buffers bound one per PipeWire buffer when the compositor offers a
      dmabuf target and the GPU allocates and imports it; the consumer
      that takes only the plain format gets producer-allocated memfds.
      Verified against a GLES headless sway; any failure stays on shm.
      24-bit shm formats now map to spa RGB/BGR (Rust offered them as
      BGRx)
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
- [x] the card's animated height between pages of different heights
      (dropdown_resize): a page stack sized to its visible page grows
      the card by its page's floor shortfall, tweened by the stack's
      interpolate-size; the panel reserves the neediest page up front
      and the transparent rest closes the dropdown

### Binaries

- [x] wayle-settings (cmd/wayle-settings, shell/settings): every page of
      the Rust app (50, in pages/nav.rs order) as data over one row and
      editor kit: the source badges and reset, every editor (enums,
      numbers, sliders, sizes, colors and color values, fonts, icons,
      files, actions, lists and maps, card lists, the theme selector,
      device selects, the TOML code view, the bar layout editor with its
      draggable chips), runtime.toml writes, the greeter apply
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
- [x] the NixOS/home-manager modules, the overlay and `packages.default`
      install the Go package (`wayle`, with wayle-greeter and wayle-lock,
      the portal and D-Bus files, the polkit action); the Rust shell
      stays buildable as `wayle-rust`; wayle-settings ships beside wayle
- [x] go.mod pins gelm by its pushed commit (no local replace)
- [x] module lifetime: each bar is a mount generation; `follow` and
      `ModuleContext.Life` end subscriptions and tickers with it

## gelm gaps and the shortcuts they force

Each entry is a workaround wayle carries because gelm lacks the
capability. Remedy it in gelm, then delete the workaround in wayle.

| gelm lacks | wayle workaround (where) | remedy in gelm |
| --- | --- | --- |
| (none open) | | |

The services' inotify watchers (internal/fswatch: brightness, mail,
icons) stay goroutine-side by design - they feed services, not the
loop; loop-side watches use app.WatchFiles.

Closed in gelm since the inventory: the Revealer (every enter/exit
transition, through offscreen layers and affine composites), nested popovers (OpenMenuPopover), filled paths (Canvas.FillPath), popovers driven by the application loop (focused Entry input, loop-driven repaint, layer get_popup, rect-anchored placement, clicks and the wheel inside them), GTK-style give-way layout (a column shrinks its expanding Scroll; Scroll.VerticalOnly and SetMaxContentHeight), the CSS engine (var(), calc(),
color-mix(), :not(), structural selectors, the box model, per-side
padding and borders, rounded rings, gradients, box shadows), raw-pixel
icons, ext-session-lock, the capture protocols and dmabuf import,
data-control, Button.BgExplicit, menu row icons and MenuStack, and
loop-delivered fd and file watches (app.WatchFD, app.WatchFiles: the
bar's user styles reload on change instead of a poll). For the
settings app: GtkSourceView's code view in TextArea (highlight.TOML,
line numbers, schemes), FlowBox, SpinButton, Paned, rich dropdown
rows and SetItems, GtkEntry's text node and width-chars, GtkScrolled-
Window's CSS box, scroll-to-focus and caret following, rows that
narrow any shrinkable child (height for width), popovers that resize
to the output, even-odd SVG fills, and HiDPI fixes (logical clips,
scaled glyph advances). Since then: the GTK node trees (row,
checkbutton > check, dropdown > button > arrow, expander > title >
arrow, notebook > header > tabs > tab, progressbar > trough >
progress, paned > separator, popover.menu with modelbutton rows, one
shared style-only part type), background-color transitions (the
timing-function solver in anim), Label width-chars, the wheel's
page^(2/3) step, and the Super+Tab exclusion from the Tab trap. The
2026-10-03 dropdown batch (gelm f215943): the hover pointer cursor
on Button/Switch/CheckButton/Slider, CenterBox,
PopoverConfig.MaxHeight, Box.SetHomogeneous, Entry.SetLeadingIcon,
KeyInterceptor (an ancestor's key controller before the focused
widget), Scroll.PropagateNaturalHeight, RichLabel wrap / max-lines /
max-width-chars / selectable, CSS background-image url() (cover-fit
in the rounded border), and app.FontVariantsOf. Nothing is on the
workaround list anymore; new toolkit needs go straight into gelm.

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
