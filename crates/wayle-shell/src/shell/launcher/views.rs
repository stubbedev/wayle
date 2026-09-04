//! Row factory and keybinding handling for the launcher surface.

use relm4::{
    Sender,
    gtk::{self, gdk, glib, pango, prelude::*},
};
use tracing::warn;
use wayle_launcher::{
    IconSource, ItemFlags, MouseBinding, MouseInput, MouseModifiers, ScrollDirection,
};

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
    /// Open (or close) the completer mode.
    ModeComplete,
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
        "mode-complete" => KeyAction::ModeComplete,
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

/// Row text presentation knobs (rofi display-columns / ellipsize).
#[derive(Debug, Clone)]
pub(super) struct RowDisplay {
    /// 1-based columns to show (None = whole text).
    pub columns: Option<Vec<u32>>,
    /// Column separator.
    pub separator: String,
    /// Truncation mode: "start" | "middle" | anything else = end.
    pub ellipsize: String,
}

impl RowDisplay {
    fn apply_columns(&self, text: &str) -> String {
        let Some(columns) = &self.columns else {
            return text.to_owned();
        };
        let parts: Vec<&str> = text.split(self.separator.as_str()).collect();
        columns
            .iter()
            .filter_map(|&column| parts.get(column.saturating_sub(1) as usize).copied())
            .collect::<Vec<_>>()
            .join(" ")
    }

    fn ellipsize_mode(&self) -> pango::EllipsizeMode {
        match self.ellipsize.as_str() {
            "start" => pango::EllipsizeMode::Start,
            "middle" => pango::EllipsizeMode::Middle,
            _ => pango::EllipsizeMode::End,
        }
    }
}

/// Build the `SignalListItemFactory` for the results list.
pub(super) fn row_factory(
    show_icons: bool,
    display: RowDisplay,
    multi: std::rc::Rc<std::cell::RefCell<MultiSelect>>,
    sender: Sender<LauncherInput>,
    mouse: MouseTable,
    thumbnailer: std::rc::Rc<wayle_launcher::Thumbnailer>,
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
        add_row_gesture(row.upcast_ref(), list_item, sender.clone(), mouse.clone());
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
                widgets
                    .ballot
                    .set_text(if multi.picked.contains(&row.item_index) {
                        &multi.ballot_selected
                    } else {
                        &multi.ballot_unselected
                    });
                widgets.ballot.set_visible(true);
            } else {
                widgets.ballot.set_visible(false);
            }
        }

        widgets.label.set_ellipsize(display.ellipsize_mode());
        widgets
            .label
            .set_use_markup(row.item.flags.contains(ItemFlags::MARKUP));
        let shown = display.apply_columns(&row.item.display);
        if row.item.flags.contains(ItemFlags::MARKUP) {
            widgets.label.set_markup(&shown);
        } else {
            widgets.label.set_text(&shown);
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
            Some(IconSource::Thumbnail { path, fallback }) if show_icons => {
                show_thumbnail(&widgets.icon, list_item, path, fallback, &thumbnailer);
            }
            _ => {
                widgets.icon.set_icon_name(None);
                widgets.icon.set_visible(show_icons);
            }
        }

        set_class(
            &container,
            "urgent",
            row.item.flags.contains(ItemFlags::URGENT),
        );
        set_class(
            &container,
            "active",
            row.item.flags.contains(ItemFlags::ACTIVE),
        );
        set_class(
            &container,
            "nonselectable",
            row.item.flags.contains(ItemFlags::NONSELECTABLE),
        );
    });

    factory
}

/// Draws a `thumbnail://` icon: the fallback right away, the real thumbnail
/// as soon as there is one.
///
/// Generation is per visible row, which is what keeps a directory of ten
/// thousand files cheap — the list only ever binds the rows it draws.
///
/// Rows are recycled, so a finished thumbnail is applied only if the row is
/// still showing the file it was asked about. Otherwise scrolling past a slow
/// thumbnailer would paint one file's picture onto whatever row inherited the
/// widget.
fn show_thumbnail(
    icon: &gtk::Image,
    list_item: &gtk::ListItem,
    path: &std::path::Path,
    fallback: &str,
    thumbnailer: &std::rc::Rc<wayle_launcher::Thumbnailer>,
) {
    icon.set_visible(true);
    if let Some(cached) = wayle_launcher::thumbnail::cached(path) {
        icon.set_from_file(Some(cached));
        return;
    }
    icon.set_icon_name(Some(fallback));

    let icon = icon.clone();
    let list_item = list_item.clone();
    let path = path.to_path_buf();
    let thumbnailer = thumbnailer.clone();
    glib::spawn_future_local(async move {
        let Some(made) = thumbnailer.generate(&path).await else {
            return;
        };
        if still_showing(&list_item, &path) {
            icon.set_from_file(Some(made));
        }
    });
}

