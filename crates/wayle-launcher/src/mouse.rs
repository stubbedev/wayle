//! rofi mouse bindings: `me-*` buttons and `ml-*` scroll.
//!
//! Parsing only — no GTK. The surface turns a [`MouseBinding`] into gesture
//! and scroll controllers; keeping the syntax here is what lets it be tested
//! without a display.
//!
//! The identifier shape is rofi's (`rofi-keys(5)`): `Mouse<D><Button>`, where
//! `D` means a double press and `Button` is one of `Primary`, `Secondary`,
//! `Middle`, `Forward`, `Back` or `ExtraN`; and `Scroll<Up|Down|Left|Right>`.
//! Both take the same `Control+`/`Shift+`/`Alt+`/`Super+` prefixes as keys.

use bitflags::bitflags;

bitflags! {
    /// Modifiers held with a mouse binding.
    #[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
    pub struct MouseModifiers: u8 {
        /// Control.
        const CONTROL = 1 << 0;
        /// Shift.
        const SHIFT = 1 << 1;
        /// Alt (rofi `Mod1`).
        const ALT = 1 << 2;
        /// Super (rofi `Mod4`).
        const SUPER = 1 << 3;
    }
}

/// Which physical button.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MouseButton {
    /// Left, on a right-handed mouse.
    Primary,
    /// Right.
    Secondary,
    /// Middle / wheel click.
    Middle,
    /// The forward side button.
    Forward,
    /// The back side button.
    Back,
    /// `ExtraN`, as reported by the pointer.
    Extra(u8),
}

impl MouseButton {
    /// The button number GDK reports for this button.
    ///
    /// 1/2/3 are primary/middle/secondary; 8 and 9 are back and forward on
    /// every pointer that has them. `ExtraN` continues past those.
    #[must_use]
    pub const fn number(self) -> u32 {
        match self {
            Self::Primary => 1,
            Self::Middle => 2,
            Self::Secondary => 3,
            Self::Back => 8,
            Self::Forward => 9,
            Self::Extra(n) => n as u32,
        }
    }
}

/// Which way the wheel turned.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ScrollDirection {
    /// Wheel up.
    Up,
    /// Wheel down.
    Down,
    /// Wheel (or tilt) left.
    Left,
    /// Wheel right.
    Right,
}

/// The pointer event a binding is on.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MouseInput {
    /// A button press, single or double.
    Click {
        /// Which button.
        button: MouseButton,
        /// Whether it takes two presses.
        double: bool,
    },
    /// A scroll in one direction.
    Scroll(ScrollDirection),
}

/// One parsed binding: modifiers plus the pointer event.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MouseBinding {
    /// Modifiers that must be held.
    pub modifiers: MouseModifiers,
    /// The event itself.
    pub input: MouseInput,
}

/// Parses one rofi mouse identifier, with its modifier prefixes.
///
/// `None` for anything that is not one — including a *key* name, since the
/// two binding tables are read from the same kind of config value and a
/// mistyped `Return` should be reported rather than bound to a button.
#[must_use]
pub fn parse(spec: &str) -> Option<MouseBinding> {
    let mut modifiers = MouseModifiers::empty();
    let mut input = None;

    for part in spec.split('+') {
        let part = part.trim();
        match part {
            "Control" | "Ctrl" => modifiers |= MouseModifiers::CONTROL,
            "Shift" => modifiers |= MouseModifiers::SHIFT,
            "Alt" | "Mod1" => modifiers |= MouseModifiers::ALT,
            "Super" | "Mod4" => modifiers |= MouseModifiers::SUPER,
            name => input = Some(parse_input(name)?),
        }
    }

    Some(MouseBinding {
        modifiers,
        input: input?,
    })
}

fn parse_input(name: &str) -> Option<MouseInput> {
    if let Some(direction) = name.strip_prefix("Scroll") {
        return Some(MouseInput::Scroll(match direction {
            "Up" => ScrollDirection::Up,
            "Down" => ScrollDirection::Down,
            "Left" => ScrollDirection::Left,
            "Right" => ScrollDirection::Right,
            _ => return None,
        }));
    }

    let rest = name.strip_prefix("Mouse")?;
    // `MouseDPrimary` is a double press; `MousePrimary` a single one. The `D`
    // is only a double marker when a button name follows it, or `MouseD…`
    // would swallow a button that legitimately starts with one.
    let (double, button) = match rest.strip_prefix('D') {
        Some(button) if !button.is_empty() => (true, button),
        _ => (false, rest),
    };
    Some(MouseInput::Click {
        button: parse_button(button)?,
        double,
    })
}

