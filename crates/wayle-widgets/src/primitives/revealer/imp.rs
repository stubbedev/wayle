use std::{
    cell::{Cell, RefCell},
    time::{Duration, Instant},
};

use gtk4::{glib, graphene, gsk, prelude::*, subclass::prelude::*};
use wayle_config::schemas::animations::AnimationType;

use super::GenieEdge;

/// Default transition duration (ms) before a caller sets one.
const DEFAULT_DURATION_MS: u32 = 200;

/// Animation step interval (~60fps). The reveal is driven by a main-loop timer
/// rather than the widget's frame clock: a slide collapses the allocation to
/// zero at progress 0, and a zero-size layer-shell surface commits no valid
/// buffer, so the compositor never delivers frame callbacks and a frame-clock
/// tick would never fire — leaving the overlay stuck invisible. A wall-clock
/// timer advances regardless, growing the surface until it renders normally.
const FRAME_INTERVAL: Duration = Duration::from_millis(16);

pub struct WayleRevealerImp {
    child: RefCell<Option<gtk4::Widget>>,
    transition: Cell<AnimationType>,
    duration_ms: Cell<u32>,
    genie_edge: Cell<GenieEdge>,
    /// Target reveal state: `true` = shown.
    reveal: Cell<bool>,
    /// Current reveal amount, `0.0` hidden .. `1.0` shown.
    progress: Cell<f64>,
    /// Reveal amount the running animation interpolates toward.
    target: Cell<f64>,
    /// `progress` captured when the current animation started.
    start_progress: Cell<f64>,
    /// Wall-clock instant the current animation started, for elapsed timing.
    anim_start: Cell<Option<Instant>>,
    /// Whether a step timer is currently in flight (drives the 1px travel-axis
    /// floor in `measure`).
    animating: Cell<bool>,
    /// Running step timer, if an animation is in flight.
    source_id: RefCell<Option<glib::SourceId>>,
}

impl Default for WayleRevealerImp {
    fn default() -> Self {
        Self {
            child: RefCell::new(None),
            transition: Cell::new(AnimationType::Fade),
            duration_ms: Cell::new(DEFAULT_DURATION_MS),
            genie_edge: Cell::new(GenieEdge::Bottom),
            reveal: Cell::new(false),
            progress: Cell::new(0.0),
            target: Cell::new(0.0),
            start_progress: Cell::new(0.0),
            anim_start: Cell::new(None),
            animating: Cell::new(false),
            source_id: RefCell::new(None),
        }
    }
}

#[glib::object_subclass]
impl ObjectSubclass for WayleRevealerImp {
    const NAME: &'static str = "WayleRevealer";
    type Type = super::WayleRevealer;
    type ParentType = gtk4::Widget;
}

impl ObjectImpl for WayleRevealerImp {
    fn dispose(&self) {
        if let Some(id) = self.source_id.take() {
            id.remove();
        }
        if let Some(child) = self.child.take() {
            child.unparent();
        }
    }
}

impl WidgetImpl for WayleRevealerImp {
    fn measure(&self, orientation: gtk4::Orientation, for_size: i32) -> (i32, i32, i32, i32) {
        let Some(child) = self.child.borrow().clone() else {
            return (0, 0, -1, -1);
        };
        let (min, nat, min_b, nat_b) = child.measure(orientation, for_size);
        let transition = self.transition.get();

        // None reserves no space while hidden, so siblings reflow at once.
        if matches!(transition, AnimationType::None) {
            return if self.reveal.get() {
                (min, nat, min_b, nat_b)
            } else {
                (0, 0, -1, -1)
            };
        }

        // Slides shrink along their travel axis so the parent reflows (sibling
        // widgets get pushed) as the surface grows/collapses — like GtkRevealer.
        // The child keeps its full size; the shrinking allocation clips it.
        //
        // While a step timer is in flight, floor the travel axis at 1px so a
        // layer-shell overlay's surface keeps a valid (non-zero) buffer and its
        // frame clock can't park mid-animation and strand the reveal. That 1px is
        // drawn fully transparent (see `snapshot`), and at rest the floor is 0 so
        // a hidden slide (e.g. a closed dropdown) leaves no sliver.
        if slide_axis(transition) == Some(orientation) {
            let shown = (f64::from(nat) * ease_in_out_cubic(self.progress.get())).round() as i32;
            let floor = i32::from(self.animating.get());
            return (0, shown.max(floor), -1, -1);
        }
        (min, nat, min_b, nat_b)
    }

