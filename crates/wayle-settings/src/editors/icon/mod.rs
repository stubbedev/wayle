//! Editor for icon-name string fields: a button showing the current icon and
//! its name that opens a searchable popover of the bundled icons (rendered as
//! previews). The search entry doubles as a free-text field — pressing Enter
//! commits whatever was typed, so arbitrary system-theme icon names still work.

use std::{
    cell::{Cell, OnceCell},
    rc::Rc,
};

use relm4::gtk::{self, prelude::*};
use wayle_config::ConfigProperty;
use wayle_i18n::t;
use wayle_icons::IconManager;

use crate::{
    editors::spawn_property_watcher, pages::spec::SettingRowInit, property_handle::PropertyHandle,
    row::RowBehavior,
};

/// Pixel size of the icon previews in the picker grid and the trigger button.
const PREVIEW_SIZE: i32 = 24;

thread_local! {
    /// The bundled icon set, read from disk once per process. Shared by every
    /// icon editor so opening pickers doesn't re-scan the icons directory.
    static ICON_NAMES: OnceCell<Rc<Vec<String>>> = const { OnceCell::new() };
}

fn icon_names() -> Rc<Vec<String>> {
    ICON_NAMES.with(|cell| {
        cell.get_or_init(|| {
            let names = IconManager::new().map(|m| m.list()).unwrap_or_default();
            Rc::new(names)
        })
        .clone()
    })
}

/// A reusable icon-name picker: the trigger button plus the bits kept alive for
/// its lifetime. Embed [`Self::widget`] anywhere a compact icon field is needed
/// (the standalone `icon` row, or a cell inside a list/map editor) and call
/// [`Self::set_display`] to reflect an externally-driven value change.
pub(crate) struct IconPickerWidget {
    pub(crate) widget: gtk::MenuButton,
    /// Updates the trigger preview + name from the outside (e.g. a config
    /// watcher) without going through the picker's own commit path.
    pub(crate) set_display: Rc<dyn Fn(&str)>,
    _keep: Box<dyn std::any::Any>,
}

/// Builds the picker popover body (search entry + scrolled grid) and returns it
/// alongside the search entry, the clear button, and grid for wiring. The clear
/// button sits beside the search field and commits the empty string (set the
/// field to "no icon"); it's a real button so it gets a pointer cursor on hover,
/// which an in-entry secondary icon can't.
fn build_popover() -> (gtk::Popover, gtk::Entry, gtk::Button, gtk::FlowBox) {
    let search = gtk::Entry::builder()
        .placeholder_text(t("settings-icon-search"))
        .primary_icon_name("edit-find-symbolic")
        .hexpand(true)
        .build();

    let clear = gtk::Button::builder()
        .icon_name("ld-x-circle-symbolic")
        .css_classes(["flat", "icon-picker-clear"])
        .tooltip_text(t("settings-icon-clear"))
        .valign(gtk::Align::Center)
        .build();
    clear.set_cursor_from_name(Some("pointer"));

    let search_row = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(4)
        .build();
    search_row.append(&search);
    search_row.append(&clear);

    let flow = gtk::FlowBox::builder()
        .selection_mode(gtk::SelectionMode::None)
        .max_children_per_line(10)
        // Top-align so a short (or filtered) result set sits at the top of the
        // scroll area instead of floating in its vertical center.
        .valign(gtk::Align::Start)
        .row_spacing(4)
        .column_spacing(4)
        .homogeneous(true)
        .build();

    let scroller = gtk::ScrolledWindow::builder()
        .hscrollbar_policy(gtk::PolicyType::Never)
        .min_content_height(420)
        .min_content_width(440)
        .child(&flow)
        .build();

    let popover_box = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .css_classes(["icon-picker-popover"])
        .build();
    popover_box.append(&search_row);
    popover_box.append(&scroller);

    // Left-align the dropdown under the trigger's start edge (no arrow): GTK
    // derives the anchor gravity from the popover's halign, so Start anchors the
    // popover's bottom-left to the trigger's bottom-left.
    let popover = gtk::Popover::builder()
        .child(&popover_box)
        .has_arrow(false)
        .position(gtk::PositionType::Bottom)
        .halign(gtk::Align::Start)
        .build();
    (popover, search, clear, flow)
}

/// Updates the trigger button to reflect the current icon name.
fn update_display(image: &gtk::Image, label: &gtk::Label, name: &str) {
    if name.is_empty() {
        image.set_icon_name(Some("ld-image-symbolic"));
        label.set_text(&t("settings-icon-none"));
    } else {
        image.set_icon_name(Some(name));
        label.set_text(name);
    }
}

/// Builds the (lazily-populated) picker grid as flat icon buttons.
fn populate_grid(flow: &gtk::FlowBox, commit: &Rc<dyn Fn(&str)>) {
    for name in icon_names().iter() {
        let image = gtk::Image::from_icon_name(name);
        image.set_pixel_size(PREVIEW_SIZE);

        let button = gtk::Button::builder()
            .css_classes(["flat", "icon-picker-cell"])
            .tooltip_text(name)
            .child(&image)
            .build();
        button.set_widget_name(name);
        // Pointer cursor so the previews read as clickable.
        button.set_cursor_from_name(Some("pointer"));

        let commit = Rc::clone(commit);
        let name = name.clone();
        button.connect_clicked(move |_| commit(&name));

        flow.append(&button);
    }
}

