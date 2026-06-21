# Bar Button

Configurable button component for shell panels with three visual variants.

## Variants

| Variant       | CSS Classes                | Description                               |
| ------------- | -------------------------- | ----------------------------------------- |
| `Basic`       | `.bar-button.basic`        | Icon and label, transparent background    |
| `BlockPrefix` | `.bar-button.block-prefix` | Colored icon block flush with button edge |
| `IconSquare`  | `.bar-button.icon-square`  | Colored icon square with button padding   |

## Import

```rust
use wayle_widgets::components::bar_buttons::{
    BarButton, BarButtonInit, BarButtonInput, BarButtonOutput,
    BarButtonBehavior, BarButtonClass, BarButtonColors, BarButtonVariant, BarSettings,
};
```

## Usage

### Basic

```rust
let bar_button = BarButton::builder()
    .launch(BarButtonInit {
        icon: "audio-volume-high-symbolic".into(),
        label: "100%".into(),
        tooltip: None,
        colors: BarButtonColors { /* ... */ },
        behavior: BarButtonBehavior { /* ... */ },
        settings: BarSettings { /* ... */ },
    })
    .forward(sender.input_sender(), |output| match output {
        BarButtonOutput::LeftClick => Msg::ToggleMute,
        BarButtonOutput::ScrollUp => Msg::VolumeUp,
        BarButtonOutput::ScrollDown => Msg::VolumeDown,
        _ => Msg::Noop,
    });
```

### With Variant

The variant is set via `settings.variant` (a `ConfigProperty<BarButtonVariant>`), not on the init struct directly.

```rust
let settings = BarSettings { /* ... */ };
settings.variant.set(BarButtonVariant::BlockPrefix);

BarButtonInit {
    icon: "network-wireless-symbolic".into(),
    label: "WiFi".into(),
    tooltip: None,
    colors: BarButtonColors { /* ... */ },
    behavior: BarButtonBehavior { /* ... */ },
    settings,
}
```

### Runtime Updates

```rust
bar_button.emit(BarButtonInput::SetIcon("audio-volume-muted-symbolic".into()));
bar_button.emit(BarButtonInput::SetLabel("Muted".into()));
bar_button.emit(BarButtonInput::SetTooltip(Some("Click to unmute".into())));
```

### Variant Switching

The variant switches reactively at runtime: update the `settings.variant` config property and the component's internal watcher rebuilds the button.

```rust
settings.variant.set(BarButtonVariant::IconSquare);
```

## Output Events

| Event         | Trigger             |
| ------------- | ------------------- |
| `LeftClick`   | Left mouse button   |
| `RightClick`  | Right mouse button  |
| `MiddleClick` | Middle mouse button |
| `ScrollUp`    | Scroll wheel up     |
| `ScrollDown`  | Scroll wheel down   |

## Configuration

All variants share these config properties:

| Property                    | Type                       | Carrier             | Description                          |
| --------------------------- | -------------------------- | ------------------- | ------------------------------------ |
| `show_icon`                 | `ConfigProperty<bool>`     | `BarButtonBehavior` | Show/hide icon                       |
| `show_label`                | `ConfigProperty<bool>`     | `BarButtonBehavior` | Show/hide label                      |
| `show_border`               | `ConfigProperty<bool>`     | `BarButtonBehavior` | Show/hide border                     |
| `visible`                   | `ConfigProperty<bool>`     | `BarButtonBehavior` | Show/hide entire button              |
| `label_max_chars`           | `ConfigProperty<u32>`      | `BarButtonBehavior` | Max chars before truncation (0=off)  |
| `icon_color`                | `ConfigProperty<ColorValue>` | `BarButtonColors` | Icon foreground color                |
| `label_color`               | `ConfigProperty<ColorValue>` | `BarButtonColors` | Label text color                     |
| `is_vertical`               | `ConfigProperty<bool>`     | `BarSettings`       | Vertical orientation                 |

Config properties are reactive - changes trigger automatic UI updates.

## CSS Classes

| Class                                       | Applied When         |
| ------------------------------------------- | -------------------- |
| `.bar-button`                               | Always (base)        |
| `.basic` / `.block-prefix` / `.icon-square` | Per variant          |
| `.icon-only`                                | Label hidden         |
| `.vertical`                                 | Vertical orientation |