/// Whether a recycled row is still bound to the file a pending thumbnail was
/// requested for.
fn still_showing(list_item: &gtk::ListItem, path: &std::path::Path) -> bool {
    let Some(boxed) = list_item.item().and_downcast::<glib::BoxedAnyObject>() else {
        return false;
    };
    let row: std::cell::Ref<'_, Row> = boxed.borrow();
    matches!(
        &row.item.icon,
        Some(IconSource::Thumbnail { path: wanted, .. }) if wanted == path
    )
}

fn row_widgets(container: &gtk::Box) -> Option<RowWidgets> {
    let ballot = container.first_child()?.downcast::<gtk::Label>().ok()?;
    let icon = ballot.next_sibling()?.downcast::<gtk::Image>().ok()?;
    let label = icon.next_sibling()?.downcast::<gtk::Label>().ok()?;
    Some(RowWidgets {
        ballot,
        icon,
        label,
    })
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
    controller.connect_key_pressed(
        move |_, key, _, state| match lookup(&bindings(), key, state) {
            Some(action) => {
                sender.emit(LauncherInput::Key(action));
                glib::Propagation::Stop
            }
            None => glib::Propagation::Proceed,
        },
    );
    widget.add_controller(controller);
}

/// Surface-level actions a pointer binding can trigger.
///
/// rofi's `row-left`/`row-right` move between *columns*, and the launcher's
/// list has one — so they are recognised and do nothing, exactly as their
/// `kb-` counterparts already are.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum MouseAction {
    /// Select the row under the pointer.
    Select,
    /// Accept the row under the pointer.
    Accept,
    /// Accept the typed text instead of a row.
    AcceptCustom,
    /// Move the selection up.
    RowUp,
    /// Move the selection down.
    RowDown,
}

/// A resolved pointer binding.
pub(super) type MouseBindingEntry = (MouseBinding, MouseAction);

fn mouse_action_from_name(name: &str) -> Option<MouseAction> {
    Some(match name {
        "me-select-entry" => MouseAction::Select,
        "me-accept-entry" => MouseAction::Accept,
        "me-accept-custom" => MouseAction::AcceptCustom,
        "ml-row-up" => MouseAction::RowUp,
        "ml-row-down" => MouseAction::RowDown,
        // Recognised, no columns to move between.
        "ml-row-left" | "ml-row-right" => return None,
        _ => return None,
    })
}

/// Compile the effective mouse binding list into a lookup table.
pub(super) fn compile_mouse(bindings: &[(String, String)]) -> Vec<MouseBindingEntry> {
    let mut table = Vec::new();
    for (action_name, specs) in bindings {
        let Some(action) = mouse_action_from_name(action_name) else {
            continue;
        };
        for binding in wayle_launcher::mouse::parse_list(specs) {
            table.push((binding, action));
        }
    }
    table
}

/// The modifier set a pointer event was delivered with, in the launcher's
/// own representation.
fn mouse_modifiers(state: gdk::ModifierType) -> MouseModifiers {
    let mut modifiers = MouseModifiers::empty();
    modifiers.set(
        MouseModifiers::CONTROL,
        state.contains(gdk::ModifierType::CONTROL_MASK),
    );
    modifiers.set(
        MouseModifiers::SHIFT,
        state.contains(gdk::ModifierType::SHIFT_MASK),
    );
    modifiers.set(
        MouseModifiers::ALT,
        state.contains(gdk::ModifierType::ALT_MASK),
    );
    modifiers.set(
        MouseModifiers::SUPER,
        state.contains(gdk::ModifierType::SUPER_MASK),
    );
    modifiers
}

