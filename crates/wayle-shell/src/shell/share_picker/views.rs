//! Notebook page builders for the share picker: windows, outputs, region.
//!
//! Ported from the standalone `hyprland-preview-share-picker`. Selection is
//! delivered through a Relm4 input [`Sender`] instead of a GTK action, and
//! logging goes through `tracing`.

use std::{collections::HashMap, process::Command, sync::Arc};

use gtk4::{
    Box, Button, EventControllerKey, Fixed, FlowBox, FlowBoxChild, GestureClick, Label, Notebook,
    Overlay, Picture, ScrolledWindow, Spinner,
    glib::{self, clone},
    prelude::*,
};
use hyprland::{
    data::{Clients, Monitor, Monitors, Transforms},
    shared::HyprData,
};
use regex::Regex;
use relm4::Sender;
use tracing::{debug, error, warn};
use wayland_client::{Connection, protocol::wl_output::WlOutput};
use wayle_share_preview::{
    frame::FrameManager, image::Image, output::OutputManager, toplevel::Toplevel,
};

use super::{
    SharePickerInput,
    config::PickerConfig,
    image::ImageExt,
    util::{ClientExt, MonitorTransformExt},
};

/// Adds an Escape-to-cancel key controller to a widget.
pub(super) fn add_escape_controller(
    widget: &impl IsA<gtk4::Widget>,
    input: Sender<SharePickerInput>,
) {
    let controller = EventControllerKey::new();
    controller.connect_key_pressed(move |_, key, _, _| {
        if key == gtk4::gdk::Key::Escape {
            input.emit(SharePickerInput::Cancel);
            return glib::Propagation::Stop;
        }
        glib::Propagation::Proceed
    });
    widget.add_controller(controller);
}

// --- Windows page ----------------------------------------------------------

/// Builds the windows page from the XDPH toplevel list.
#[allow(clippy::cognitive_complexity)]
pub(super) fn build_windows_page(
    con: &Connection,
    toplevels: &[Toplevel],
    config: &PickerConfig,
    input: &Sender<SharePickerInput>,
) -> ScrolledWindow {
    let container = FlowBox::builder()
        .vexpand(false)
        .row_spacing(config.windows_spacing)
        .column_spacing(config.windows_spacing)
        .orientation(gtk4::Orientation::Horizontal)
        .homogeneous(true)
        .min_children_per_line(config.windows_min_per_row)
        .build();
    let scrolled_window = ScrolledWindow::builder()
        .child(&container)
        .css_classes(["share-picker-page"])
        .build();

    let manager = match FrameManager::new(con) {
        Ok(manager) => Arc::new(manager),
        Err(err) => {
            error!(%err, "unable to create frame manager");
            return placeholder(&scrolled_window, "No windows available");
        }
    };
    let clients = match Clients::get() {
        Ok(clients) => clients
            .into_iter()
            .map(|mut client| {
                client.sanitize();
                client
            })
            .collect::<Vec<_>>(),
        Err(err) => {
            error!(%err, "unable to get clients from hyprland socket");
            Vec::new()
        }
    };
    let monitors = Monitors::get()
        .map(|monitors| monitors.into_iter().collect::<Vec<_>>())
        .unwrap_or_else(|err| {
            error!(%err, "unable to get monitors from hyprland socket");
            Vec::new()
        });

    let mut cards = 0;
    for toplevel in toplevels {
        let Some(client) = clients
            .iter()
            .find(|c| c.class.eq(&toplevel.class) && c.title.eq(&toplevel.title))
        else {
            error!("no hyprland client matches toplevel class and title");
            continue;
        };
        let Some(monitor) = monitors.iter().find(|m| Some(m.id) == client.monitor) else {
            error!("no hyprland monitor for hyprland client");
            continue;
        };

        let handle_str = &format!("{}", client.address)[2..];
        let alt_handle = match u64::from_str_radix(handle_str, 16) {
            Ok(handle) => handle,
            Err(err) => {
                error!(%err, "unable to convert client address to u64");
                continue;
            }
        };

        let card = build_window_card(
            toplevel,
            config,
            monitor.transform,
            alt_handle,
            &manager,
            input,
        );
        cards += 1;
        container.insert(&card, 0);
    }

    if cards == 0 {
        return placeholder(&scrolled_window, "No windows available");
    }

    container.set_max_children_per_line(config.windows_max_per_row.min(cards));
    scrolled_window
}

