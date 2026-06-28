//! Magnifier-loupe colour picker overlay.
//!
//! Shows a frozen-frame layer-shell surface on every monitor. A zoom loupe
//! follows the pointer, magnifying the pixels under the cursor with a crosshair
//! on the exact pixel that will be picked, a live hex/RGB readout, and a row of
//! recently-picked swatches. A click samples that one pixel and returns it;
//! Escape cancels.
//!
//! Capture lives in the screenshot host (which owns the wlroots capture path);
//! it hands us per-output frozen frames + textures through
//! [`crate::services::color_picker::request_color`].

use std::{cell::RefCell, collections::HashMap, rc::Rc};

use gtk4_layer_shell::{Edge, KeyboardMode, Layer, LayerShell};
use image::RgbImage;
use relm4::{
    gtk,
    gtk::{ContentFit, EventControllerKey, EventControllerMotion, GestureClick, cairo, gdk, glib, prelude::*},
    prelude::*,
};
use tokio::sync::oneshot;

use crate::shell::helpers::monitors::current_monitors;

/// Zoom factor: each source pixel is drawn as a `ZOOM`×`ZOOM` block.
const ZOOM: f64 = 11.0;
/// Loupe radius in source pixels (the grid is `2*RADIUS+1` square).
const RADIUS: i32 = 8;
/// Most swatches kept in the recently-picked history.
const HISTORY_MAX: usize = 8;

/// A surface's shared last-known pointer position (`None` when off-surface).
type Cursor = Rc<RefCell<Option<(f64, f64)>>>;
/// Shared recently-picked colour swatches, newest first.
type History = Rc<RefCell<Vec<(u8, u8, u8)>>>;

/// Per-monitor frozen frame: the sampled image plus the texture painted behind
/// the loupe.
pub(crate) struct FrameData {
    pub(crate) image: RgbImage,
    pub(crate) texture: gdk::Texture,
}

/// Messages driving the picker.
pub(crate) enum ColorPickerInput {
    /// Open the picker; the chosen sRGB `(r, g, b)` in `[0, 1]` (or `None` on
    /// cancel) is sent back. `frames` is keyed by output connector.
    Show {
        reply: oneshot::Sender<Option<(f64, f64, f64)>>,
        frames: HashMap<String, FrameData>,
    },
    /// The user picked a colour (`Some`) or cancelled (`None`); tears down.
    Finish(Option<(f64, f64, f64)>),
}

impl std::fmt::Debug for ColorPickerInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Show { frames, .. } => f
                .debug_struct("Show")
                .field("outputs", &frames.len())
                .finish_non_exhaustive(),
            Self::Finish(c) => f.debug_tuple("Finish").field(c).finish(),
        }
    }
}

/// The colour-picker overlay component.
pub(crate) struct ColorPicker {
    reply: Option<oneshot::Sender<Option<(f64, f64, f64)>>>,
    surfaces: Vec<gtk::Window>,
    /// Recently-picked colours, newest first; shared across every surface's
    /// loupe and persisted between picks.
    history: History,
}

#[relm4::component(pub(crate))]
impl Component for ColorPicker {
    type Init = ();
    type Input = ColorPickerInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        // Placeholder root: real surfaces are per-monitor layer-shell windows.
        #[root]
        gtk::Window {
            set_decorated: false,
            set_visible: false,
        }
    }

    fn init(
        _init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let model = ColorPicker {
            reply: None,
            surfaces: Vec::new(),
            history: Rc::new(RefCell::new(load_history())),
        };
        let widgets = view_output!();
        let _ = &sender;
        ComponentParts { model, widgets }
    }

    fn update(
        &mut self,
        msg: ColorPickerInput,
        sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            ColorPickerInput::Show { reply, frames } => {
                if let Some(prev) = self.reply.take() {
                    let _ = prev.send(None);
                }
                self.close();
                self.reply = Some(reply);
                self.open(&sender, frames);
            }
            ColorPickerInput::Finish(color) => {
                if let Some((r, g, b)) = color {
                    self.push_history((
                        (r * 255.0).round() as u8,
                        (g * 255.0).round() as u8,
                        (b * 255.0).round() as u8,
                    ));
                }
                if let Some(reply) = self.reply.take() {
                    let _ = reply.send(color);
                }
                self.close();
            }
        }
    }
}