/// Every action bound to a button press, in the order to run them.
///
/// All of them, not the first: `me-select-entry` and `me-accept-entry` are
/// separate bindings that default to the same button, and a click has to do
/// both. Selecting sorts before accepting so the row is current when the
/// accept reads it.
///
/// `presses` is GTK's click count, so a double-press binding loses to a
/// single click while a single-press binding still fires on the first press
/// of a double click — which is what lets `MousePrimary` and `MouseDPrimary`
/// share one button.
pub(super) fn lookup_button(
    table: &[MouseBindingEntry],
    button: u32,
    presses: i32,
    state: gdk::ModifierType,
) -> Vec<MouseAction> {
    let modifiers = mouse_modifiers(state);
    let mut actions: Vec<MouseAction> = table
        .iter()
        .filter(|(binding, _)| {
            binding.modifiers == modifiers
                && match binding.input {
                    MouseInput::Click {
                        button: bound,
                        double,
                    } => bound.number() == button && (!double || presses >= 2),
                    MouseInput::Scroll(_) => false,
                }
        })
        .map(|(_, action)| *action)
        .collect();
    actions.sort_by_key(|action| u8::from(*action != MouseAction::Select));
    actions.dedup();
    actions
}

/// Every action bound to a scroll in `direction`.
pub(super) fn lookup_scroll(
    table: &[MouseBindingEntry],
    direction: ScrollDirection,
    state: gdk::ModifierType,
) -> Vec<MouseAction> {
    let modifiers = mouse_modifiers(state);
    let mut actions: Vec<MouseAction> = table
        .iter()
        .filter(|(binding, _)| {
            binding.modifiers == modifiers && binding.input == MouseInput::Scroll(direction)
        })
        .map(|(_, action)| *action)
        .collect();
    actions.dedup();
    actions
}

/// Shared, live-swappable pointer binding table.
///
/// An `Rc<RefCell<..>>` rather than a value because the row gestures are
/// installed once and the table changes per session, exactly as the key
/// controller's already does.
pub(super) type MouseTable = std::rc::Rc<std::cell::RefCell<Vec<MouseBindingEntry>>>;

/// Attaches the button bindings to one realized row.
///
/// On the row rather than on the list: a `ListItem` knows its own position,
/// and picking a row out of a click coordinate would have to re-derive it
/// from a row height the list is free to change.
///
/// Bubble phase, so GTK's own selection-on-click still happens first and a
/// bound action runs on top of it.
fn add_row_gesture(
    row: &gtk::Widget,
    list_item: &gtk::ListItem,
    sender: Sender<LauncherInput>,
    table: MouseTable,
) {
    let gesture = gtk::GestureClick::new();
    // 0 = every button, so a side button can be bound.
    gesture.set_button(0);
    gesture.set_propagation_phase(gtk::PropagationPhase::Bubble);
    let list_item = list_item.clone();
    gesture.connect_pressed(move |gesture, presses, _, _| {
        let state = gesture.current_event_state();
        let actions = lookup_button(&table.borrow(), gesture.current_button(), presses, state);
        let position = list_item.position();
        let row = (position != gtk::INVALID_LIST_POSITION).then_some(position);
        for action in actions {
            sender.emit(LauncherInput::Mouse { action, row });
        }
    });
    row.add_controller(gesture);
}

/// Attaches the scroll bindings to the results list.
///
/// Capture phase, and the event is stopped when a binding claims it: rofi's
/// wheel *moves the selection*, so the viewport must not scroll out from
/// under it at the same time.
pub(super) fn add_scroll_controller(
    list: &gtk::ListView,
    sender: Sender<LauncherInput>,
    table: MouseTable,
) {
    let scroll = gtk::EventControllerScroll::new(
        gtk::EventControllerScrollFlags::VERTICAL | gtk::EventControllerScrollFlags::HORIZONTAL,
    );
    scroll.set_propagation_phase(gtk::PropagationPhase::Capture);
    scroll.connect_scroll(move |controller, dx, dy| {
        let Some(direction) = scroll_direction(dx, dy) else {
            return glib::Propagation::Proceed;
        };
        let state = controller.current_event_state();
        let actions = lookup_scroll(&table.borrow(), direction, state);
        if actions.is_empty() {
            return glib::Propagation::Proceed;
        }
        for action in actions {
            sender.emit(LauncherInput::Mouse { action, row: None });
        }
        glib::Propagation::Stop
    });
    list.add_controller(scroll);
}