fn build_window_card(
    toplevel: &Toplevel,
    config: &PickerConfig,
    transform: Transforms,
    alt_handle: u64,
    manager: &Arc<FrameManager>,
    input: &Sender<SharePickerInput>,
) -> FlowBoxChild {
    let (tx, rx) = tokio::sync::oneshot::channel();

    let picture = Picture::builder()
        .vexpand(true)
        .valign(gtk4::Align::Center)
        .height_request(config.widget_size)
        .content_fit(gtk4::ContentFit::Contain)
        .css_classes(["share-picker-image"])
        .build();
    let spinner = Spinner::builder()
        .spinning(true)
        .halign(gtk4::Align::Center)
        .valign(gtk4::Align::Center)
        .build();

    let card = Box::builder()
        .orientation(gtk4::Orientation::Vertical)
        .vexpand(false)
        .hexpand(false)
        .halign(gtk4::Align::Fill)
        .valign(gtk4::Align::Start)
        .css_classes(["share-picker-card", "share-picker-card-loading"])
        .build();

    let overlay = Overlay::builder().child(&picture).build();
    overlay.add_overlay(&spinner);

    let title = if toplevel.title.trim().is_empty() {
        toplevel.class.as_str()
    } else {
        toplevel.title.as_str()
    };
    let label = Label::builder()
        .max_width_chars(1)
        .label(title)
        .ellipsize(gtk4::pango::EllipsizeMode::End)
        .single_line_mode(true)
        .css_classes(["share-picker-image-label"])
        .hexpand(false)
        .build();
    let class_label = Label::builder()
        .max_width_chars(1)
        .label(toplevel.class.as_str())
        .ellipsize(gtk4::pango::EllipsizeMode::End)
        .single_line_mode(true)
        .css_classes(["share-picker-image-class-label"])
        .hexpand(false)
        .build();

    card.append(&overlay);
    card.append(&label);
    card.append(&class_label);

    let container = FlowBoxChild::builder()
        .halign(gtk4::Align::Fill)
        .valign(gtk4::Align::Fill)
        .child(&card)
        .build();
    container.set_tooltip_text(Some(&format!("{}\n{}", toplevel.title, toplevel.class)));

    let payload = format!("window:{}", toplevel.id);
    container.set_cursor_from_name(Some("pointer"));
    let gesture = GestureClick::new();
    gesture.connect_released(clone!(
        #[strong]
        input,
        #[strong]
        payload,
        move |_, _, _, _| input.emit(SharePickerInput::Select(payload.clone()))
    ));
    container.add_controller(gesture);
    container.connect_activate(clone!(
        #[strong]
        input,
        #[strong]
        payload,
        move |_| input.emit(SharePickerInput::Select(payload.clone()))
    ));

    request_window_frame(
        toplevel,
        config.resize_size,
        transform,
        alt_handle,
        manager.clone(),
        tx,
    );
    update_frame_lazily(card, picture, Some(spinner), rx);

    container
}

fn request_window_frame(
    toplevel: &Toplevel,
    resize_size: u32,
    transform: Transforms,
    alt_handle: u64,
    manager: Arc<FrameManager>,
    tx: tokio::sync::oneshot::Sender<Image>,
) {
    let id = toplevel.id;
    let handle = toplevel.window_address.unwrap_or_else(|| {
        warn!(
            id,
            "missing window address in toplevel, falling back to socket address"
        );
        alt_handle
    });

    relm4::spawn(async move {
        // FrameManager::capture_frame needs `&mut self`; clone the (cheap,
        // Connection-backed) manager so each concurrent capture owns one.
        let mut manager = (*manager).clone();
        let buffer = match manager.capture_frame(handle) {
            Ok(buffer) => buffer,
            Err(err) => return error!(%err, id, "unable to capture frame for toplevel"),
        };
        let img = match Image::new(buffer).and_then(Image::into_rgb) {
            Ok(img) => img,
            Err(err) => return error!(%err, id, "unable to build rgb image for toplevel"),
        };
        let mut img = img;
        img.resize_to_fit(resize_size);
        let img = img.transform(transform.into());
        if tx.send(img).is_err() {
            error!(id, "unable to transmit toplevel image: channel closed");
        }
    });
}

