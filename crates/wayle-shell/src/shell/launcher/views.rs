//! Row factory and keybinding handling for the launcher surface.

use relm4::{
    Sender,
    gtk::{self, gdk, glib, pango, prelude::*},
};
use tracing::warn;
use wayle_launcher::{IconSource, ItemFlags};

use super::{LauncherInput, match_model::Row};

/// Estimated row height used to fix the list height at `lines` rows.
// ponytail: constant estimate; measure the first realized row if themes
// with large fonts make this visibly wrong.
pub(super) const ROW_PX: i32 = 40;

/// Surface-level actions the key controller can trigger.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum KeyAction {
    /// Accept the selected row.
    Accept,
    /// Alternate accept (run-in-terminal etc.).
    AcceptAlt,
    /// Accept the typed text as custom input.
    AcceptCustom,
    /// Dismiss the launcher.
    Cancel,
    /// Shift-delete: remove entry / close window.
    DeleteEntry,
    /// Next mode tab.
    ModeNext,
    /// Previous mode tab.
    ModePrevious,
    /// Move selection up/down.
    RowUp,
    /// Move selection down.
    RowDown,
    /// Jump to first row.
    RowFirst,
    /// Jump to last row.
    RowLast,
    /// Page up.
    PagePrev,
    /// Page down.
    PageNext,
    /// kb-custom-N (1..=19).
    Custom(u8),
}

/// A resolved binding: key + modifiers → action.
pub(super) type KeyBinding = (gdk::Key, gdk::ModifierType, KeyAction);

/// Actions the surface implements, mapped from rofi `kb-` action names.
/// Entry-editing actions (move-word, remove-char, paste) stay with GTK's
/// own editing keys.
fn action_from_name(name: &str) -> Option<KeyAction> {
    Some(match name {
        "accept-entry" => KeyAction::Accept,
        "accept-alt" => KeyAction::AcceptAlt,
        "accept-custom" => KeyAction::AcceptCustom,
        "cancel" => KeyAction::Cancel,
        "delete-entry" => KeyAction::DeleteEntry,
        "mode-next" => KeyAction::ModeNext,
        "mode-previous" => KeyAction::ModePrevious,
        "row-up" => KeyAction::RowUp,
        "row-down" | "element-next" => KeyAction::RowDown,
        "element-prev" => KeyAction::RowUp,
        "row-first" => KeyAction::RowFirst,
        "row-last" => KeyAction::RowLast,
        "page-prev" => KeyAction::PagePrev,
        "page-next" => KeyAction::PageNext,
        custom => {
            let n: u8 = custom.strip_prefix("custom-")?.parse().ok()?;
            (1..=19).contains(&n).then_some(KeyAction::Custom(n))?
        }
    })
}

/// Compile the effective binding list into a lookup table.
pub(super) fn compile_bindings(bindings: &[(String, String)]) -> Vec<KeyBinding> {
    let mut table = Vec::new();
    for (action_name, keys) in bindings {
        let Some(action) = action_from_name(action_name) else {
            continue;
        };
        for key_spec in keys.split(',') {
            match parse_key(key_spec.trim()) {
                Some((key, modifiers)) => table.push((key, modifiers, action)),
                None => warn!(binding = %key_spec, "unparseable launcher keybinding"),
            }
        }
    }
    table
}

/// Parse rofi-style `Control+Shift+Return` into a gdk key + modifier mask.
fn parse_key(spec: &str) -> Option<(gdk::Key, gdk::ModifierType)> {
    let mut modifiers = gdk::ModifierType::empty();
    let mut key = None;
    for part in spec.split('+') {
        match part {
            "Control" | "Ctrl" => modifiers |= gdk::ModifierType::CONTROL_MASK,
            "Shift" => modifiers |= gdk::ModifierType::SHIFT_MASK,
            "Alt" | "Mod1" => modifiers |= gdk::ModifierType::ALT_MASK,
            "Super" | "Mod4" => modifiers |= gdk::ModifierType::SUPER_MASK,
            name => key = gdk::Key::from_name(name),
        }
    }
    key.map(|key| (key, modifiers))
}

/// Find the action bound to a pressed key.
pub(super) fn lookup(
    table: &[KeyBinding],
    key: gdk::Key,
    state: gdk::ModifierType,
) -> Option<KeyAction> {
    let relevant = gdk::ModifierType::CONTROL_MASK
        | gdk::ModifierType::SHIFT_MASK
        | gdk::ModifierType::ALT_MASK
        | gdk::ModifierType::SUPER_MASK;
    let state = state & relevant;
    // Match both the exact keyval and its lowercase form so Shift+letter
    // bindings work regardless of how the compositor reports the keyval.
    table
        .iter()
        .find(|(bound_key, bound_mods, _)| {
            (*bound_key == key || *bound_key == key.to_lower()) && *bound_mods == state
        })
        .map(|(_, _, action)| *action)
}

/// Widgets inside one recycled list row.
struct RowWidgets {
    ballot: gtk::Label,
    icon: gtk::Image,
    label: gtk::Label,
}

/// Multi-select display state shared between the component and the factory.
#[derive(Debug, Default)]
pub(super) struct MultiSelect {
    /// Multi-select is active for the current session.
    pub enabled: bool,
    /// Toggled item indices.
    pub picked: std::collections::BTreeSet<u32>,
    /// Ballot prefix for picked rows (rofi `-ballot-selected-str`).
    pub ballot_selected: String,
    /// Ballot prefix for unpicked rows.
    pub ballot_unselected: String,
}