    fn size_allocate(&self, width: i32, height: i32, baseline: i32) {
        let Some(child) = self.child.borrow().clone() else {
            return;
        };
        // Slides give the child its full natural size and offset it toward the
        // travel edge; the (smaller, clipped) allocation reveals a growing strip.
        match slide_axis(self.transition.get()) {
            Some(gtk4::Orientation::Vertical) => {
                let (_, nat_h, _, _) = child.measure(gtk4::Orientation::Vertical, width);
                // SlideUp enters from the bottom (anchor bottom); SlideDown from the top.
                let dy = match self.transition.get() {
                    AnimationType::SlideUp => (height - nat_h) as f32,
                    _ => 0.0,
                };
                let offset = gsk::Transform::new().translate(&graphene::Point::new(0.0, dy));
                child.allocate(width, nat_h, baseline, Some(offset));
            }
            Some(gtk4::Orientation::Horizontal) => {
                let (_, nat_w, _, _) = child.measure(gtk4::Orientation::Horizontal, height);
                // SlideLeft enters from the right (anchor right); SlideRight from the left.
                let dx = match self.transition.get() {
                    AnimationType::SlideLeft => (width - nat_w) as f32,
                    _ => 0.0,
                };
                let offset = gsk::Transform::new().translate(&graphene::Point::new(dx, 0.0));
                child.allocate(nat_w, height, baseline, Some(offset));
            }
            _ => child.allocate(width, height, baseline, None),
        }
    }

    fn snapshot(&self, snapshot: &gtk4::Snapshot) {
        let Some(child) = self.child.borrow().clone() else {
            return;
        };
        let transition = self.transition.get();
        let p = self.progress.get();
        let obj = self.obj();

        // None never tweens: show the child whenever it is meant to be revealed.
        if matches!(transition, AnimationType::None) {
            if self.reveal.get() {
                obj.snapshot_child(&child, snapshot);
            }
            return;
        }
        if p <= 0.0 {
            return;
        }
        // Slides: the animated allocation + overflow clip already produce the
        // motion (see `size_allocate`), so just draw the child — but skip it while
        // collapsed to the 1px buffer floor so that floor stays fully transparent.
        if let Some(axis) = slide_axis(transition) {
            let collapsed = match axis {
                gtk4::Orientation::Horizontal => obj.width() <= 1,
                _ => obj.height() <= 1,
            };
            if !collapsed {
                obj.snapshot_child(&child, snapshot);
            }
            return;
        }
        // Fully shown: cheap fast-path, no transform stack.
        if p >= 1.0 {
            obj.snapshot_child(&child, snapshot);
            return;
        }

        let w = obj.width() as f32;
        let h = obj.height() as f32;

        snapshot.save();
        let alpha = match transition {
            AnimationType::Fade => p as f32,
            AnimationType::Bounce => {
                let s = ease_out_back(p) as f32;
                snapshot.translate(&graphene::Point::new(w / 2.0, h / 2.0));
                snapshot.scale(s, s);
                snapshot.translate(&graphene::Point::new(-w / 2.0, -h / 2.0));
                p as f32
            }
            AnimationType::Genie => {
                apply_genie(
                    snapshot,
                    self.genie_edge.get(),
                    ease_in_out_cubic(p) as f32,
                    w,
                    h,
                );
                p as f32
            }
            AnimationType::Zoom => {
                let s = ease_in_out_cubic(p) as f32;
                snapshot.translate(&graphene::Point::new(w / 2.0, h / 2.0));
                snapshot.scale(s, s);
                snapshot.translate(&graphene::Point::new(-w / 2.0, -h / 2.0));
                p as f32
            }
            AnimationType::Rotate => {
                let t = ease_in_out_cubic(p) as f32;
                let s = 0.3 + 0.7 * t;
                snapshot.translate(&graphene::Point::new(w / 2.0, h / 2.0));
                snapshot.rotate((1.0 - t) * 180.0);
                snapshot.scale(s, s);
                snapshot.translate(&graphene::Point::new(-w / 2.0, -h / 2.0));
                p as f32
            }
            AnimationType::Flip => {
                let t = ease_in_out_cubic(p) as f32;
                snapshot.translate(&graphene::Point::new(w / 2.0, h / 2.0));
                snapshot.scale(t.max(0.001), 1.0);
                snapshot.translate(&graphene::Point::new(-w / 2.0, -h / 2.0));
                p as f32
            }
            // None and slides return before the transform stack (handled above).
            AnimationType::None
            | AnimationType::SlideUp
            | AnimationType::SlideDown
            | AnimationType::SlideLeft
            | AnimationType::SlideRight => p as f32,
            swing @ (AnimationType::SwingUp
            | AnimationType::SwingDown
            | AnimationType::SwingLeft
            | AnimationType::SwingRight) => {
                apply_swing(snapshot, swing, ease_in_out_cubic(p) as f32, w, h);
                p as f32
            }
        };
        snapshot.push_opacity(f64::from(alpha.clamp(0.0, 1.0)));
        obj.snapshot_child(&child, snapshot);
        snapshot.pop();
        snapshot.restore();
    }
}