impl ColorPicker {
    /// Builds and shows one frozen-frame surface per monitor that has a frame.
    fn open(&mut self, sender: &ComponentSender<Self>, frames: HashMap<String, FrameData>) {
        for (connector, monitor) in current_monitors() {
            let Some(frame) = frames.get(&connector) else {
                continue;
            };
            let g = monitor.geometry();
            let logical = (g.width(), g.height());
            let image = Rc::new(frame.image.clone());
            let cursor: Cursor = Rc::new(RefCell::new(None));

            let window = gtk::Window::builder().decorated(false).build();
            window.add_css_class("color-picker-window");
            window.init_layer_shell();
            window.set_namespace(Some("wayle-color-picker"));
            window.set_layer(Layer::Overlay);
            window.set_monitor(Some(&monitor));
            window.set_keyboard_mode(KeyboardMode::Exclusive);
            window.set_exclusive_zone(-1);
            for edge in [Edge::Top, Edge::Bottom, Edge::Left, Edge::Right] {
                window.set_anchor(edge, true);
            }

            let area = gtk::DrawingArea::new();
            area.set_hexpand(true);
            area.set_vexpand(true);
            area.set_cursor_from_name(Some("none"));
            attach_loupe_draw(&area, &image, logical, &cursor, &self.history);

            let picture = gtk::Picture::for_paintable(&frame.texture);
            picture.set_content_fit(ContentFit::Fill);
            let overlay = gtk::Overlay::new();
            overlay.set_child(Some(&picture));
            overlay.add_overlay(&area);
            window.set_child(Some(&overlay));

            attach_motion(&area, &cursor);
            attach_click(&area, sender, &image, logical);
            attach_escape(&window, sender);

            window.present();
            self.surfaces.push(window);
        }

        // No frame matched any monitor — nothing to pick from.
        if self.surfaces.is_empty()
            && let Some(reply) = self.reply.take()
        {
            let _ = reply.send(None);
        }
    }

    fn close(&mut self) {
        for window in self.surfaces.drain(..) {
            window.destroy();
        }
    }

    fn push_history(&mut self, color: (u8, u8, u8)) {
        let mut history = self.history.borrow_mut();
        history.retain(|c| *c != color);
        history.insert(0, color);
        history.truncate(HISTORY_MAX);
        save_history(&history);
    }
}

/// Samples the source pixel under a surface-local logical point.
fn sample(image: &RgbImage, logical: (i32, i32), lx: f64, ly: f64) -> (u8, u8, u8) {
    let sx = image.width() as f64 / logical.0.max(1) as f64;
    let sy = image.height() as f64 / logical.1.max(1) as f64;
    let px = ((lx * sx) as i64).clamp(0, image.width() as i64 - 1) as u32;
    let py = ((ly * sy) as i64).clamp(0, image.height() as i64 - 1) as u32;
    let p = image.get_pixel(px, py);
    (p[0], p[1], p[2])
}

/// Records the pointer position (surface-local) and repaints the loupe.
fn attach_motion(area: &gtk::DrawingArea, cursor: &Cursor) {
    let motion = EventControllerMotion::new();
    {
        let cursor = Rc::clone(cursor);
        let area_ref = area.clone();
        motion.connect_motion(move |_, x, y| {
            *cursor.borrow_mut() = Some((x, y));
            area_ref.queue_draw();
        });
    }
    {
        let cursor = Rc::clone(cursor);
        let area_ref = area.clone();
        motion.connect_leave(move |_| {
            *cursor.borrow_mut() = None;
            area_ref.queue_draw();
        });
    }
    area.add_controller(motion);
}

/// Wires a click to sample the pixel under the cursor and finish.
fn attach_click(
    area: &gtk::DrawingArea,
    sender: &ComponentSender<ColorPicker>,
    image: &Rc<RgbImage>,
    logical: (i32, i32),
) {
    let click = GestureClick::new();
    let image = Rc::clone(image);
    let input = sender.input_sender().clone();
    click.connect_released(move |_, _, x, y| {
        let (r, g, b) = sample(&image, logical, x, y);
        input.emit(ColorPickerInput::Finish(Some((
            f64::from(r) / 255.0,
            f64::from(g) / 255.0,
            f64::from(b) / 255.0,
        ))));
    });
    area.add_controller(click);
}

/// Wires Escape to cancel the whole picker.
fn attach_escape(window: &gtk::Window, sender: &ComponentSender<ColorPicker>) {
    let input = sender.input_sender().clone();
    let key = EventControllerKey::new();
    key.connect_key_pressed(move |_, keyval, _, _| {
        if keyval == gdk::Key::Escape {
            input.emit(ColorPickerInput::Finish(None));
            return glib::Propagation::Stop;
        }
        glib::Propagation::Proceed
    });
    window.add_controller(key);
}

/// Wires the cairo draw func that paints the magnifier loupe + readout.
fn attach_loupe_draw(
    area: &gtk::DrawingArea,
    image: &Rc<RgbImage>,
    logical: (i32, i32),
    cursor: &Cursor,
    history: &History,
) {
    let image = Rc::clone(image);
    let cursor = Rc::clone(cursor);
    let history = Rc::clone(history);
    area.set_draw_func(move |_, cr, width, height| {
        let Some((cx, cy)) = *cursor.borrow() else {
            return;
        };
        draw_loupe(cr, &image, logical, cx, cy, width, height, &history.borrow());
    });
}