// --- Outputs page ----------------------------------------------------------

/// Pixel bounding box across all monitors, used to lay out output cards.
struct MonitorArea {
    min_x: i32,
    max_x: i32,
    min_y: i32,
    max_y: i32,
    aspect_ratio: f64,
    width: i32,
    height: i32,
    offset_x: i32,
    offset_y: i32,
}

impl From<&Vec<Monitor>> for MonitorArea {
    fn from(monitors: &Vec<Monitor>) -> Self {
        let min_x = monitors.iter().map(|m| m.x).min().unwrap_or_default();
        let min_y = monitors.iter().map(|m| m.y).min().unwrap_or_default();
        let max_x = monitors
            .iter()
            .map(|m| m.x + m.width as i32)
            .max()
            .unwrap_or_default();
        let max_y = monitors
            .iter()
            .map(|m| m.y + m.height as i32)
            .max()
            .unwrap_or_default();
        let width = max_x - min_x;
        let height = max_y - min_y;
        Self {
            min_x,
            max_x,
            min_y,
            max_y,
            width,
            height,
            aspect_ratio: width as f64 / height as f64,
            offset_x: -min_x,
            offset_y: -min_y,
        }
    }
}

/// Builds the outputs page from the live monitor layout.
#[allow(clippy::cognitive_complexity)]
pub(super) fn build_outputs_page(
    con: &Connection,
    config: &PickerConfig,
    input: &Sender<SharePickerInput>,
) -> ScrolledWindow {
    // Fill the viewport (not shrink to the children's bounding box) so the
    // per-card `px_offset_x` / `px_offset_y` have real slack to center the
    // monitor map into — otherwise the map pins to the top-left corner.
    let container = Fixed::builder().hexpand(true).vexpand(true).build();
    let scrolled_window = ScrolledWindow::builder()
        .child(&container)
        .css_classes(["share-picker-page"])
        .build();

    let manager = match OutputManager::new(con) {
        Ok(manager) => Arc::new(manager),
        Err(err) => {
            error!(%err, "unable to create output manager");
            return placeholder(&scrolled_window, "No outputs available");
        }
    };
    let mut monitors = match Monitors::get() {
        Ok(monitors) => monitors.into_iter().collect::<Vec<_>>(),
        Err(err) => {
            error!(%err, "unable to get monitors from hyprland socket");
            return placeholder(&scrolled_window, "No outputs available");
        }
    };
    monitors
        .iter_mut()
        .for_each(MonitorTransformExt::apply_transform);
    if config.outputs_respect_scaling {
        apply_output_scaling(&mut monitors);
    }
    let area = MonitorArea::from(&monitors);

    if manager.outputs.is_empty() {
        return placeholder(&scrolled_window, "No outputs available");
    }

    for (wl_output, output) in &manager.outputs {
        let Some(name) = &output.name else {
            error!("output without a name");
            continue;
        };
        let Some(monitor) = monitors.iter().find(|m| m.name.eq(name)).cloned() else {
            error!(name, "output does not exist on hyprland");
            continue;
        };
        let card = build_output_card(&monitor, config, wl_output, &area, &manager, input);
        append_output_on_allocation(&container, &card, &monitor, &area);
    }

    scrolled_window
}

