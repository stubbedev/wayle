# v0.8.34

Three open issues fixed, one deliberately left open. Tagged `v0.8.34`.

| Commit | Issue |
| --- | --- |
| `79acc644` `fix(file-chooser): filter in memory, real globs, streaming search` | closes #10 |
| `b7a262e8` `feat(launcher): per-invocation geometry overrides (-width, -lines, offsets)` | refs #8 |
| `38dd388f` `feat(network): native VPN support across NM, systemd units, and links` | closes #9 |
| `66989a97` `chore: run the test suite with nextest` | — |

---

## #10 — file chooser: search and the file-type filter

**Search is now a filter over the current folder, applied in memory.** It was a
full recursive tree walk re-run from scratch on every query change, delivering
results only once the walk finished — from `$HOME` that means traversing
`node_modules`, `.git` and `.cache` before anything appears on screen. That was
the stall.

- `Active` caches `all_entries` (the unfiltered listing) keyed by `listed_dir`,
  so only a directory change re-reads the filesystem. `populate` derives the
  visible rows by applying the hidden toggle, the file-type filter and the
  search text over that cached vector.
- `list_dir` / `build_search_entries` no longer apply the hidden or filter
  rules; those live in one place now.
- The 300 ms `search-delay` is gone — an in-memory scan needs no debounce.

**Recursive search survives as an explicit opt-in**: a "Search subfolders"
toggle next to the hidden-files toggle. The walk streams matches back in batches
of 64 as it finds them, so rows appear while it is still running instead of
arriving in one lump at the end. The generation counter still cancels a
superseded walk.

**The file-type filter matched nothing at all.** `matches_glob` was a three-case
toy matcher. GTK4's `gtk_file_filter_add_suffix` serialises for the portal as a
character-class glob — `*.png` arrives as `*.[pP][nN][gG]` — which that matcher
could never match, so every modern app's filter selected zero files and "All
Files" was the only working entry. Replaced with `glob::Pattern` (already a
dependency), matched case-insensitively. That also fixes literal `*.gif` against
`PHOTO.GIF`, and mid-pattern wildcards (`image_?.png`, `*.tar.*`) now work at
all.

Tests pin the exact GTK4 portal filter variant, the glob semantics, the text
filter, and the unfiltered `list_dir` contract.

---

## #9 — native VPN support

VPN state had to be bolted on from outside: a `[[modules.custom]]` block driving
a watch script that combined `inotify` on a marker file with `ip monitor link`.
wayle already spoke NetworkManager's VPN surface and then dropped it on the
floor — and NM-only would not have replaced that script anyway, because
openconnect-as-a-systemd-unit, `wg-quick@wg0` and `tailscaled` are all invisible
to NM.

A VPN entry now names **where its state comes from** rather than which VPN
software it is. That is what makes one mechanism cover every common type:

| Backend | State source | Control |
| --- | --- | --- |
| `networkmanager` | active-connection list | `ActivateConnection` / `DeactivateConnection` |
| `systemd` | unit `ActiveState` (with `Subscribe()`) | `StartUnit` / `StopUnit` |
| `link` | `/sys/class/net/<iface>/operstate` | read-only unless commands are set |

`networkmanager` covers OpenVPN, WireGuard, OpenConnect, L2TP, PPTP, Fortinet
and anything else with an NM plugin — the profile type never has to be known,
because a connection whose type is `vpn` or `wireguard` is a VPN and NM
activates it the same way regardless.

The other two combine on one entry: give a `systemd` entry an `interface` and
the unit supplies `connecting` while the link decides `connected`. `activating`
is a real connecting state, which is the one thing no interface can report —
during 2FA there is no tunnel yet. And the link is needed because a VPN process
can be alive while its tunnel is down (dropped session, resume from suspend).
That combination is exactly what the shell script was doing by hand.

`connect` / `disconnect` commands override the backend's own action, which is
what keeps an interactive openconnect 2FA helper working. A `connect-timeout`
drops an entry back out of `connecting` when nothing comes up, so a failed 2FA
cannot leave the indicator spinning forever.

Nothing polls. The link watcher rides NM's device events and re-reads sysfs on
each edge rather than opening an `AF_NETLINK` socket, since building a
`sockaddr_nl` needs `unsafe` and the workspace forbids it; NM is already a hard
dependency of this module, so nothing is lost.

