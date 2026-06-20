//! On-screen webcam-position preview.
//!
//! A layer-shell overlay window that renders the live webcam feed at the exact
//! size and position it will occupy in the recording, so the position can be set
//! by dragging the real picture around the real screen. Position is persisted as
//! relative percentages (matching the recorder pipeline), so it stays correct
//! across monitors of different resolutions.
//!
//! The live feed comes from a GStreamer `v4l2src ! videoconvert !
//! gtk4paintablesink` pipeline; the sink exposes a `gdk::Paintable` shown in a
//! `gtk::Picture`. If `gtk4paintablesink` (gst-plugin-gtk4) is not installed the
//! window still opens and stays draggable, just without the live image.

use std::{cell::Cell, rc::Rc, sync::Arc};

use gstreamer as gst;
use gst::prelude::*;
use gtk::prelude::*;
use gtk4_layer_shell::{Edge, KeyboardMode, Layer, LayerShell};
use relm4::{gtk, prelude::*};
use tracing::warn;
use wayle_config::{ConfigService, schemas::styling::Percentage};

pub(super) struct WebcamPreviewInit {
    pub config: Arc<ConfigService>,
    pub monitor: gtk::gdk::Monitor,
}

pub(super) struct WebcamPreview {
    /// Kept alive so [`Drop`] can stop it; `None` if the pipeline failed to build.
    pipeline: Option<gst::Element>,
}

#[derive(Debug)]
pub(super) enum WebcamPreviewMsg {
    /// The "done" button was pressed; ask the parent to close the preview.
    Done,
}

impl SimpleComponent for WebcamPreview {
    type Init = WebcamPreviewInit;
    type Input = WebcamPreviewMsg;
    type Output = ();
    type Root = gtk::Window;
    type Widgets = ();

    fn init_root() -> Self::Root {
        gtk::Window::new()
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        root.init_layer_shell();
        root.set_namespace(Some("wayle-webcam-preview"));
        root.set_layer(Layer::Overlay);
        root.set_keyboard_mode(KeyboardMode::None);
        root.set_monitor(Some(&init.monitor));
        root.set_decorated(false);
        root.add_css_class("webcam-preview-window");

        let config = init.config.config();
        let rec = &config.modules.recorder;

        let geo = init.monitor.geometry();
        let screen_w = geo.width().max(1);
        let screen_h = geo.height().max(1);
        let size = i32::from(rec.webcam_size.get().value());
        let cam_w = (screen_w * size / 100).max(80);
        let cam_h = (cam_w * 9 / 16).max(45);
        let free_w = (screen_w - cam_w).max(0);
        let free_h = (screen_h - cam_h).max(0);
        let xpos = free_w * i32::from(rec.webcam_x.get().value().min(100)) / 100;
        let ypos = free_h * i32::from(rec.webcam_y.get().value().min(100)) / 100;

        root.set_anchor(Edge::Top, true);
        root.set_anchor(Edge::Left, true);
        root.set_margin(Edge::Left, xpos);
        root.set_margin(Edge::Top, ypos);
        root.set_default_size(cam_w, cam_h);

        let overlay = gtk::Overlay::new();

        let picture = gtk::Picture::new();
        picture.add_css_class("webcam-preview-picture");
        picture.set_size_request(cam_w, cam_h);
        overlay.set_child(Some(&picture));

        let done = gtk::Button::new();
        done.set_icon_name("ld-check-symbolic");
        done.set_tooltip_text(Some("Done"));
        done.add_css_class("webcam-preview-done");
        done.set_halign(gtk::Align::End);
        done.set_valign(gtk::Align::Start);
        {
            let sender = sender.clone();
            done.connect_clicked(move |_| sender.input(WebcamPreviewMsg::Done));
        }
        overlay.add_overlay(&done);

        root.set_child(Some(&overlay));

        let pipeline = build_pipeline(&rec.webcam_device.get(), &picture);

        // Drag the live picture anywhere on the monitor; persist on release.
        let drag = gtk::GestureDrag::new();
        let pos = Rc::new(Cell::new((xpos, ypos)));
        let start = Rc::new(Cell::new((0, 0)));
        {
            let (pos, start) = (pos.clone(), start.clone());
            drag.connect_drag_begin(move |_, _, _| start.set(pos.get()));
        }
        {
            let (pos, start, win) = (pos.clone(), start.clone(), root.clone());
            drag.connect_drag_update(move |_, offset_x, offset_y| {
                let (sx, sy) = start.get();
                let nx = (sx + offset_x as i32).clamp(0, free_w);
                let ny = (sy + offset_y as i32).clamp(0, free_h);
                win.set_margin(Edge::Left, nx);
                win.set_margin(Edge::Top, ny);
                pos.set((nx, ny));
            });
        }
        {
            let (pos, config) = (pos.clone(), init.config.clone());
            drag.connect_drag_end(move |_, _, _| {
                let (px, py) = pos.get();
                let rec = &config.config().modules.recorder;
                rec.webcam_x.set(Percentage::new(pct(px, free_w)));
                rec.webcam_y.set(Percentage::new(pct(py, free_h)));
            });
        }
        overlay.add_controller(drag);

        root.set_visible(true);

        ComponentParts {
            model: Self { pipeline },
            widgets: (),
        }
    }

    fn update(&mut self, msg: Self::Input, sender: ComponentSender<Self>) {
        match msg {
            WebcamPreviewMsg::Done => {
                let _ = sender.output(());
            }
        }
    }
}

impl Drop for WebcamPreview {
    fn drop(&mut self) {
        if let Some(pipeline) = &self.pipeline {
            let _ = pipeline.set_state(gst::State::Null);
        }
    }
}

/// Converts a pixel offset into a 0-100 percentage of `free`, 0 when no room.
fn pct(px: i32, free: i32) -> u8 {
    if free <= 0 {
        0
    } else {
        (px.clamp(0, free) * 100 / free) as u8
    }
}

/// Builds and starts the preview pipeline, wiring its paintable into `picture`.
/// Returns `None` (and logs) if GStreamer or the gtk4 sink is unavailable.
fn build_pipeline(device: &str, picture: &gtk::Picture) -> Option<gst::Element> {
    if let Err(err) = gst::init() {
        warn!(%err, "webcam preview: gstreamer init failed");
        return None;
    }
    let device_arg = if device.is_empty() {
        String::new()
    } else {
        format!(" device={device}")
    };
    let description =
        format!("v4l2src{device_arg} ! videoconvert ! gtk4paintablesink name=preview_sink");

    let pipeline = match gst::parse::launch(&description) {
        Ok(pipeline) => pipeline,
        Err(err) => {
            warn!(%err, "webcam preview: pipeline failed to build (is gst-plugin-gtk4 installed?)");
            return None;
        }
    };

    let sink = pipeline
        .dynamic_cast_ref::<gst::Bin>()
        .and_then(|bin| bin.by_name("preview_sink"));
    if let Some(sink) = sink {
        let paintable = sink.property::<gtk::gdk::Paintable>("paintable");
        picture.set_paintable(Some(&paintable));
    }

    if let Err(err) = pipeline.set_state(gst::State::Playing) {
        warn!(%err, "webcam preview: failed to start pipeline");
    }
    Some(pipeline)
}