/// Compensates monitor positions for fractional scaling. Verbatim port of the
/// upstream heuristic; ugly but matches what users already see.
fn apply_output_scaling(monitors: &mut [Monitor]) {
    let mut translations: HashMap<i128, i32> = HashMap::new();
    monitors.iter().for_each(|m| {
        translations.insert(m.id, 0);
    });

    monitors.sort_by_key(|a| a.x);
    let copy = monitors.to_vec();
    monitors.iter_mut().for_each(|m| {
        if m.scale != 1.0 {
            let new_width = (m.width as f32 / m.scale) as u16;
            let translation = if new_width > m.width {
                (new_width - m.width) as i32
            } else {
                -((m.width - new_width) as i32)
            };
            copy.iter()
                .filter(|o| {
                    o.x > m.x + m.width as i32
                        && o.y <= m.y + m.height as i32
                        && o.y + o.height as i32 >= m.y
                })
                .for_each(|o| {
                    if let Some(entry) = translations.get_mut(&o.id) {
                        *entry += translation;
                    }
                });
            m.width = new_width;
        }
    });
    for (key, value) in translations.iter_mut() {
        if let Some(m) = monitors.iter_mut().find(|m| m.id == *key) {
            m.x += *value;
        }
        *value = 0;
    }

    monitors.sort_by_key(|a| a.y);
    let copy = monitors.to_vec();
    monitors.iter_mut().for_each(|m| {
        if m.scale != 1.0 {
            let new_height = (m.height as f32 / m.scale) as u16;
            let translation = if new_height > m.height {
                (new_height - m.height) as i32
            } else {
                -((m.height - new_height) as i32)
            };
            copy.iter()
                .filter(|o| {
                    o.y > m.y + m.height as i32
                        && o.x <= m.x + m.width as i32
                        && o.x + o.width as i32 >= m.x
                })
                .for_each(|o| {
                    if let Some(entry) = translations.get_mut(&o.id) {
                        *entry += translation;
                    }
                });
            m.height = new_height;
        }
    });
    for (key, value) in &translations {
        if let Some(m) = monitors.iter_mut().find(|m| m.id == *key) {
            m.y += *value;
        }
    }
}

fn build_output_card(
    monitor: &Monitor,
    config: &PickerConfig,
    output: &WlOutput,
    area: &MonitorArea,
    manager: &Arc<OutputManager>,
    input: &Sender<SharePickerInput>,
) -> Button {
    let (tx, rx) = tokio::sync::oneshot::channel();

    let picture = Picture::builder()
        .vexpand(true)
        .valign(gtk4::Align::Fill)
        .halign(gtk4::Align::Fill)
        .content_fit(gtk4::ContentFit::Fill)
        .css_classes(["share-picker-image"])
        .build();

    let card = Box::builder()
        .orientation(gtk4::Orientation::Vertical)
        .vexpand(false)
        .hexpand(false)
        .halign(gtk4::Align::Fill)
        .valign(gtk4::Align::Fill)
        .css_classes(["share-picker-card", "share-picker-card-loading"])
        .build();

    if area.min_x != monitor.x {
        card.set_margin_start(config.outputs_spacing as i32);
    }
    if area.max_x != monitor.x + monitor.width as i32 {
        card.set_margin_end(config.outputs_spacing as i32);
    }
    if area.min_y != monitor.y {
        card.set_margin_top(config.outputs_spacing as i32);
    }
    if area.max_y != monitor.y + monitor.height as i32 {
        card.set_margin_bottom(config.outputs_spacing as i32);
    }
    card.append(&picture);

    if config.outputs_show_label {
        let label = Label::builder()
            .max_width_chars(1)
            .label(&monitor.name)
            .ellipsize(gtk4::pango::EllipsizeMode::End)
            .single_line_mode(true)
            .css_classes(["share-picker-image-label"])
            .hexpand(false)
            .build();
        card.append(&label);
    }

    let container = Button::builder().focusable(true).child(&card).build();
    container.set_cursor_from_name(Some("pointer"));
    let payload = format!("screen:{}", monitor.name);

    container.connect_clicked(clone!(
        #[strong]
        input,
        #[strong]
        payload,
        move |_| input.emit(SharePickerInput::Select(payload.clone()))
    ));

    request_output_frame(
        monitor,
        config.resize_size,
        output.clone(),
        manager.clone(),
        tx,
    );
    update_frame_lazily(card, picture, None, rx);

    container
}

