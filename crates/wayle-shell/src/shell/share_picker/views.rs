//! Notebook page builders for the share picker: windows, outputs, region.
//!
//! Compositor-agnostic: window thumbnails capture via Hyprland's
//! toplevel-export when a handle is available, otherwise the generic
//! `ext-image-copy-capture` path; output thumbnails + layout come from
//! `wl_output` (wlr-screencopy). The capturable window/output identity all
//! flows from the `XDPH_WINDOW_SHARING_LIST` toplevels and `wl_output`, never
//! the Hyprland socket.

use std::sync::Arc;

use gtk4::{
    Box, Button, EventControllerKey, Fixed, FlowBox, FlowBoxChild, GestureClick, Label, Notebook,
    Overlay, Picture, ScrolledWindow, Spinner,
    glib::{self, clone},
    prelude::*,
};
use relm4::Sender;
use tracing::{debug, error};
use wayland_client::Connection;
use wayle_share_preview::{
    buffer::Buffer, ext_capture::ExtToplevelManager, frame::FrameManager, image::Image,
    image::Transforms, output::OutputManager, toplevel::Toplevel,
};

use super::{SharePickerInput, config::PickerConfig, image::ImageExt, util::OutputInfo};

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
pub(super) fn build_windows_page(
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

    if toplevels.is_empty() {
        return placeholder(&scrolled_window, "No windows available");
    }

    for toplevel in toplevels {
        let card = build_window_card(toplevel, config, input);
        container.insert(&card, 0);
    }

    container.set_max_children_per_line(config.windows_max_per_row.min(toplevels.len() as u32));
    scrolled_window
}

fn build_window_card(
    toplevel: &Toplevel,
    config: &PickerConfig,
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

    request_window_frame(toplevel, config.resize_size, tx);
    update_frame_lazily(card, picture, Some(spinner), rx);

    container
}

fn request_window_frame(
    toplevel: &Toplevel,
    resize_size: u32,
    tx: tokio::sync::oneshot::Sender<Image>,
) {
    let id = toplevel.id;
    let address = toplevel.window_address;
    let class = toplevel.class.clone();
    let title = toplevel.title.clone();

    // Captures block on a busy Wayland dispatch loop; run them on the blocking
    // thread pool so every window/output is captured concurrently rather than
    // starving the few async worker threads and draining serially.
    relm4::spawn_blocking(move || {
        let buffer = match capture_window_buffer(address, &class, &title) {
            Ok(buffer) => buffer,
            Err(err) => return error!(%err, id, "unable to capture window frame"),
        };
        let img = match Image::new(buffer).and_then(Image::into_rgb) {
            Ok(img) => img,
            Err(err) => return error!(%err, id, "unable to build rgb image for window"),
        };
        let mut img = img;
        img.resize_to_fit(resize_size);
        if tx.send(img).is_err() {
            error!(id, "unable to transmit window image: channel closed");
        }
    });
}

/// Captures a window: Hyprland toplevel-export when a handle is present,
/// otherwise the generic `ext` path matching by app_id/title.
fn capture_window_buffer(
    address: Option<u64>,
    class: &str,
    title: &str,
) -> Result<Buffer, String> {
    let connection =
        Connection::connect_to_env().map_err(|e| format!("cannot connect to wayland: {e}"))?;

    if let Some(handle) = address
        && let Ok(mut manager) = FrameManager::new(&connection)
        && let Ok(buffer) = manager.capture_frame(handle)
    {
        return Ok(buffer);
    }

    let mut manager = ExtToplevelManager::new(&connection)
        .map_err(|_| "window capture not supported on this compositor".to_owned())?;
    let handle = manager
        .toplevels()
        .iter()
        .find(|t| t.app_id.as_deref() == Some(class) && t.title.as_deref() == Some(title))
        .map(|t| t.handle.clone())
        .ok_or("could not match the window to capture")?;
    manager
        .capture_toplevel(&handle)
        .map_err(|e| format!("window capture failed: {e}"))
}

// --- Outputs page ----------------------------------------------------------

/// Pixel bounding box across all outputs, used to lay out output cards.
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

impl From<&[OutputInfo]> for MonitorArea {
    fn from(outputs: &[OutputInfo]) -> Self {
        let min_x = outputs.iter().map(|o| o.x).min().unwrap_or_default();
        let min_y = outputs.iter().map(|o| o.y).min().unwrap_or_default();
        let max_x = outputs
            .iter()
            .map(|o| o.x + o.width)
            .max()
            .unwrap_or_default();
        let max_y = outputs
            .iter()
            .map(|o| o.y + o.height)
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
            aspect_ratio: width as f64 / height.max(1) as f64,
            offset_x: -min_x,
            offset_y: -min_y,
        }
    }
}

/// Builds the outputs page from the live `wl_output` layout.
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

    let mut outputs: Vec<OutputInfo> = manager
        .outputs
        .iter()
        .filter_map(|(wl_output, output)| OutputInfo::from_output(wl_output, output))
        .collect();

    if outputs.is_empty() {
        return placeholder(&scrolled_window, "No outputs available");
    }

    if config.outputs_respect_scaling {
        apply_output_scaling(&mut outputs);
    }
    let area = MonitorArea::from(outputs.as_slice());

    for output in &outputs {
        let card = build_output_card(output, config, &manager, &area, input);
        append_output_on_allocation(&container, &card, output, &area);
    }

    scrolled_window
}