/// Paints the loupe grid, crosshair, hex/RGB readout, and history swatches.
#[allow(clippy::too_many_arguments)]
fn draw_loupe(
    cr: &cairo::Context,
    image: &RgbImage,
    logical: (i32, i32),
    cx: f64,
    cy: f64,
    width: i32,
    height: i32,
    history: &[(u8, u8, u8)],
) {
    let sx = image.width() as f64 / logical.0.max(1) as f64;
    let sy = image.height() as f64 / logical.1.max(1) as f64;
    let center_px = ((cx * sx) as i64).clamp(0, image.width() as i64 - 1) as i32;
    let center_py = ((cy * sy) as i64).clamp(0, image.height() as i64 - 1) as i32;

    let grid = f64::from(2 * RADIUS + 1);
    let loupe_size = grid * ZOOM;
    let pad = 10.0;
    let readout_h = 28.0;
    let swatch = 16.0;
    let swatch_gap = 4.0;
    let history_h = if history.is_empty() {
        0.0
    } else {
        swatch + pad
    };
    let panel_w = loupe_size;
    let panel_h = loupe_size + readout_h + history_h;

    // Place the panel near the cursor, flipping to stay on-screen.
    let mut px = cx + 24.0;
    let mut py = cy + 24.0;
    if px + panel_w > f64::from(width) {
        px = cx - 24.0 - panel_w;
    }
    if py + panel_h > f64::from(height) {
        py = cy - 24.0 - panel_h;
    }
    px = px.max(0.0);
    py = py.max(0.0);

    cr.set_operator(cairo::Operator::Over);

    // Backing card.
    cr.set_source_rgba(0.08, 0.08, 0.10, 0.92);
    cr.rectangle(px - 4.0, py - 4.0, panel_w + 8.0, panel_h + 8.0);
    let _ = cr.fill();

    // Magnified pixel grid.
    for gy in -RADIUS..=RADIUS {
        for gx in -RADIUS..=RADIUS {
            let sxp = (center_px + gx).clamp(0, image.width() as i32 - 1) as u32;
            let syp = (center_py + gy).clamp(0, image.height() as i32 - 1) as u32;
            let p = image.get_pixel(sxp, syp);
            cr.set_source_rgb(
                f64::from(p[0]) / 255.0,
                f64::from(p[1]) / 255.0,
                f64::from(p[2]) / 255.0,
            );
            let rx = px + f64::from(gx + RADIUS) * ZOOM;
            let ry = py + f64::from(gy + RADIUS) * ZOOM;
            cr.rectangle(rx, ry, ZOOM, ZOOM);
            let _ = cr.fill();
        }
    }

    // Crosshair box on the exact center pixel.
    let center = px + f64::from(RADIUS) * ZOOM;
    cr.set_source_rgba(1.0, 1.0, 1.0, 0.95);
    cr.set_line_width(1.5);
    cr.rectangle(center, py + f64::from(RADIUS) * ZOOM, ZOOM, ZOOM);
    let _ = cr.stroke();
    cr.set_source_rgba(0.0, 0.0, 0.0, 0.9);
    cr.set_line_width(1.5);
    cr.rectangle(center - 1.0, py + f64::from(RADIUS) * ZOOM - 1.0, ZOOM + 2.0, ZOOM + 2.0);
    let _ = cr.stroke();

    let (r, g, b) = (
        image.get_pixel(center_px as u32, center_py as u32)[0],
        image.get_pixel(center_px as u32, center_py as u32)[1],
        image.get_pixel(center_px as u32, center_py as u32)[2],
    );

    // Readout: a colour chip + hex string.
    let ry = py + loupe_size;
    cr.set_source_rgb(f64::from(r) / 255.0, f64::from(g) / 255.0, f64::from(b) / 255.0);
    cr.rectangle(px, ry + 4.0, 20.0, 20.0);
    let _ = cr.fill();

    cr.select_font_face("monospace", cairo::FontSlant::Normal, cairo::FontWeight::Bold);
    cr.set_font_size(13.0);
    cr.set_source_rgba(1.0, 1.0, 1.0, 0.95);
    let hex = format!("#{r:02X}{g:02X}{b:02X}");
    cr.move_to(px + 28.0, ry + 19.0);
    let _ = cr.show_text(&hex);

    // History swatches row.
    if !history.is_empty() {
        let hy = ry + readout_h;
        for (i, (hr, hg, hb)) in history.iter().enumerate() {
            let hx = px + i as f64 * (swatch + swatch_gap);
            if hx + swatch > px + panel_w {
                break;
            }
            cr.set_source_rgb(
                f64::from(*hr) / 255.0,
                f64::from(*hg) / 255.0,
                f64::from(*hb) / 255.0,
            );
            cr.rectangle(hx, hy, swatch, swatch);
            let _ = cr.fill();
            cr.set_source_rgba(1.0, 1.0, 1.0, 0.3);
            cr.set_line_width(1.0);
            cr.rectangle(hx, hy, swatch, swatch);
            let _ = cr.stroke();
        }
    }
}

