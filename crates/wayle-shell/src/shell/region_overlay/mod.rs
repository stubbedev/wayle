//! Slurp-like region-selection overlay.
//!
//! Shows a transparent layer-shell surface on every monitor; the user drags a
//! rectangle and the global selection is delivered back through a oneshot
//! channel carried in [`RegionOverlayInput::Show`]. Both the share picker and
//! the screenshot service consume it through
//! [`crate::services::region_overlay::request_region`].
//!
//! Each surface paints a dim wash with cairo and punches a transparent hole
//! where the selection rectangle overlaps that monitor. The drag state is
//! shared across all surfaces in global (compositor-layout) logical
//! coordinates so a drag can start on one monitor and end on another.

use std::{cell::RefCell, collections::HashMap, rc::Rc};

use gtk4_layer_shell::{Edge, KeyboardMode, Layer, LayerShell};
use relm4::{
    gtk,
    gtk::{cairo, gdk, glib, prelude::*, ContentFit, EventControllerKey, GestureDrag},
    prelude::*,
};
use tokio::sync::oneshot;
use tracing::debug;

use crate::shell::helpers::monitors::current_monitors;

/// A region the user selected, in logical pixels relative to `output`.
#[derive(Debug, Clone)]
pub(crate) struct RegionSelection {
    /// Connector name of the output the selection is relative to.
    pub(crate) output: String,
    pub(crate) x: i32,
    pub(crate) y: i32,
    pub(crate) width: i32,
    pub(crate) height: i32,
}

/// Messages driving the overlay.
pub(crate) enum RegionOverlayInput {
    /// Open the overlay; the chosen region (or `None` on cancel) is sent back.
    Show {
        /// Channel the selection is delivered on.
        reply: oneshot::Sender<Option<RegionSelection>>,
        /// Frozen per-output frames, keyed by connector. When a connector has a
        /// frame the surface paints it (freeze-frame, screenshot path); empty
        /// for the live path (share picker).
        frames: HashMap<String, gdk::Texture>,
    },
    /// Drag finished (`Some`) or cancelled (`None`); tears down the surfaces.
    Finish(Option<RegionSelection>),
}

impl std::fmt::Debug for RegionOverlayInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Show { frames, .. } => f
                .debug_struct("Show")
                .field("frozen_outputs", &frames.len())
                .finish_non_exhaustive(),
            Self::Finish(sel) => f.debug_tuple("Finish").field(sel).finish(),
        }
    }
}

/// A drag rectangle in global (compositor-layout) logical coordinates.
#[derive(Clone, Copy)]
struct DragRect {
    start: (f64, f64),
    end: (f64, f64),
}

impl DragRect {
    /// `(x, y, width, height)` with a positive extent regardless of direction.
    fn normalized(&self) -> (f64, f64, f64, f64) {
        let x = self.start.0.min(self.end.0);
        let y = self.start.1.min(self.end.1);
        let w = (self.end.0 - self.start.0).abs();
        let h = (self.end.1 - self.start.1).abs();
        (x, y, w, h)
    }
}

/// A monitor's connector name and global logical geometry.
#[derive(Clone)]
struct MonitorGeom {
    connector: String,
    x: i32,
    y: i32,
    width: i32,
    height: i32,
}

/// The region-selection overlay component.
pub(crate) struct RegionOverlay {
    reply: Option<oneshot::Sender<Option<RegionSelection>>>,
    /// Live overlay surfaces, one per monitor; drained on finish.
    surfaces: Vec<gtk::Window>,
    /// Shared drag state read by every surface's draw func.
    drag: Rc<RefCell<Option<DragRect>>>,
    /// Drawing areas to repaint as the drag moves.
    areas: Rc<RefCell<Vec<gtk::DrawingArea>>>,
}

#[relm4::component(pub(crate))]
impl Component for RegionOverlay {
    type Init = ();
    type Input = RegionOverlayInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        // Placeholder root: the real surfaces are layer-shell windows created
        // per monitor on `Show`. This window is never mapped.
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
        let model = RegionOverlay {
            reply: None,
            surfaces: Vec::new(),
            drag: Rc::new(RefCell::new(None)),
            areas: Rc::new(RefCell::new(Vec::new())),
        };
        let widgets = view_output!();
        let _ = &sender;
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: RegionOverlayInput, sender: ComponentSender<Self>, _root: &Self::Root) {
        match msg {
            RegionOverlayInput::Show { reply, frames } => {
                // Drop any previous, unanswered request.
                if let Some(prev) = self.reply.take() {
                    let _ = prev.send(None);
                }
                self.close();
                self.reply = Some(reply);
                self.open(&sender, &frames);
            }
            RegionOverlayInput::Finish(selection) => {
                if let Some(reply) = self.reply.take() {
                    let _ = reply.send(selection);
                }
                self.close();
            }
        }
    }
}