/// Build the `SignalListItemFactory` for the results list.
pub(super) fn row_factory(
    show_icons: bool,
    multi: std::rc::Rc<std::cell::RefCell<MultiSelect>>,
) -> gtk::SignalListItemFactory {
    let factory = gtk::SignalListItemFactory::new();

    factory.connect_setup(move |_, object| {
        let Some(list_item) = object.downcast_ref::<gtk::ListItem>() else {
            return;
        };
        let row = gtk::Box::new(gtk::Orientation::Horizontal, 8);
        row.add_css_class("launcher-row");
        let ballot = gtk::Label::new(None);
        ballot.add_css_class("launcher-row-ballot");
        ballot.set_visible(false);
        let icon = gtk::Image::new();
        icon.add_css_class("launcher-row-icon");
        icon.set_visible(show_icons);
        let label = gtk::Label::new(None);
        label.add_css_class("launcher-row-label");
        label.set_xalign(0.0);
        label.set_hexpand(true);
        label.set_ellipsize(pango::EllipsizeMode::End);
        label.set_single_line_mode(true);
        row.append(&ballot);
        row.append(&icon);
        row.append(&label);
        list_item.set_child(Some(&row));
    });

    factory.connect_bind(move |_, object| {
        let Some(list_item) = object.downcast_ref::<gtk::ListItem>() else {
            return;
        };
        let Some(boxed) = list_item.item().and_downcast::<glib::BoxedAnyObject>() else {
            return;
        };
        let row: std::cell::Ref<'_, Row> = boxed.borrow();
        let Some(container) = list_item.child().and_downcast::<gtk::Box>() else {
            return;
        };
        let Some(widgets) = row_widgets(&container) else {
            return;
        };

        {
            let multi = multi.borrow();
            if multi.enabled {
                widgets.ballot.set_text(if multi.picked.contains(&row.item_index) {
                    &multi.ballot_selected
                } else {
                    &multi.ballot_unselected
                });
                widgets.ballot.set_visible(true);
            } else {
                widgets.ballot.set_visible(false);
            }
        }

        widgets.label.set_use_markup(row.item.flags.contains(ItemFlags::MARKUP));
        if row.item.flags.contains(ItemFlags::MARKUP) {
            widgets.label.set_markup(&row.item.display);
        } else {
            widgets.label.set_text(&row.item.display);
        }

        match &row.item.icon {
            Some(IconSource::Name(name)) if show_icons => {
                widgets.icon.set_icon_name(Some(name));
                widgets.icon.set_visible(true);
            }
            Some(IconSource::File(path)) if show_icons => {
                widgets.icon.set_from_file(Some(path));
                widgets.icon.set_visible(true);
            }
            _ => {
                widgets.icon.set_icon_name(None);
                widgets.icon.set_visible(show_icons);
            }
        }

        set_class(&container, "urgent", row.item.flags.contains(ItemFlags::URGENT));
        set_class(&container, "active", row.item.flags.contains(ItemFlags::ACTIVE));
        set_class(
            &container,
            "nonselectable",
            row.item.flags.contains(ItemFlags::NONSELECTABLE),
        );
    });

    factory
}

fn row_widgets(container: &gtk::Box) -> Option<RowWidgets> {
    let ballot = container.first_child()?.downcast::<gtk::Label>().ok()?;
    let icon = ballot.next_sibling()?.downcast::<gtk::Image>().ok()?;
    let label = icon.next_sibling()?.downcast::<gtk::Label>().ok()?;
    Some(RowWidgets { ballot, icon, label })
}

fn set_class(widget: &impl IsA<gtk::Widget>, class: &str, on: bool) {
    if on {
        widget.add_css_class(class);
    } else {
        widget.remove_css_class(class);
    }
}

/// Key controller for the whole surface (capture phase so list navigation
/// works while the entry has focus). Unmatched keys proceed to the entry.
pub(super) fn add_key_controller(
    widget: &impl IsA<gtk::Widget>,
    sender: Sender<LauncherInput>,
    bindings: impl Fn() -> Vec<KeyBinding> + 'static,
) {
    let controller = gtk::EventControllerKey::new();
    controller.set_propagation_phase(gtk::PropagationPhase::Capture);
    controller.connect_key_pressed(move |_, key, _, state| {
        match lookup(&bindings(), key, state) {
            Some(action) => {
                sender.emit(LauncherInput::Key(action));
                glib::Propagation::Stop
            }
            None => glib::Propagation::Proceed,
        }
    });
    widget.add_controller(controller);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_key_with_modifiers() {
        let (key, modifiers) = parse_key("Control+Shift+Return").unwrap();
        assert_eq!(key, gdk::Key::Return);
        assert!(modifiers.contains(gdk::ModifierType::CONTROL_MASK));
        assert!(modifiers.contains(gdk::ModifierType::SHIFT_MASK));
    }

    #[test]
    fn compile_and_lookup() {
        let table = compile_bindings(&[
            ("accept-entry".to_owned(), "Return,KP_Enter".to_owned()),
            ("custom-3".to_owned(), "Alt+3".to_owned()),
            ("move-word-back".to_owned(), "Alt+b".to_owned()), // unimplemented → skipped
        ]);
        assert_eq!(
            lookup(&table, gdk::Key::Return, gdk::ModifierType::empty()),
            Some(KeyAction::Accept)
        );
        assert_eq!(
            lookup(&table, gdk::Key::_3, gdk::ModifierType::ALT_MASK),
            Some(KeyAction::Custom(3))
        );
        assert_eq!(
            lookup(&table, gdk::Key::b, gdk::ModifierType::ALT_MASK),
            None
        );
    }
}
