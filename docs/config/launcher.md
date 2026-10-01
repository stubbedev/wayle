---
title: launcher
outline: [2, 3]
---

# launcher

<div v-pre>

Application launcher / dmenu (rofi replacement).

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `location` | [`LauncherLocation`](/config/types#launcher-location) | `"center"` | Surface position on screen. |
| `width` | [`Size`](/config/types#size) | `1` | Surface width: a multiplier of the default 600px (`1.0` = default) or absolute pixels (e.g. `"800px"`). |
| `lines` | u32 | `10` | Visible result lines. |
| `monitor` | string | `""` | Output connector to show on ("" = focused output). |
| `modes` | array of string | `[...]` | Modes enabled by default (order = tab/kb-mode-next order). Script modes are referenced by their `[launcher.scripts]` key. |
| `cycle` | bool | `true` | Wrap selection at list edges. |
| `matching` | [`LauncherMatching`](/config/types#launcher-matching) | `"normal"` | Matching method. |
| `tokenize` | bool | `true` | Split the query into independently matched words. |
| `negate-char` | string | `"-"` | Token prefix that negates a token. |
| `normalize-match` | bool | `true` | Strip accents/normalize Unicode while matching. |
| `sort` | bool | `false` | Rank results by match quality (off = keep list order, rofi default). |
| `sorting-method` | [`LauncherSorting`](/config/types#launcher-sorting) | `"levenshtein"` | Ranking method when `sort` is on. |
| `case` | [`LauncherCase`](/config/types#launcher-case) | `"insensitive"` | Case handling. |
| `terminal` | string | `""` | Terminal emulator for terminal apps ("" = autodetect). |
| `show-icons` | bool | `true` | Show row icons. |
| `icon-theme` | string | `""` | Icon theme override ("" = system theme). |
| `sidebar-mode` | bool | `false` | Show mode tabs at the bottom of the surface. |
| `auto-select` | bool | `false` | Accept automatically when exactly one result remains. |
| `hover-select` | bool | `false` | Select the row under the mouse cursor. |
| `fixed-num-lines` | bool | `true` | Keep the list height fixed at `lines` rows. |
| `display-names` | map of string | `{}` | Per-mode display-name overrides (rofi `display-{mode}`), e.g. `drun = "apps"`. |
| `scripts` | map of string | `{}` | Custom script modes: name → executable (rofi `name:script`). |
| `keybindings` | map of string | `{}` | Keybinding overrides: action (rofi `kb-` name without the prefix, e.g. `accept`, `cancel`, `mode-next`, `custom-1`) → comma-separated key list (e.g. `"Control+Tab,F3"`). Unset actions keep rofi's defaults. |
| `mouse-bindings` | map of string | `{}` | Mouse binding overrides: action (rofi's `me-`/`ml-` name, prefix included, e.g. `me-accept-entry`, `ml-row-down`) → comma-separated button list (e.g. `"MouseDPrimary"`, `"ScrollDown"`). Unset actions keep their defaults. |
| `font` | string | `""` | Launcher font, as a Pango description (`"Inter 12"`). Empty keeps the shell's font. rofi's `-font` overrides it per invocation. |
| `styles` | map of string | `{}` | Named looks selectable per invocation with `-style <name>`: name → the CSS applied for that session, on top of `[styling]`. |
| `preview-cmd` | string | `""` | Command that turns a `thumbnail://` row icon into an image file: `{input}` the icon's path, `{output}` where to write the thumbnail, `{size}` the requested pixel size. Empty uses the system's XDG thumbnailers. rofi's `-preview-cmd`. |
| `history` | [`LauncherHistoryConfig`](/config/types#launcher-history-config) | `{...}` | Launch history / frecency. |
| `drun` | [`LauncherDrunConfig`](/config/types#launcher-drun-config) | `{...}` | drun (application) mode. |
| `run` | [`LauncherRunConfig`](/config/types#launcher-run-config) | `{...}` | run (command) mode. |
| `window` | [`LauncherWindowConfig`](/config/types#launcher-window-config) | `{...}` | window switcher mode. |
| `ssh` | [`LauncherSshConfig`](/config/types#launcher-ssh-config) | `{...}` | ssh mode. |
| `filebrowser` | [`LauncherFilebrowserConfig`](/config/types#launcher-filebrowser-config) | `{...}` | file browser mode. |
| `combi` | [`LauncherCombiConfig`](/config/types#launcher-combi-config) | `{...}` | combi (combined modes) mode. |

::: details More about `styles`

CSS rather than a second copy of the display keys: a preset exists to
say "this invocation looks different", and there is no list of the
ways someone might want that. `-style compact` then costs one config
entry instead of a schema change.

:::

## Default configuration

```toml
[launcher]
location = "center"
width = 1.0
lines = 10
monitor = ""
modes = [
    "drun",
    "run",
    "window",
]
cycle = true
matching = "normal"
tokenize = true
negate-char = "-"
normalize-match = true
sort = false
sorting-method = "levenshtein"
case = "insensitive"
terminal = ""
show-icons = true
icon-theme = ""
sidebar-mode = false
auto-select = false
hover-select = false
fixed-num-lines = true
font = ""
preview-cmd = ""

[launcher.display-names]

[launcher.scripts]

[launcher.keybindings]

[launcher.mouse-bindings]

[launcher.styles]

[launcher.history]
enable = true
max-size = 25

[launcher.drun]
categories = []
exclude-categories = []
match-fields = [
    "name",
    "generic",
    "exec",
    "categories",
    "keywords",
]
display-format = "{name} [<span weight='light' size='small'><i>({generic})</i></span>]"
show-actions = false
url-launcher = "xdg-open"

[launcher.run]
run-command = "{cmd}"
shell-command = "{terminal} -e {cmd}"
list-command = ""

[launcher.window]
format = "{w}   {c}   {t}"
match-fields = [
    "title",
    "class",
]
hide-active = false
close-on-delete = true

[launcher.ssh]
client = "ssh"
command = "{terminal} -e {ssh-client} {host}"
parse-hosts = false
parse-known-hosts = true

[launcher.filebrowser]
directory = ""
sorting-method = "name"
directories-first = true
show-hidden = false
command = ""

[launcher.combi]
modes = [
    "window",
    "drun",
    "run",
]
display-format = "{text}"
```


</div>