impl RegionOverlay {
    /// Builds and shows one layer-shell surface per monitor. When `frames` has
    /// a frozen frame for a connector the surface paints it behind the dim wash
    /// (freeze-frame); otherwise the surface is transparent and the live screen
    /// shows through the punched hole.
    fn open(&mut self, sender: &ComponentSender<Self>, frames: &HashMap<String, gdk::Texture>) {
        let monitors = current_monitors();
        let geoms: Rc<Vec<MonitorGeom>> = Rc::new(
            monitors
                .iter()
                .map(|(connector, monitor)| {
                    let g = monitor.geometry();
                    MonitorGeom {
                        connector: connector.clone(),
                        x: g.x(),
                        y: g.y(),
                        width: g.width(),
                        height: g.height(),
                    }
                })
                .collect(),
        );

        for (connector, monitor) in monitors {
            let g = monitor.geometry();
            let offset = (g.x() as f64, g.y() as f64);
            debug!(connector = %connector, x = g.x(), y = g.y(), "region overlay surface");

            let window = gtk::Window::builder().decorated(false).build();
            window.add_css_class("region-overlay-window");
            window.init_layer_shell();
            window.set_namespace(Some("wayle-region-overlay"));
            window.set_layer(Layer::Overlay);
            window.set_monitor(Some(&monitor));
            window.set_keyboard_mode(KeyboardMode::Exclusive);
            window.set_exclusive_zone(-1);
            for edge in [Edge::Top, Edge::Bottom, Edge::Left, Edge::Right] {
                window.set_anchor(edge, true);
            }

            let area = gtk::DrawingArea::new();
            area.add_css_class("region-overlay-area");
            area.set_hexpand(true);
            area.set_vexpand(true);
            area.set_cursor_from_name(Some("crosshair"));
            Self::attach_draw_func(&area, &self.drag, offset);

            // Freeze-frame path: paint the captured frame behind the dim wash.
            // The draw func clears the selection to transparent, revealing the
            // bright frame underneath. Without a frame the surface stays
            // transparent and the live screen shows through (share picker).
            match frames.get(&connector) {
                Some(texture) => {
                    let picture = gtk::Picture::for_paintable(texture);
                    picture.set_content_fit(ContentFit::Fill);
                    let stack = gtk::Overlay::new();
                    stack.set_child(Some(&picture));
                    stack.add_overlay(&area);
                    window.set_child(Some(&stack));
                }
                None => window.set_child(Some(&area)),
            }
            self.areas.borrow_mut().push(area.clone());

            self.attach_drag_gesture(&area, sender, offset, &geoms);
            Self::attach_escape(&window, sender);

            window.present();
            self.surfaces.push(window);
        }
    }

    /// Tears down every live surface and resets shared state.
    fn close(&mut self) {
        for window in self.surfaces.drain(..) {
            window.destroy();
        }
        self.areas.borrow_mut().clear();
        *self.drag.borrow_mut() = None;
    }

    /// Wires the cairo draw func that dims the surface and punches the hole.
    fn attach_draw_func(
        area: &gtk::DrawingArea,
        drag: &Rc<RefCell<Option<DragRect>>>,
        offset: (f64, f64),
    ) {
        let drag = drag.clone();
        area.set_draw_func(move |area, cr, _w, _h| {
            // Dim wash across the whole monitor.
            cr.set_operator(cairo::Operator::Source);
            cr.set_source_rgba(0.0, 0.0, 0.0, 0.35);
            let _ = cr.paint();

            let Some(rect) = *drag.borrow() else {
                return;
            };
            let (gx, gy, gw, gh) = rect.normalized();
            let lx = gx - offset.0;
            let ly = gy - offset.1;

            // Clear the selection to fully transparent so the screen shows.
            cr.set_operator(cairo::Operator::Clear);
            cr.rectangle(lx, ly, gw, gh);
            let _ = cr.fill();

            // Accent border around the selection. The accent is the area's
            // themed `color` (set via `.region-overlay-area` in SCSS), so it
            // follows the active palette/theme provider.
            let accent = area.color();
            cr.set_operator(cairo::Operator::Over);
            cr.set_source_rgba(
                f64::from(accent.red()),
                f64::from(accent.green()),
                f64::from(accent.blue()),
                f64::from(accent.alpha()),
            );
            cr.set_line_width(2.0);
            cr.rectangle(lx, ly, gw, gh);
            let _ = cr.stroke();

            // Size label: "<width> × <height>" in logical pixels, drawn in the
            // accent color on a dark backing box near the selection's top-left.
            let label = format!("{} × {}", gw.round() as i32, gh.round() as i32);
            cr.select_font_face("monospace", cairo::FontSlant::Normal, cairo::FontWeight::Bold);
            cr.set_font_size(14.0);
            if let Ok(ext) = cr.text_extents(&label) {
                let pad = 6.0;
                let box_w = ext.width() + pad * 2.0;
                let box_h = ext.height() + pad * 2.0;
                let bx = lx;
                // Above the rectangle if there is room, otherwise just inside it.
                let by = if ly - box_h - 4.0 >= 0.0 {
                    ly - box_h - 4.0
                } else {
                    ly + 4.0
                };

                cr.set_source_rgba(0.0, 0.0, 0.0, 0.75);
                cr.rectangle(bx, by, box_w, box_h);
                let _ = cr.fill();

                cr.set_source_rgba(
                    f64::from(accent.red()),
                    f64::from(accent.green()),
                    f64::from(accent.blue()),
                    f64::from(accent.alpha()),
                );
                cr.move_to(bx + pad - ext.x_bearing(), by + pad - ext.y_bearing());
                let _ = cr.show_text(&label);
            }
        });
    }

