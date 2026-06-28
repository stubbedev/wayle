# xdg-desktop-portal-hyprland integration

Wayle ships the screencast picker that the Hyprland portal (XDPH) shows when an
app asks to screen-share. The picker is the running shell's own layer-shell
surface, driven over D-Bus — no separate process or repo.

## Wiring the picker

XDPH execs its `custom_picker_binary` through `/bin/sh`, sets
`XDPH_WINDOW_SHARING_LIST`, and reads the chosen source from the binary's
stdout. Point it at the `wayle portal share-picker` subcommand:

`~/.config/hypr/xdph.conf`:

```ini
screencopy {
  custom_picker_binary = wayle portal share-picker
}
```

Use an absolute path if `wayle` is not on XDPH's `PATH` (it usually is not under
systemd):

```ini
screencopy {
  custom_picker_binary = /run/current-system/sw/bin/wayle portal share-picker
}
```

The stub forwards the request to the running shell over
`com.wayle.SharePicker1` (the shell registers it at startup), the shell pops up
the picker surface, and the selection is printed back as the
`[SELECTION].../...` line XDPH expects. Pass `--allow-token` to pre-check the
restore-token box:

```ini
custom_picker_binary = wayle portal share-picker --allow-token
```

The shell must be running (`wayle shell`) for the picker to appear; otherwise
the stub exits with an error and XDPH falls back to its own behaviour.

## Removing the second prompt for "share whole monitor" (Chrome)

When you pick **Entire screen** in Chrome, Chrome's in-page chooser only selects
the *source type*; the portal still runs the picker so you can choose *which*
output. With a single monitor that second step is pure friction.

Two layers remove it:

1. **Restore tokens (no patch).** Tick "Allow a restore token" the first time.
   Chrome stores it per-origin and XDPH replays the prior selection without
   prompting on later shares. First share still prompts.

2. **`auto-select-single-monitor.patch` (XDPH fork).** XDPH currently *ignores*
   the `types` field the client sends with `SelectSources`, so it can never
   tell that the request was monitor-only. The patch reads `types` and, when a
   new config option is enabled, auto-selects the sole output for monitor-only
   requests — skipping the picker entirely on the first share too.

   ```ini
   screencopy {
     auto_select_single_monitor = true
   }
   ```

   Caveat: this only fires when the client requests **monitors but not
   windows**. Modern Chrome narrows `types` to the chosen surface, so "Entire
   screen" qualifies; older clients that always request `MONITOR | WINDOW` will
   still prompt (no regression — the patch just does nothing for them). It also
   only fires with exactly one output; multi-monitor setups always prompt so
   you can pick which screen.

### Applying the patch with Nix

Overlay XDPH with the patch:

```nix
nixpkgs.overlays = [
  (final: prev: {
    xdg-desktop-portal-hyprland = prev.xdg-desktop-portal-hyprland.overrideAttrs (old: {
      patches = (old.patches or [ ]) ++ [
        ./contrib/xdph/auto-select-single-monitor.patch
      ];
    });
  })
];
```

The patch is generated against XDPH `v1.3.12`. Re-cut it from this directory's
diff if you track a newer tag.