Surfaced in both places: the module icon shows the folded state of every VPN
(`vpn-show = "auto"` keeps it invisible until one is configured), and the
dropdown gains a VPN section above the scan list with one row per entry and
click-to-toggle.

### Replacing the custom module

The `[[modules.custom]] id = "vpn"` block, its `icon-map`/`color-map`, and the
`vpn-watch` branch of the watcher script can all go, replaced by:

```toml
[[modules.network.vpn]]
id = "konform"
label = "Konform"
backend = "systemd"
unit = "openconnect-konform.service"
bus = "system"
interface = "oc-konform"
connect = "vpn-konform-connect"
disconnect = "vpn-konform-disconnect"
connect-timeout = 90
```

`wg-quick`, which needs no helper:

```toml
[[modules.network.vpn]]
id = "wg0"
backend = "systemd"
unit = "wg-quick@wg0.service"
interface = "wg0"
```

A NetworkManager profile needs nothing but its NM id:

```toml
[[modules.network.vpn]]
id = "work-openvpn"
```

Verified on screen, not just by reasoning: the shell was run on a nested
headless sway with these entries configured, the network dropdown rendered both
VPN rows with their state, and the bar icon switched to the VPN state icon.

---

## #8 — launcher (partial, issue left open)

`[launcher] width` / `lines` are global config, so two keybinds could not ask for
different geometry — the one gap that actually bites when replacing rofi, where
a wide `drun` and a default-width `window` switcher live side by side in the
same config.

- `-width` in all three rofi forms: `60` (percent of the monitor), `-30`
  (characters), `600px` (pixels).
- `-lines` as an alias of `-l`.
- `-xoffset` / `-yoffset`, applied as layer-shell margins on the anchored edges,
  signed so a positive x moves right regardless of which edge is anchored.
  Margins do nothing on a centered axis, so the CLI warns when offsets are
  passed with `-location 0` rather than silently ignoring them.
- `-theme` / `-theme-str` now warn that wayle has no rasi themes and point at
  `[styling]`, instead of the generic "not supported" line.

Percent and character widths cannot be resolved at config level, so `UiSettings`
carries the override and `apply_ui` resolves it late: percent against the monitor
the surface is on, characters against the entry font's advance measured with
pango.

The issue's own acceptance section said *"Everything except `-width` already
works today"* — that is met. **Deliberately not done**, and why the issue stays
open: `-preview-cmd`, the `-on-*` event hooks, mouse bindings, `-font`,
`-ignored-prefixes`, `recursivebrowser` / `windowcd`, the plugin ABI, and the
built-in calc / emoji / clipboard modes. That is a feature program, not a fix.

---

## Tooling

`just test` now runs `cargo nextest run --workspace --no-fail-fast` and forwards
extra args; the devShell provides `cargo-nextest` so it works without a local
install.

---

## #7 — dropdown popover animations: not done

Left open on purpose. Two reasons.

**1. The size animation has no consumer yet.** The fix means decoupling popover
measurement from its content (a `GtkOverlay` with a fixed-size spacer as the
main child and the card as a non-measuring overlay child) — a rewrite of the
shared layer *every* dropdown sits on. But no dropdown currently changes page
size: treeman and weather were made homogeneous on both axes precisely because
of this constraint. So the change alters measurement for all thirteen dropdowns
and produces no visible behaviour until each one adopts per-page sizes in a
follow-up.

**2. The outside-click exit cannot be hooked.** A mapped dropdown is an
`xdg_popup` holding an input grab, and the grab-driven dismissal gives no
pre-close callback — `GtkPopover::closed` fires when the surface is already
down. The only real fix is a transparent input-catching layer surface plus
`autohide = false`, which is a new surface with its own lifecycle and stacking
risks, and it changes keyboard-dismiss semantics too.

Shipping that unverified onto master immediately before a tag was the wrong
trade. The verification harness it needs does now exist and works — a nested
headless sway, a `wlr-virtual-pointer` click injector, and `grim` screenshots —
so a focused follow-up can do it properly and check the result on screen, which
is what the issue's acceptance criteria demand.