    /// Wires a drag gesture that updates the shared rect and replies on end.
    fn attach_drag_gesture(
        &self,
        area: &gtk::DrawingArea,
        sender: &ComponentSender<Self>,
        offset: (f64, f64),
        geoms: &Rc<Vec<MonitorGeom>>,
    ) {
        let gesture = GestureDrag::new();

        {
            let drag = self.drag.clone();
            let areas = self.areas.clone();
            gesture.connect_drag_begin(move |_, sx, sy| {
                let g = (offset.0 + sx, offset.1 + sy);
                *drag.borrow_mut() = Some(DragRect { start: g, end: g });
                redraw(&areas);
            });
        }
        {
            let drag = self.drag.clone();
            let areas = self.areas.clone();
            gesture.connect_drag_update(move |_, ox, oy| {
                if let Some(rect) = drag.borrow_mut().as_mut() {
                    rect.end = (rect.start.0 + ox, rect.start.1 + oy);
                }
                redraw(&areas);
            });
        }
        {
            let drag = self.drag.clone();
            let geoms = geoms.clone();
            let input = sender.input_sender().clone();
            gesture.connect_drag_end(move |_, ox, oy| {
                let current = *drag.borrow();
                let selection = current.and_then(|mut rect| {
                    rect.end = (rect.start.0 + ox, rect.start.1 + oy);
                    finalize(&rect, &geoms)
                });
                input.emit(RegionOverlayInput::Finish(selection));
            });
        }

        area.add_controller(gesture);
    }

    /// Wires Escape on a surface to cancel the whole overlay.
    fn attach_escape(window: &gtk::Window, sender: &ComponentSender<Self>) {
        let input = sender.input_sender().clone();
        let key = EventControllerKey::new();
        key.connect_key_pressed(move |_, keyval, _, _| {
            if keyval == gdk::Key::Escape {
                input.emit(RegionOverlayInput::Finish(None));
                return glib::Propagation::Stop;
            }
            glib::Propagation::Proceed
        });
        window.add_controller(key);
    }
}

/// Repaints every active surface.
fn redraw(areas: &Rc<RefCell<Vec<gtk::DrawingArea>>>) {
    for area in areas.borrow().iter() {
        area.queue_draw();
    }
}

/// Resolves a global drag rectangle to an output-relative [`RegionSelection`].
///
/// The output is the one containing the rectangle's top-left corner (falling
/// back to the first monitor); coordinates are made relative to that output to
/// match `slurp`'s `<output>@x,y,w,h` and
/// `OutputManager::capture_output_region` semantics.
fn finalize(rect: &DragRect, geoms: &[MonitorGeom]) -> Option<RegionSelection> {
    let (gx, gy, gw, gh) = rect.normalized();
    if gw < 1.0 || gh < 1.0 {
        return None;
    }
    let gx = gx.round() as i32;
    let gy = gy.round() as i32;
    let gw = gw.round() as i32;
    let gh = gh.round() as i32;

    let monitor = geoms
        .iter()
        .find(|m| gx >= m.x && gx < m.x + m.width && gy >= m.y && gy < m.y + m.height)
        .or_else(|| geoms.first())?;

    Some(RegionSelection {
        output: monitor.connector.clone(),
        x: gx - monitor.x,
        y: gy - monitor.y,
        width: gw,
        height: gh,
    })
}
