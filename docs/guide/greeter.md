# Greeter (login screen)

`wayle-greeter` is a [greetd](https://sr.ht/~kennylevinsen/greetd/) greeter — a
display-manager login screen that can replace sddm/gdm for Wayland sessions. It
shares the wayle theme (colors, fonts) and has its own `[greeter]` config
section for appearance: background, clock, cursor, user list, and power
buttons — editable under **Greeter → Login Screen** in `wayle-settings`.

Features:

- **Session picker** — discovers Wayland sessions from
  `wayland-sessions/*.desktop` and X11 sessions from `xsessions/*.desktop` (the
  same files sddm/gdm read) and offers them in a dropdown. X11 entries are
  labelled `(X11)` and launched through `startx`. The last-used session is
  remembered and pre-selected.
- **User list** — real login accounts (from `/etc/passwd`) shown as clickable
  avatars; avatars come from AccountsService
  (`/var/lib/AccountsService/icons/<user>`) or `~/.face`, with an
  initial-letter fallback. Hidden when there are more than 8 users (the
  username field always works).
- **Remembered username** — the last successful username is pre-filled, with
  focus on the password field.
- **Caps Lock warning** — shown under the password entry while Caps Lock is on.
- **Power controls** — shutdown and reboot buttons.
- **Localized** — greeter labels follow the system locale (same Fluent-based
  i18n as the shell).

## Configuration

The greeter reads the `[greeter]` section of its config file (see below for
where that file lives per setup):

| Key | Default | What it does |
| --- | --- | --- |
| `background-mode` | `"color"` | `color`, `image`, or `wallpaper` (reuses `wallpaper.wallpaper`). |
| `background-image` | `""` | Image path for `background-mode = "image"`. |
| `background-color` | `"#000000"` | Fill color for `background-mode = "color"`. |
| `show-clock` | `true` | Show the clock above the login form. |
| `clock-format` / `date-format` | `"%H:%M"` / `"%A, %B %-d"` | strftime formats. |
| `show-user-list` | `true` | Clickable avatars for login users. |
| `show-power-buttons` | `true` | Shutdown/reboot buttons at the bottom. |
| `cursor-theme` | `""` (auto) | Xcursor theme. Empty = auto-detect (see below). |
| `cursor-size` | `24` (auto) | Logical cursor size; scaled per display, so HiDPI outputs get a sharp cursor (hotplug included). Unset = auto-detect. |

When `cursor-theme` / `cursor-size` are not set, the greeter auto-detects the
cursor the last logged-in user actually sees. The primary source is a small file
each running wayle shell writes at startup —
`~/.local/state/wayle/greeter-cursor`, recording the session's live
`XCURSOR_THEME` / `XCURSOR_SIZE` (size falling back to `HYPRCURSOR_SIZE`). That
is exactly what the compositor exported, so the login screen keeps the last
session's cursor even when it was set via env or `hyprcursor` rather than a
config file. If no record exists, the greeter falls back to reading dotfiles:
the Hyprland config (`env = XCURSOR_*` lines, `hyprctl setcursor`, following
`source =` includes), niri's `cursor` block, sway's `seat … xcursor_theme`, GTK
`settings.ini`, and `~/.icons/default/index.theme` — preferring the compositor
of the remembered session. Both are best-effort: they only work when that user's
home is readable by the greetd user (e.g. `chmod o+x` on `$HOME`); otherwise the
`XCURSOR_THEME`/`XCURSOR_SIZE` environment and finally the defaults apply.

These options live under **Greeter → Login Screen** in `wayle-settings`. The
greeter reads the *system* config (`/etc/wayle/config.toml` plus a
`runtime.toml` overlay beside it), not your user config. The **Apply to login
screen** button on that page pushes the current greeter values to the system via
`pkexec wayle-greeter apply-config` (bundled polkit action
`dev.stubbe.wayle.greeter.apply-config`, admin auth), writing only greeter keys
to `/etc/wayle/runtime.toml` — so any user can set the login-screen background
without hand-editing root-owned files, and a hand-written `config.toml` is never
clobbered.

## How it runs

greetd starts a kiosk compositor ([cage](https://github.com/cage-kiosk/cage))
whose single client is the greeter. On successful login greetd replaces the
greeter with your chosen session.

```
greetd → cage -s → wayle-greeter → (login) → your compositor
```

## NixOS

Enable it through the system module:

```nix
programs.wayle.greeter = {
  enable = true;
  # Theme it like your desktop (full wayle config schema; the greeter honours
  # styling, greeter.* and wallpaper).
  settings = {
    styling.appearance = "dark";
    greeter.background-mode = "color";
    greeter.background-color = "#1e1e2e";
    greeter.cursor-size = 24;
  };
};
```

Sessions are discovered from the aggregate NixOS sessions directory, which
contains the session files of every installed Wayland compositor. Options:

| Option | Default | What it does |
| --- | --- | --- |
| `greeter.session.dirs` | NixOS sessions dir | Directories scanned for Wayland `*.desktop` session files. |
| `greeter.session.x11Dirs` | NixOS xsessions dir | Directories scanned for X11 session files (launched via `startx`; `[]` disables). |
| `greeter.session.command` | `""` | Optional explicit fallback session, offered as a "Custom" entry. |
| `greeter.session.environment` | `[]` | Extra `KEY=value` entries for the started session. |
| `greeter.renderer` | `"auto"` | `auto` tries GPU and falls back to software; `software` forces the driver-free pixman + cairo path. |
| `greeter.graphicsWrapper` | `[]` | Command prefix (e.g. a nixGL wrapper) around the whole launch — for non-NixOS-style driver setups. |

The last session/username are stored in `/var/lib/wayle-greeter/`, owned by the
greetd user.

## Other distros (manual greetd)

Point greetd's `default_session` at the greeter, hosted by cage:

```toml
# /etc/greetd/config.toml
[default_session]
command = "cage -s -- wayle-greeter --config /etc/wayle/config.toml --state /var/lib/wayle-greeter/last-session"
user = "greeter"
```

- The greeter runs pre-login as the greetd user (no `$HOME`), so its theme
  config lives at a system path — `/etc/wayle/config.toml` by default.
- Create the state dir and make it writable by the greetd user:
  `install -d -o greeter -g greeter -m 0700 /var/lib/wayle-greeter`.
- Sessions are discovered from `/usr/local/share/wayland-sessions` and
  `/usr/share/wayland-sessions`; add `--sessions DIR` to override.
- An explicit fallback session can be appended: `-- sway` (offered as
  "Custom", and the only entry when no `.desktop` sessions exist).

With home-manager, `programs.wayle.greeter.enable` installs a
`wayle-greeter-session` wrapper (cage + greeter in one command) you can point
greetd at directly.

### Software rendering / nixGL

The login screen needs no GPU: with `renderer = "software"` (or manually
`WLR_RENDERER=pixman WLR_DRM_NO_MODIFIERS=1 GSK_RENDERER=cairo GDK_DISABLE=gl`)
the whole stack renders without any GPU driver — useful on VMs or hosts where GL
init is unreliable.

On non-NixOS hosts running the nix-built package, GPU acceleration needs
[nixGL](https://github.com/nix-community/nixGL) wrapping the whole launch:

```
nixGLIntel wayle-greeter-session --config /etc/wayle/config.toml --state /var/lib/wayle-greeter/last-session
```

## CLI reference

```
wayle-greeter [--config PATH] [--sessions DIR]... [--xsessions DIR]... [--state PATH] [--env KEY=VAL]... [-- <session argv...>]
```

| Flag | Default | What it does |
| --- | --- | --- |
| `--config PATH` | `/etc/wayle/config.toml` | Wayle config used for theming. |
| `--sessions DIR` | `/usr/{local/,}share/wayland-sessions` | Wayland session `.desktop` dir (repeatable; overrides defaults). |
| `--xsessions DIR` | `/usr/{local/,}share/xsessions` | X11 session `.desktop` dir (repeatable; launched via `startx`). |
| `--state PATH` | `$XDG_STATE_HOME/wayle-greeter/last-session` | File the last session id is remembered in (username in a `last-user` sibling). |
| `--env KEY=VAL` | — | Extra environment for the started session (repeatable). |
| `-- <argv...>` | — | Optional explicit fallback session ("Custom"). |