fn request_output_frame(
    monitor: &Monitor,
    resize_size: u32,
    output: WlOutput,
    manager: Arc<OutputManager>,
    tx: tokio::sync::oneshot::Sender<Image>,
) {
    let name = monitor.name.clone();
    let transform = monitor.transform;

    relm4::spawn(async move {
        // capture_output needs `&mut self`; clone per concurrent capture.
        let mut manager = (*manager).clone();
        let buffer = match manager.capture_output(&output) {
            Ok(buffer) => buffer,
            Err(err) => return error!(%err, name, "unable to capture output"),
        };
        let img = match Image::new(buffer).and_then(Image::into_rgb) {
            Ok(img) => img,
            Err(err) => return error!(%err, name, "unable to build rgb image for output"),
        };
        let mut img = img;
        img.resize_to_fit(resize_size);
        let img = img.transform(transform.into());
        if tx.send(img).is_err() {
            error!(name, "unable to transmit output image: channel closed");
        }
    });
}

#[allow(clippy::similar_names)]
fn append_output_on_allocation(
    container: &Fixed,
    card: &Button,
    monitor: &Monitor,
    area: &MonitorArea,
) {
    let aspect_ratio = area.aspect_ratio;
    let monitors_width = area.width;
    let monitors_height = area.height;
    let offset_x = area.offset_x;
    let offset_y = area.offset_y;
    let (height, width, x, y) = (monitor.height, monitor.width, monitor.x, monitor.y);

    container.add_tick_callback(clone!(
        #[strong]
        card,
        move |container, _| {
            let alloc_w = container.width();
            let alloc_h = container.height();
            if alloc_w == 0 || alloc_h == 0 {
                return glib::ControlFlow::Continue;
            }
            let container_aspect_ratio = alloc_w as f64 / alloc_h as f64;
            let monitors_width_f = monitors_width as f64;
            let monitors_height_f = monitors_height as f64;
            let transform_x = |x: i32| {
                if aspect_ratio > container_aspect_ratio {
                    (x as f64 / monitors_width_f) * alloc_w as f64
                } else {
                    (x as f64 / monitors_width_f) * alloc_h as f64 * aspect_ratio
                }
            };
            let transform_y = |y: i32| {
                if aspect_ratio > container_aspect_ratio {
                    (y as f64 / monitors_height_f) * alloc_w as f64 / aspect_ratio
                } else {
                    (y as f64 / monitors_height_f) * alloc_h as f64
                }
            };

            card.set_width_request(transform_x(width as i32) as i32);
            card.set_height_request(transform_y(height as i32) as i32);

            let transformed_monitor_width = transform_x(monitors_width);
            let transformed_monitor_height = transform_y(monitors_height);
            let px_offset_x = (alloc_w as f64 - transformed_monitor_width).max(0.0) / 2.0;
            let px_offset_y = (alloc_h as f64 - transformed_monitor_height).max(0.0) / 2.0;

            container.put(
                &card,
                px_offset_x + transform_x(offset_x + x),
                px_offset_y + transform_y(offset_y + y),
            );
            glib::ControlFlow::Break
        }
    ));
}

// --- Region page -----------------------------------------------------------