impl WayleRevealerImp {
    pub(super) fn set_child(&self, child: Option<&gtk4::Widget>) {
        if let Some(old) = self.child.take() {
            old.unparent();
        }
        if let Some(child) = child {
            child.set_parent(&*self.obj());
            self.child.replace(Some(child.clone()));
        }
        self.obj().queue_resize();
    }

    pub(super) fn child(&self) -> Option<gtk4::Widget> {
        self.child.borrow().clone()
    }

    pub(super) fn set_transition(&self, transition: AnimationType) {
        if self.transition.get() != transition {
            self.transition.set(transition);
            // Slides clip the child to the shrinking allocation; in-place
            // effects (e.g. bounce overshoot) must draw outside it.
            let overflow = if slide_axis(transition).is_some() {
                gtk4::Overflow::Hidden
            } else {
                gtk4::Overflow::Visible
            };
            self.obj().set_overflow(overflow);
            self.obj().queue_resize();
        }
    }

    /// Redraw, also relaying out when the transition animates the allocation so
    /// the parent reflows around it.
    fn invalidate(&self) {
        if animates_size(self.transition.get()) {
            self.obj().queue_resize();
        } else {
            self.obj().queue_draw();
        }
    }

    pub(super) fn set_duration(&self, ms: u32) {
        self.duration_ms.set(ms);
    }

    pub(super) fn set_genie_edge(&self, edge: GenieEdge) {
        self.genie_edge.set(edge);
    }

    pub(super) fn reveal_child(&self) -> bool {
        self.reveal.get()
    }

    pub(super) fn set_reveal_child(&self, reveal: bool) {
        if self.reveal.get() == reveal {
            return;
        }
        self.reveal.set(reveal);
        self.start_animation(if reveal { 1.0 } else { 0.0 });
    }

    fn start_animation(&self, target: f64) {
        self.target.set(target);

        // Zero duration (animations disabled) snaps with no timer.
        if self.duration_ms.get() == 0 {
            if let Some(id) = self.source_id.take() {
                id.remove();
            }
            self.animating.set(false);
            self.progress.set(target);
            self.invalidate();
            return;
        }

        self.start_progress.set(self.progress.get());
        self.anim_start.set(Some(Instant::now()));
        self.animating.set(true);

        // A live timer already animates toward the new target/anchor — reusing it
        // (it re-reads target/start above) avoids stacking a second source.
        if self.source_id.borrow().is_some() {
            return;
        }
        let weak = self.obj().downgrade();
        let id = glib::timeout_add_local(FRAME_INTERVAL, move || {
            let Some(obj) = weak.upgrade() else {
                return glib::ControlFlow::Break;
            };
            obj.imp().step()
        });
        self.source_id.replace(Some(id));
    }

    fn step(&self) -> glib::ControlFlow {
        let duration = f64::from(self.duration_ms.get());
        let elapsed_ms = self
            .anim_start
            .get()
            .map_or(duration, |start| start.elapsed().as_secs_f64() * 1000.0);
        let frac = if duration <= 0.0 {
            1.0
        } else {
            (elapsed_ms / duration).clamp(0.0, 1.0)
        };

        let from = self.start_progress.get();
        let to = self.target.get();
        self.progress.set(from + (to - from) * frac);
        self.invalidate();

        if frac >= 1.0 {
            self.progress.set(to);
            self.animating.set(false);
            // Returning Break removes the source; drop the handle without re-removing.
            self.source_id.replace(None);
            // Drop the transparent 1px buffer floor now the animation has settled.
            self.invalidate();
            glib::ControlFlow::Break
        } else {
            glib::ControlFlow::Continue
        }
    }
}