/// Builds a reusable icon-name picker bound to a `set` callback, displaying
/// `initial` to start. The caller owns when/how the value is persisted; the
/// picker just reports the chosen (or typed) name.
pub(crate) fn icon_picker_widget(initial: &str, set: Rc<dyn Fn(&str)>) -> IconPickerWidget {
    let image = gtk::Image::new();
    // Size is set in CSS (`var(--icon-sm)`) so the trigger matches the height of
    // every other settings input (entries, dropdowns) and scales with the UI;
    // a fixed larger pixel size made the icon rows taller than the rest. The CSS
    // also pins a min-width so name labels still start at the same x across
    // stacked pickers (e.g. an icon-list editor).
    image.set_halign(gtk::Align::Center);
    let label = gtk::Label::builder()
        .ellipsize(gtk::pango::EllipsizeMode::End)
        .xalign(0.0)
        .build();

    let trigger_box = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .build();
    trigger_box.append(&image);
    trigger_box.append(&label);

    let menu = gtk::MenuButton::builder()
        .child(&trigger_box)
        .css_classes(["icon-picker-trigger"])
        .valign(gtk::Align::Center)
        .build();
    // GTK4 dropped the CSS `cursor` property, so the pointer must be set in code
    // for the trigger to read as clickable like the entry/dropdown fields.
    menu.set_cursor_from_name(Some("pointer"));

    update_display(&image, &label, initial);

    let (popover, search, clear, flow) = build_popover();
    menu.set_popover(Some(&popover));

    // Escape closes the picker without committing. A capture-phase controller is
    // needed so the keypress is handled before the search entry consumes it.
    {
        let key = gtk::EventControllerKey::new();
        key.set_propagation_phase(gtk::PropagationPhase::Capture);
        let menu = menu.clone();
        key.connect_key_pressed(move |_, keyval, _, _| {
            if keyval == gtk::gdk::Key::Escape {
                menu.popdown();
                gtk::glib::Propagation::Stop
            } else {
                gtk::glib::Propagation::Proceed
            }
        });
        popover.add_controller(key);
    }

    // Commit a chosen name: report it via `set`, refresh the trigger, close.
    let commit: Rc<dyn Fn(&str)> = {
        let image = image.clone();
        let label = label.clone();
        let menu = menu.clone();
        Rc::new(move |name: &str| {
            set(name);
            update_display(&image, &label, name);
            menu.popdown();
        })
    };

    // Populate the grid on first open only — building previews for the whole
    // icon set up front for every field would be wasteful.
    let built = Rc::new(Cell::new(false));
    {
        let flow = flow.clone();
        let commit = Rc::clone(&commit);
        let built = Rc::clone(&built);
        menu.connect_active_notify(move |btn| {
            if btn.is_active() && !built.replace(true) {
                populate_grid(&flow, &commit);
            }
        });
    }

    // Search filters the grid; Enter commits the typed text verbatim so any
    // system-theme icon name can be entered, not just the bundled ones.
    {
        let flow = flow.clone();
        search.connect_changed(move |entry| {
            let query = entry.text().to_lowercase();
            flow.set_filter_func(move |child| {
                if query.is_empty() {
                    return true;
                }
                child
                    .child()
                    .map(|w| w.widget_name().to_lowercase().contains(&query))
                    .unwrap_or(false)
            });
        });
    }
    {
        let commit = Rc::clone(&commit);
        search.connect_activate(move |entry| {
            let text = entry.text().to_string();
            if !text.is_empty() {
                commit(&text);
            }
        });
    }
    // The clear button commits the empty string to set the field to "no icon".
    {
        let commit = Rc::clone(&commit);
        clear.connect_clicked(move |_| commit(""));
    }

    let set_display: Rc<dyn Fn(&str)> = {
        let image = image.clone();
        let label = label.clone();
        Rc::new(move |name: &str| update_display(&image, &label, name))
    };

    IconPickerWidget {
        widget: menu,
        set_display,
        _keep: Box::new((popover, commit, built)),
    }
}

/// Row that edits an icon-name string with a searchable, preview-driven picker.
pub(crate) fn icon(property: &ConfigProperty<String>) -> SettingRowInit {
    let set: Rc<dyn Fn(&str)> = {
        let set = property.clone();
        Rc::new(move |name: &str| set.set(name.to_owned()))
    };
    let picker = icon_picker_widget(&property.get(), set);
    let control = picker.widget.clone().upcast();

    // External config changes (reset, file edit) refresh the trigger.
    let set_display = Rc::clone(&picker.set_display);
    let get = property.clone();
    let watcher = spawn_property_watcher(property, move || {
        set_display(&get.get());
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value: &String| value.clone()),
        control,
        keepalive: Box::new((picker, watcher)),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