/// The dominant axis of a scroll delta, or `None` for a stop event.
fn scroll_direction(dx: f64, dy: f64) -> Option<ScrollDirection> {
    if dx == 0.0 && dy == 0.0 {
        return None;
    }
    if dy.abs() >= dx.abs() {
        return Some(if dy > 0.0 {
            ScrollDirection::Down
        } else {
            ScrollDirection::Up
        });
    }
    Some(if dx > 0.0 {
        ScrollDirection::Right
    } else {
        ScrollDirection::Left
    })
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

    #[test]
    fn mouse_bindings_compile_and_look_up_by_button() {
        let table = compile_mouse(&[
            (
                "me-accept-entry".to_owned(),
                "MousePrimary,MouseDPrimary".to_owned(),
            ),
            (
                "me-accept-custom".to_owned(),
                "Control+MouseDPrimary".to_owned(),
            ),
            ("ml-row-down".to_owned(), "ScrollDown".to_owned()),
            // Recognised, no columns to move between → contributes nothing.
            ("ml-row-left".to_owned(), "ScrollLeft".to_owned()),
        ]);

        assert_eq!(
            lookup_button(&table, 1, 1, gdk::ModifierType::empty()),
            [MouseAction::Accept]
        );
        assert_eq!(
            lookup_button(&table, 1, 2, gdk::ModifierType::CONTROL_MASK),
            [MouseAction::AcceptCustom]
        );
        assert_eq!(
            lookup_scroll(&table, ScrollDirection::Down, gdk::ModifierType::empty()),
            [MouseAction::RowDown]
        );
        assert!(
            lookup_scroll(&table, ScrollDirection::Left, gdk::ModifierType::empty()).is_empty(),
            "a column binding on a one-column list does nothing"
        );
    }

    #[test]
    fn a_double_press_binding_does_not_fire_on_a_single_click() {
        let table = compile_mouse(&[("me-accept-entry".to_owned(), "MouseDPrimary".to_owned())]);
        assert!(lookup_button(&table, 1, 1, gdk::ModifierType::empty()).is_empty());
        assert_eq!(
            lookup_button(&table, 1, 2, gdk::ModifierType::empty()),
            [MouseAction::Accept]
        );
        // A different button is not the bound one.
        assert!(lookup_button(&table, 3, 2, gdk::ModifierType::empty()).is_empty());
    }

    #[test]
    fn a_binding_with_no_modifiers_does_not_fire_with_one_held() {
        let table = compile_mouse(&[("me-select-entry".to_owned(), "MousePrimary".to_owned())]);
        assert_eq!(
            lookup_button(&table, 1, 1, gdk::ModifierType::empty()),
            [MouseAction::Select]
        );
        assert!(
            lookup_button(&table, 1, 1, gdk::ModifierType::ALT_MASK).is_empty(),
            "Alt+click is a different binding, not the same one"
        );
    }

    #[test]
    fn two_bindings_on_one_button_both_run_and_select_goes_first() {
        // The shipped defaults put select-entry and accept-entry on
        // MousePrimary, so a click has to do both — and select before
        // accept, or the accept reads the row the pointer left behind.
        let table = compile_mouse(&[
            ("me-accept-entry".to_owned(), "MousePrimary".to_owned()),
            ("me-select-entry".to_owned(), "MousePrimary".to_owned()),
        ]);
        assert_eq!(
            lookup_button(&table, 1, 1, gdk::ModifierType::empty()),
            [MouseAction::Select, MouseAction::Accept]
        );
    }

    #[test]
    fn the_shipped_defaults_bind_what_they_say_they_do() {
        let table = compile_mouse(&wayle_launcher::keybinds::effective_mouse(
            &std::collections::BTreeMap::new(),
        ));
        // wayle keeps single-click accept; rofi's double click also accepts.
        assert_eq!(
            lookup_button(&table, 1, 1, gdk::ModifierType::empty()),
            [MouseAction::Select, MouseAction::Accept]
        );
        assert_eq!(
            lookup_button(&table, 1, 2, gdk::ModifierType::empty()),
            [MouseAction::Select, MouseAction::Accept],
            "a double click is still one accept, not two"
        );
        assert_eq!(
            lookup_button(&table, 1, 2, gdk::ModifierType::CONTROL_MASK),
            [MouseAction::AcceptCustom]
        );
        assert_eq!(
            lookup_scroll(&table, ScrollDirection::Up, gdk::ModifierType::empty()),
            [MouseAction::RowUp]
        );
        assert_eq!(
            lookup_scroll(&table, ScrollDirection::Down, gdk::ModifierType::empty()),
            [MouseAction::RowDown]
        );
    }
}