/// Travel axis for a slide transition (the axis whose allocation is animated so
/// the parent reflows). `None` for transitions that keep a full-size slot.
fn slide_axis(transition: AnimationType) -> Option<gtk4::Orientation> {
    match transition {
        AnimationType::SlideUp | AnimationType::SlideDown => Some(gtk4::Orientation::Vertical),
        AnimationType::SlideLeft | AnimationType::SlideRight => Some(gtk4::Orientation::Horizontal),
        _ => None,
    }
}

/// Whether the transition animates the widget's allocation (so siblings are
/// pushed and the parent must relayout each frame).
fn animates_size(transition: AnimationType) -> bool {
    matches!(transition, AnimationType::None) || slide_axis(transition).is_some()
}

/// Rotate about the relevant edge for a swing transition (affine approximation
/// of GtkRevealer's swing).
fn apply_swing(snapshot: &gtk4::Snapshot, transition: AnimationType, t: f32, w: f32, h: f32) {
    let angle = (1.0 - t) * 90.0;
    let (pivot, deg) = match transition {
        AnimationType::SwingUp => (graphene::Point::new(w / 2.0, h), -angle),
        AnimationType::SwingDown => (graphene::Point::new(w / 2.0, 0.0), angle),
        AnimationType::SwingLeft => (graphene::Point::new(w, h / 2.0), angle),
        AnimationType::SwingRight => (graphene::Point::new(0.0, h / 2.0), -angle),
        _ => (graphene::Point::new(w / 2.0, h / 2.0), 0.0),
    };
    snapshot.translate(&pivot);
    snapshot.rotate(deg);
    snapshot.translate(&graphene::Point::new(-pivot.x(), -pivot.y()));
}

/// Scale the child toward its anchored edge ("suck to a point") for genie.
fn apply_genie(snapshot: &gtk4::Snapshot, edge: GenieEdge, t: f32, w: f32, h: f32) {
    // The collapsing axis goes to ~0; the cross axis only narrows a little.
    let narrow = 0.15 + 0.85 * t;
    let (pivot, sx, sy) = match edge {
        GenieEdge::Bottom => (graphene::Point::new(w / 2.0, h), narrow, t),
        GenieEdge::Top => (graphene::Point::new(w / 2.0, 0.0), narrow, t),
        GenieEdge::Left => (graphene::Point::new(0.0, h / 2.0), t, narrow),
        GenieEdge::Right => (graphene::Point::new(w, h / 2.0), t, narrow),
    };
    snapshot.translate(&pivot);
    snapshot.scale(sx.max(0.001), sy.max(0.001));
    snapshot.translate(&graphene::Point::new(-pivot.x(), -pivot.y()));
}

/// Smooth ease used by every transition except bounce.
fn ease_in_out_cubic(t: f64) -> f64 {
    if t < 0.5 {
        4.0 * t * t * t
    } else {
        let f = -2.0 * t + 2.0;
        1.0 - f * f * f / 2.0
    }
}

/// Overshoot ease — rises past `1.0` near the end then settles, the "bounce".
fn ease_out_back(t: f64) -> f64 {
    const C1: f64 = 1.70158;
    const C3: f64 = C1 + 1.0;
    let f = t - 1.0;
    1.0 + C3 * f * f * f + C1 * f * f
}

#[cfg(test)]
mod tests {
    use super::{ease_in_out_cubic, ease_out_back};

    #[test]
    fn easings_pin_endpoints() {
        for e in [ease_in_out_cubic(0.0), ease_out_back(0.0)] {
            assert!(e.abs() < 1e-9, "easing(0) must be 0, got {e}");
        }
        for e in [ease_in_out_cubic(1.0), ease_out_back(1.0)] {
            assert!((e - 1.0).abs() < 1e-9, "easing(1) must be 1, got {e}");
        }
    }

    #[test]
    fn ease_in_out_cubic_is_monotonic_and_bounded() {
        let mut prev = ease_in_out_cubic(0.0);
        for i in 1..=100 {
            let v = ease_in_out_cubic(f64::from(i) / 100.0);
            assert!(v >= prev - 1e-9, "cubic must not decrease");
            assert!(
                (0.0..=1.0).contains(&v),
                "cubic must stay in [0,1], got {v}"
            );
            prev = v;
        }
    }

    #[test]
    fn ease_out_back_overshoots() {
        // The overshoot above 1.0 is what reads as a bounce.
        let peak = (1..100)
            .map(|i| ease_out_back(f64::from(i) / 100.0))
            .fold(f64::MIN, f64::max);
        assert!(peak > 1.0, "back ease must overshoot 1.0, peak was {peak}");
    }
}