/// Builds the region page; clicking the button runs the region tool and emits
/// the selected `region:<output>@<x>,<y>,<w>,<h>`.
pub(super) fn build_region_page(
    config: &PickerConfig,
    input: &Sender<SharePickerInput>,
) -> ScrolledWindow {
    let container = Box::builder()
        .css_classes(["share-picker-page"])
        .orientation(gtk4::Orientation::Vertical)
        .halign(gtk4::Align::Center)
        .valign(gtk4::Align::Center)
        .build();
    let scrolled_window = ScrolledWindow::builder().child(&container).build();

    let Some(args) = shlex::split(&config.region_command) else {
        error!(command = config.region_command, "invalid region command");
        return scrolled_window;
    };
    if args.is_empty() {
        error!("empty region command");
        return scrolled_window;
    }
    let regex = match Regex::new(r"^.+@-?\d+,-?\d+,\d+,\d+$") {
        Ok(regex) => regex,
        Err(err) => {
            error!(%err, "invalid region regex");
            return scrolled_window;
        }
    };

    let button = Button::builder()
        .label("Select region")
        .css_classes(["primary", "share-picker-region-button"])
        .build();
    button.set_cursor_from_name(Some("pointer"));
    container.insert_child_after(&button, Option::<&Box>::None);

    button.connect_clicked(clone!(
        #[strong]
        input,
        #[strong]
        args,
        #[strong]
        regex,
        move |btn| {
            let Some(root) = btn.root() else {
                return;
            };
            let mut command = Command::new(&args[0]);
            command.args(&args[1..]);
            debug!(?command, "running region command");
            root.set_visible(false);

            glib::spawn_future_local(clone!(
                #[strong]
                input,
                #[strong]
                regex,
                #[strong]
                root,
                async move {
                    match command.output() {
                        Ok(output) => {
                            let region = String::from_utf8_lossy(&output.stdout);
                            let region = region.trim();
                            if regex.is_match(region) {
                                input.emit(SharePickerInput::Select(format!("region:{region}")));
                            } else {
                                error!(region, "region command returned unexpected output");
                                root.set_visible(true);
                            }
                        }
                        Err(err) => {
                            error!(%err, "error while selecting share region");
                            root.set_visible(true);
                        }
                    }
                }
            ));
        }
    ));

    scrolled_window
}

// --- Shared helpers --------------------------------------------------------

fn placeholder(scrolled_window: &ScrolledWindow, text: &str) -> ScrolledWindow {
    let placeholder = Label::builder()
        .label(text)
        .halign(gtk4::Align::Center)
        .valign(gtk4::Align::Center)
        .vexpand(true)
        .hexpand(true)
        .css_classes(["share-picker-placeholder"])
        .build();
    scrolled_window.set_child(Some(&placeholder));
    scrolled_window.clone()
}

fn update_frame_lazily(
    card: Box,
    picture: Picture,
    spinner: Option<Spinner>,
    rx: tokio::sync::oneshot::Receiver<Image>,
) {
    glib::spawn_future_local(async move {
        let result = rx.await;
        if let Some(spinner) = &spinner {
            spinner.set_visible(false);
        }
        card.remove_css_class("share-picker-card-loading");
        let img = match result {
            Ok(img) => img,
            Err(err) => return error!(%err, "unable to receive captured image"),
        };
        match img.into_pixbuf() {
            // `Picture::set_pixbuf` is deprecated since GTK 4.12; wrap the
            // pixbuf in a texture and set it as the paintable instead.
            Ok(pixbuf) => picture.set_paintable(Some(&gtk4::gdk::Texture::for_pixbuf(&pixbuf))),
            Err(err) => error!(%err, "unable to create pixbuf from captured image"),
        }
    });
}

/// Index of a config page within the notebook.
pub(super) fn page_label(text: &str) -> Label {
    let label = Label::builder()
        .css_classes(["share-picker-tab-label"])
        .label(text)
        .hexpand(true)
        .build();
    // `hexpand` makes the label fill the tab, so a pointer cursor on it covers
    // the whole clickable tab area.
    label.set_cursor_from_name(Some("pointer"));
    label
}

/// Appends all three pages to `notebook` and selects the configured default.
pub(super) fn populate_notebook(
    notebook: &Notebook,
    con: &Connection,
    toplevels: &[Toplevel],
    config: &PickerConfig,
    input: &Sender<SharePickerInput>,
) {
    use super::config::Page;

    let windows = build_windows_page(con, toplevels, config, input);
    let windows_idx = notebook.append_page(&windows, Some(&page_label("Windows")));
    let outputs = build_outputs_page(con, config, input);
    let outputs_idx = notebook.append_page(&outputs, Some(&page_label("Outputs")));
    let region = build_region_page(config, input);
    let region_idx = notebook.append_page(&region, Some(&page_label("Region")));

    let default = match config.default_page {
        Page::Windows => windows_idx,
        Page::Outputs => outputs_idx,
        Page::Region => region_idx,
    };
    notebook.set_current_page(Some(default));
}