fn parse_button(name: &str) -> Option<MouseButton> {
    Some(match name {
        "Primary" => MouseButton::Primary,
        "Secondary" => MouseButton::Secondary,
        "Middle" => MouseButton::Middle,
        "Forward" => MouseButton::Forward,
        "Back" => MouseButton::Back,
        extra => MouseButton::Extra(extra.strip_prefix("Extra")?.parse().ok()?),
    })
}

/// Parses a comma-separated binding list, dropping (and reporting) the parts
/// that are not mouse identifiers.
#[must_use]
pub fn parse_list(specs: &str) -> Vec<MouseBinding> {
    specs
        .split(',')
        .map(str::trim)
        .filter(|spec| !spec.is_empty())
        .filter_map(|spec| match parse(spec) {
            Some(binding) => Some(binding),
            None => {
                tracing::warn!(binding = %spec, "unparseable launcher mouse binding");
                None
            }
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn click(button: MouseButton, double: bool) -> MouseInput {
        MouseInput::Click { button, double }
    }

    #[test]
    fn a_bare_button_is_a_single_press_with_no_modifiers() {
        let binding = parse("MousePrimary").unwrap();
        assert_eq!(binding.input, click(MouseButton::Primary, false));
        assert_eq!(binding.modifiers, MouseModifiers::empty());
    }

    #[test]
    fn the_d_prefix_is_a_double_press() {
        assert_eq!(
            parse("MouseDPrimary").unwrap().input,
            click(MouseButton::Primary, true)
        );
        // rofi's own default for accept-custom.
        let binding = parse("Control+MouseDPrimary").unwrap();
        assert_eq!(binding.input, click(MouseButton::Primary, true));
        assert_eq!(binding.modifiers, MouseModifiers::CONTROL);
    }

    #[test]
    fn every_button_name_rofi_documents_parses() {
        for (name, button) in [
            ("MousePrimary", MouseButton::Primary),
            ("MouseSecondary", MouseButton::Secondary),
            ("MouseMiddle", MouseButton::Middle),
            ("MouseForward", MouseButton::Forward),
            ("MouseBack", MouseButton::Back),
            ("MouseExtra7", MouseButton::Extra(7)),
        ] {
            assert_eq!(parse(name).unwrap().input, click(button, false), "{name}");
        }
    }

    #[test]
    fn scroll_directions_parse() {
        for (name, direction) in [
            ("ScrollUp", ScrollDirection::Up),
            ("ScrollDown", ScrollDirection::Down),
            ("ScrollLeft", ScrollDirection::Left),
            ("ScrollRight", ScrollDirection::Right),
        ] {
            assert_eq!(
                parse(name).unwrap().input,
                MouseInput::Scroll(direction),
                "{name}"
            );
        }
    }

    #[test]
    fn a_key_name_is_not_a_mouse_binding() {
        // The two tables share a config shape, so a key in the mouse table
        // has to be refused rather than bound to some arbitrary button.
        assert!(parse("Return").is_none());
        assert!(parse("Control+space").is_none());
        assert!(parse("").is_none());
        // Nearly-right names are refused too, not rounded to a neighbour.
        assert!(parse("MouseThird").is_none());
        assert!(parse("ScrollSideways").is_none());
        assert!(parse("MouseExtra").is_none());
        assert!(parse("MouseD").is_none());
        // Modifiers alone bind nothing.
        assert!(parse("Control+Shift").is_none());
    }

    #[test]
    fn a_list_keeps_what_parses_and_drops_what_does_not() {
        let bindings = parse_list("MousePrimary, nonsense ,ScrollDown");
        assert_eq!(bindings.len(), 2);
        assert_eq!(bindings[0].input, click(MouseButton::Primary, false));
        assert_eq!(bindings[1].input, MouseInput::Scroll(ScrollDirection::Down));
        assert!(parse_list(" , ").is_empty());
    }

    #[test]
    fn button_numbers_match_what_a_pointer_reports() {
        assert_eq!(MouseButton::Primary.number(), 1);
        assert_eq!(MouseButton::Middle.number(), 2);
        assert_eq!(MouseButton::Secondary.number(), 3);
        assert_eq!(MouseButton::Back.number(), 8);
        assert_eq!(MouseButton::Forward.number(), 9);
        assert_eq!(MouseButton::Extra(11).number(), 11);
    }
}