/// Path to the persisted colour history file.
fn history_path() -> Option<std::path::PathBuf> {
    let base = std::env::var_os("XDG_DATA_HOME")
        .map(std::path::PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| std::path::PathBuf::from(h).join(".local/share")))?;
    Some(base.join("wayle").join("color-history"))
}

/// Loads the recently-picked swatches (`#RRGGBB` per line), newest first.
fn load_history() -> Vec<(u8, u8, u8)> {
    let Some(path) = history_path() else {
        return Vec::new();
    };
    let Ok(text) = std::fs::read_to_string(path) else {
        return Vec::new();
    };
    text.lines().filter_map(parse_hex).take(HISTORY_MAX).collect()
}

/// Persists the swatches, one `#RRGGBB` per line (best-effort).
fn save_history(history: &[(u8, u8, u8)]) {
    let Some(path) = history_path() else {
        return;
    };
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    let body: String = history
        .iter()
        .map(|(r, g, b)| format!("#{r:02X}{g:02X}{b:02X}\n"))
        .collect();
    let _ = std::fs::write(path, body);
}

/// Parses a `#RRGGBB` line into an RGB triple.
fn parse_hex(line: &str) -> Option<(u8, u8, u8)> {
    let hex = line.trim().strip_prefix('#')?;
    if hex.len() != 6 {
        return None;
    }
    let r = u8::from_str_radix(&hex[0..2], 16).ok()?;
    let g = u8::from_str_radix(&hex[2..4], 16).ok()?;
    let b = u8::from_str_radix(&hex[4..6], 16).ok()?;
    Some((r, g, b))
}

#[cfg(test)]
mod tests {
    use image::RgbImage;
    use relm4::gtk::cairo;

    use super::{draw_loupe, parse_hex};

    #[test]
    fn parses_valid_hex() {
        assert_eq!(parse_hex("#FF8000"), Some((255, 128, 0)));
        assert_eq!(parse_hex("  #000000 "), Some((0, 0, 0)));
    }

    #[test]
    fn rejects_bad_hex() {
        assert_eq!(parse_hex("FF8000"), None);
        assert_eq!(parse_hex("#FFF"), None);
        assert_eq!(parse_hex("#GGGGGG"), None);
    }

    /// Renders the loupe offscreen over a synthetic gradient and writes a PNG
    /// for eyeballing (the interactive overlay can't be driven on headless
    /// sway). Asserts the loupe actually painted pixels — i.e. the draw code
    /// runs and produces output rather than leaving the surface blank.
    #[test]
    fn loupe_renders_to_png() {
        // 120x120 red→green horizontal, green→blue vertical gradient.
        let src = RgbImage::from_fn(120, 120, |x, y| {
            image::Rgb([
                (x * 255 / 119) as u8,
                (y * 255 / 119) as u8,
                ((x + y) * 255 / 238) as u8,
            ])
        });

        let surface = cairo::ImageSurface::create(cairo::Format::ARgb32, 800, 600).unwrap();
        let cr = cairo::Context::new(&surface).unwrap();
        // Light backdrop so the loupe panel stands out in the dump.
        cr.set_source_rgb(0.9, 0.9, 0.9);
        cr.paint().unwrap();

        let history = [(255, 0, 0), (0, 128, 255), (16, 200, 64)];
        draw_loupe(&cr, &src, (120, 120), 400.0, 300.0, 800, 600, &history);
        drop(cr);

        // Convert the cairo surface (premultiplied BGRA, little-endian ARGB32)
        // into an RgbImage and save via the `image` crate — cairo's own
        // `write_to_png` needs a feature we don't enable.
        let stride = surface.stride() as usize;
        let (w, h) = (800usize, 600usize);
        let data = surface.take_data().unwrap();
        let mut img = RgbImage::new(w as u32, h as u32);
        for y in 0..h {
            for x in 0..w {
                let i = y * stride + x * 4;
                img.put_pixel(
                    x as u32,
                    y as u32,
                    image::Rgb([data[i + 2], data[i + 1], data[i]]),
                );
            }
        }
        // Confirm the loupe painted over the backdrop near the cursor.
        let probe = img.get_pixel(430, 330);
        let painted = probe.0 != [230, 230, 230];
        drop(data);
        img.save("/tmp/wayle-loupe-test.png").unwrap();
        assert!(painted, "loupe did not paint over the backdrop");
    }
}
