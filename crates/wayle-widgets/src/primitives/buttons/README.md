# Button Templates

Button variants for actions and navigation.

## Available

| Template           | CSS Classes        | Use Case                  |
| ------------------ | ------------------ | ------------------------- |
| `PrimaryButton`    | `.primary`         | Main actions, CTAs        |
| `SecondaryButton`  | `.secondary`       | Secondary actions         |
| `DangerButton`     | `.danger`          | Destructive actions       |
| `GhostButton`      | `.ghost`           | Subtle actions, toolbars  |
| `GhostIconButton`  | `.ghost-icon`      | Icon-only ghost buttons   |
| `IconButton`       | `.icon`            | Icon-only with background |
| `LinkButton`       | `.link`            | Text links                |
| `MutedLinkButton`  | `.link .muted`     | De-emphasized links       |
| `DangerLinkButton` | `.link .danger`    | Destructive text links    |
| `MenuButton`       | `.menu`            | Dropdown menu trigger     |

## Import

```rust
use wayle_widgets::primitives::buttons::{
    PrimaryButton, SecondaryButton, GhostButton, LinkButton, LinkButtonClass,
};
```

## Link Button Modifiers

For `LinkButton`, use `LinkButtonClass` constants to apply modifiers:

| Constant                  | CSS Class | Effect                   |
| ------------------------- | --------- | ------------------------ |
| `LinkButtonClass::MUTED`  | `.muted`  | De-emphasized text color |
| `LinkButtonClass::DANGER` | `.danger` | Red destructive color    |

```rust
view! {
    #[template]
    LinkButton {
        add_css_class: LinkButtonClass::MUTED,
        #[template_child]
        label {
            set_label: "Cancel",
        },
    }
}
```

## Usage

### Text Button

```rust
view! {
    #[template]
    PrimaryButton {
        connect_clicked => Msg::Save,
        #[template_child]
        label {
            set_label: "Save",
        },
    }
}
```

### Icon + Text

```rust
view! {
    #[template]
    PrimaryButton {
        #[template_child]
        icon {
            set_visible: true,
            set_icon_name: Some("document-save-symbolic"),
        },
        #[template_child]
        label {
            set_label: "Save",
        },
    }
}
```

### Icon Only

```rust
view! {
    #[template]
    GhostIconButton {
        set_icon_name: Some("window-close-symbolic"),
    }
}
```

### Dynamic State

```rust
view! {
    #[template]
    PrimaryButton {
        #[watch]
        set_sensitive: !model.loading,
        #[template_child]
        label {
            #[watch]
            set_label: if model.loading { "Saving..." } else { "Save" },
        },
    }
}
```

## Template Children

Text buttons expose:

- **`icon`** - `gtk::Image`, hidden by default
- **`label`** - `gtk::Label`

`IconButton` and `GhostIconButton` use `set_icon_name` directly.
