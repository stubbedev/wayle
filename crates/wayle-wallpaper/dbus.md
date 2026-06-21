# Wallpaper D-Bus Interface

Control wallpapers, cycling, and theming.

- **Service:** `com.wayle.Wallpaper1`
- **Path:** `/com/wayle/Wallpaper`

## Monitor Targeting

Methods accepting `monitor`:

- **Empty string `""`** - Targets all monitors
- **Monitor name** - Targets a specific monitor (e.g., "DP-1", "HDMI-A-1")

## Methods

### Wallpaper Control

| Method                | Arguments           | Returns | Description                      |
| --------------------- | ------------------- | ------- | -------------------------------- |
| `SetWallpaper`        | `s path, s monitor` | -       | Set wallpaper from file path     |
| `SetFitMode`          | `s mode, s monitor` | -       | Set scaling mode                 |
| `WallpaperForMonitor` | `s monitor`         | `s`     | Get wallpaper path for a monitor |
| `GetFitMode`          | `s monitor`         | `s`     | Get fit mode for a monitor       |

Fit modes: `fill`, `fit`, `center`, `stretch`

### Cycling Control

| Method         | Arguments                              | Returns | Description                |
| -------------- | -------------------------------------- | ------- | -------------------------- |
| `StartCycling` | `s directory, u interval_secs, s mode` | -       | Start cycling wallpapers   |
| `StopCycling`  | -                                      | -       | Stop wallpaper cycling     |
| `Next`         | -                                      | -       | Advance to next wallpaper  |
| `Previous`     | -                                      | -       | Go back to previous        |
| `GetIsCycling` | -                                      | `b`     | Check if cycling is active |

Cycling modes: `sequential`, `random`

### Theming

| Method              | Arguments   | Returns | Description                           |
| ------------------- | ----------- | ------- | ------------------------------------- |
| `ExtractColors`     | -           | -       | Extract colors from current wallpaper |
| `SetThemingMonitor` | `s monitor` | -       | Set monitor for color extraction      |

### Monitor Management

| Method              | Arguments   | Returns | Description              |
| ------------------- | ----------- | ------- | ------------------------ |
| `ListMonitors`      | -           | `as`    | List registered monitors |
| `RegisterMonitor`   | `s monitor` | -       | Register a monitor       |
| `UnregisterMonitor` | `s monitor` | -       | Unregister a monitor     |

## Properties

| Property         | Type | Access | Description                       |
| ---------------- | ---- | ------ | --------------------------------- |
| `IsCycling`      | `b`  | read   | Whether cycling is active         |
| `ThemingMonitor` | `s`  | read   | Monitor used for color extraction |

## Signals

| Signal            | Arguments | Description                       |
| ----------------- | --------- | --------------------------------- |
| `ColorsExtracted` | -         | Emitted when color extraction finishes |