/// Scales each output's extent down to logical size, so mixed-DPI layouts read
/// the way the compositor lays them out.
fn apply_output_scaling(outputs: &mut [OutputInfo]) {
    for output in outputs.iter_mut() {
        if output.scale > 1.0 {
            output.width = (output.width as f32 / output.scale) as i32;
            output.height = (output.height as f32 / output.scale) as i32;
        }
    }
}

fn build_output_card(
    output: &OutputInfo,
    config: &PickerConfig,
    manager: &Arc<OutputManager>,
    area: &MonitorArea,
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

    if area.min_x != output.x {
        card.set_margin_start(config.outputs_spacing as i32);
    }
    if area.max_x != output.x + output.width {
        card.set_margin_end(config.outputs_spacing as i32);
    }
    if area.min_y != output.y {
        card.set_margin_top(config.outputs_spacing as i32);
    }
    if area.max_y != output.y + output.height {
        card.set_margin_bottom(config.outputs_spacing as i32);
    }
    card.append(&picture);

    if config.outputs_show_label {
        let label = Label::builder()
            .max_width_chars(1)
            .label(&output.name)
            .ellipsize(gtk4::pango::EllipsizeMode::End)
            .single_line_mode(true)
            .css_classes(["share-picker-image-label"])
            .hexpand(false)
            .build();
        card.append(&label);
    }

    let container = Button::builder().focusable(true).child(&card).build();
    container.set_cursor_from_name(Some("pointer"));
    let payload = format!("screen:{}", output.name);

    container.connect_clicked(clone!(
        #[strong]
        input,
        #[strong]
        payload,
        move |_| input.emit(SharePickerInput::Select(payload.clone()))
    ));

    request_output_frame(output, config.resize_size, manager.clone(), tx);
    update_frame_lazily(card, picture, None, rx);

    container
}

fn request_output_frame(
    output: &OutputInfo,
    resize_size: u32,
    manager: Arc<OutputManager>,
    tx: tokio::sync::oneshot::Sender<Image>,
) {
    let name = output.name.clone();
    let wl_output = output.wl_output.clone();
    let transform = output.transform;

    relm4::spawn_blocking(move || {
        // capture_output needs `&mut self`; clone per concurrent capture.
        let mut manager = (*manager).clone();
        let buffer = match manager.capture_output(&wl_output) {
            Ok(buffer) => buffer,
            Err(err) => return error!(%err, name, "unable to capture output"),
        };
        let img = match Image::new(buffer).and_then(Image::into_rgb) {
            Ok(img) => img,
            Err(err) => return error!(%err, name, "unable to build rgb image for output"),
        };
        let mut img = img;
        img.resize_to_fit(resize_size);
        let img = img.transform(Transforms::from(transform));
        if tx.send(img).is_err() {
            error!(name, "unable to transmit output image: channel closed");
        }
    });
}

#[allow(clippy::similar_names)]
fn append_output_on_allocation(
    container: &Fixed,
    card: &Button,
    output: &OutputInfo,
    area: &MonitorArea,
) {
    let aspect_ratio = area.aspect_ratio;
    let monitors_width = area.width;
    let monitors_height = area.height;
    let offset_x = area.offset_x;
    let offset_y = area.offset_y;
    let (height, width, x, y) = (output.height, output.width, output.x, output.y);

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
            let monitors_width_f = monitors_width.max(1) as f64;
            let monitors_height_f = monitors_height.max(1) as f64;
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

            card.set_width_request(transform_x(width) as i32);
            card.set_height_request(transform_y(height) as i32);

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

/// Builds the region page; clicking the button opens the in-shell region
/// overlay and emits the selected `region:<output>@<x>,<y>,<w>,<h>`.
pub(super) fn build_region_page(input: &Sender<SharePickerInput>) -> ScrolledWindow {
    let container = Box::builder()
        .css_classes(["share-picker-page"])
        .orientation(gtk4::Orientation::Vertical)
        .halign(gtk4::Align::Center)
        .valign(gtk4::Align::Center)
        .build();
    let scrolled_window = ScrolledWindow::builder().child(&container).build();

    let button = Button::builder()
        .label("Select region")
        .css_classes(["primary", "share-picker-region-button"])
        .build();
    button.set_cursor_from_name(Some("pointer"));
    container.insert_child_after(&button, Option::<&Box>::None);

    button.connect_clicked(clone!(
        #[strong]
        input,
        move |btn| {
            let Some(root) = btn.root() else {
                return;
            };
            // Hide the picker while the overlay is up so it does not occlude
            // the screen being selected; restore it if the user cancels.
            root.set_visible(false);

            glib::spawn_future_local(clone!(
                #[strong]
                input,
                #[strong]
                root,
                async move {
                    match crate::services::region_overlay::request_region(
                        std::collections::HashMap::new(),
                    )
                    .await
                    {
                        Some(sel) => {
                            input.emit(SharePickerInput::Select(format!(
                                "region:{}@{},{},{},{}",
                                sel.output, sel.x, sel.y, sel.width, sel.height
                            )));
                        }
                        None => {
                            debug!("region selection cancelled");
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

    let windows = build_windows_page(toplevels, config, input);
    let windows_idx = notebook.append_page(&windows, Some(&page_label("Windows")));
    let outputs = build_outputs_page(con, config, input);
    let outputs_idx = notebook.append_page(&outputs, Some(&page_label("Outputs")));
    let region = build_region_page(input);
    let region_idx = notebook.append_page(&region, Some(&page_label("Region")));

    let default = match config.default_page {
        Page::Windows => windows_idx,
        Page::Outputs => outputs_idx,
        Page::Region => region_idx,
    };
    notebook.set_current_page(Some(default));
}
