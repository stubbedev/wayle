//! Animating a dropdown's size inside a popover that cannot be resized.
//!
//! # Why this is not just `set_size_request`
//!
//! A mapped dropdown popover is a Wayland `xdg_popup` holding an input grab.
//! GTK cannot resize such a surface in place — it destroys and recreates it,
//! which fires `GtkPopover::closed` and tears the popover down mid-interaction.
//! Confirmed at the protocol level with `WAYLAND_DEBUG=1`: the trace shows an
//! outgoing `xdg_popup.destroy()` with **no** `popup_done` from the
//! compositor. The client tears it down; nobody dismissed it.
//!
//! Two corollaries cost real debugging time before, and both still hold:
//!
//! - `set_width_request` / `set_height_request` on a popover are *floors, not
//!   sizes*. The popover still measures to its child's natural size, so any
//!   content wider than the request silently widens the surface and destroys
//!   the popup;
//! - consequently a label that can hold unbounded text (branch name, device
//!   name, track title, file path) needs a capped natural width —
//!   `set_ellipsize` plus `set_max_width_chars`, or `NaturalWrapMode::None`.
//!   Ellipsizing alone is not enough; an ellipsized label still reports the
//!   full string as its natural width.
//!
//! # The shape that works
//!
//! The surface is sized **once**, before it is mapped, to the tallest page the
//! content could ever show — then it never changes for that mapped lifetime.
//! Inside it, the card animates.
//!
//! ```text
//! ┌─ popover surface (fixed = tallest page) ─┐
//! │ ┌─ card (animates) ─┐                    │
//! │ │ visible page      │                    │
//! │ └───────────────────┘                    │
//! │            (transparent)                 │
//! └──────────────────────────────────────────┘
//! ```
//!
//! [`measure`] finds that height by measuring every page of every
//! [`gtk::Stack`] in the content, not just the visible one. The popover's
//! measurement comes from a spacer, so the card is free to be smaller than the
//! surface without dragging the surface down with it — the popover's own
//! background is unset (`popover.dropdown > contents { all: unset }`), so the
//! card *is* the visible surface and shrinking it reads as the popover
//! resizing.
//!
//! This also retires a rule: multi-page dropdowns previously had to be
//! `vhomogeneous`/`hhomogeneous`, because a stack that re-measures on page
//! change resized the popover and closed it. The surface no longer tracks the
//! stack, so pages may differ in size.

use std::cell::Cell;

use gtk::prelude::*;
use relm4::{gtk, gtk::glib};
use tracing::debug;

/// The natural size of `content`, widened and heightened to fit every page it
/// could show.
///
/// The visible page is what GTK measures; a dropdown that swaps pages needs
/// room for the biggest one, or switching to it would clip. Returns
/// `(width, height)`.
#[must_use]
pub fn measure(content: &gtk::Widget) -> (i32, i32) {
    let (_, natural, _, _) = content.measure(gtk::Orientation::Horizontal, -1);
    let width = natural.max(1);
    let (_, height, _, _) = content.measure(gtk::Orientation::Vertical, width);

    // Every stack page measured at the settled width, so a page's height is
    // the height it will actually be allocated.
    let pages: Vec<(i32, i32)> = stacks(content)
        .into_iter()
        .map(|stack| {
            let visible = stack.measure(gtk::Orientation::Vertical, width).1;
            (visible, tallest_page(&stack, width))
        })
        .collect();

    (width, surface_height(height.max(1), &pages))
}

/// How tall the surface has to be for content that measures `content_height`
/// with the pages it is showing now.
///
/// `pages` is one `(visible page height, tallest page height)` pair per stack.
/// The room a stack needs beyond what it currently occupies is the difference
/// between its tallest page and its visible one; the surface needs the worst
/// such case, not their sum — swapping one stack's page does not swap
/// another's.
#[must_use]
fn surface_height(content_height: i32, pages: &[(i32, i32)]) -> i32 {
    // Clamped at zero: a stack whose hidden pages are all *shorter* than the
    // visible one needs no extra room, and a negative delta here would
    // shrink the surface below what the visible page already occupies.
    let extra = pages
        .iter()
        .map(|(visible, tallest)| tallest.saturating_sub(*visible).max(0))
        .max()
        .unwrap_or(0);
    content_height.saturating_add(extra)
}

/// The tallest natural height across a stack's pages, at `width`.
fn tallest_page(stack: &gtk::Stack, width: i32) -> i32 {
    let pages = stack.pages();
    let mut tallest = 0;
    for index in 0..pages.n_items() {
        let Some(page) = pages.item(index).and_downcast::<gtk::StackPage>() else {
            continue;
        };
        let child = page.child();
        // An unmapped page still measures: `measure` is not gated on being
        // realized, which is what makes sizing for a page nobody has opened
        // yet possible at all.
        let (_, natural, _, _) = child.measure(gtk::Orientation::Vertical, width);
        tallest = tallest.max(natural);
    }
    tallest
}

/// Every [`gtk::Stack`] in the tree below `widget`, itself included.
fn stacks(widget: &gtk::Widget) -> Vec<gtk::Stack> {
    let mut found = Vec::new();
    if let Some(stack) = widget.downcast_ref::<gtk::Stack>() {
        found.push(stack.clone());
    }
    let mut child = widget.first_child();
    while let Some(current) = child {
        found.extend(stacks(&current));
        child = current.next_sibling();
    }
    found
}

/// Tweens `card`'s height request from its current height to `target` over
/// `duration_ms`, then hands the height back to GTK.
///
/// A zero duration jumps, which is what `animations.enabled = false` means.
/// The height request is cleared at the end so the card goes back to
/// measuring itself — leaving it pinned would freeze the card at whatever the
/// last page needed.
pub fn animate_height(card: &gtk::Widget, target: i32, duration_ms: u32) {
    let from = card.height();
    if from <= 0 || target <= 0 || from == target {
        card.set_height_request(-1);
        return;
    }
    if duration_ms == 0 {
        card.set_height_request(-1);
        return;
    }

    debug!(from, target, duration_ms, "animating dropdown card height");
    card.set_height_request(from);

    let start = Cell::new(None::<i64>);
    let card_for_clear = card.clone();
    card.add_tick_callback(move |card, clock| {
        let now = clock.frame_time();
        let begun = match start.get() {
            Some(begun) => begun,
            None => {
                start.set(Some(now));
                now
            }
        };
        // frame_time is microseconds.
        let elapsed = (now - begun).max(0) / 1000;
        let progress = (elapsed as f64 / f64::from(duration_ms)).clamp(0.0, 1.0);
        let eased = ease_out_cubic(progress);

        let height = f64::from(from) + (f64::from(target) - f64::from(from)) * eased;
        card.set_height_request(height.round() as i32);

        if progress >= 1.0 {
            // Back to self-measuring, so the next page change starts from the
            // real height rather than a pinned one.
            card_for_clear.set_height_request(-1);
            return glib::ControlFlow::Break;
        }
        glib::ControlFlow::Continue
    });
}

/// Matches the revealer's own easing so a size change and a reveal read as one
/// motion.
fn ease_out_cubic(progress: f64) -> f64 {
    let inverted = 1.0 - progress;
    1.0 - inverted * inverted * inverted
}

/// Re-runs `on_change` whenever any stack in `content` switches page.
///
/// Returns the number of stacks watched, so a caller can tell "nothing to
/// watch" from "watching".
pub fn watch_pages(content: &gtk::Widget, on_change: impl Fn() + Clone + 'static) -> usize {
    let found = stacks(content);
    for stack in &found {
        let on_change = on_change.clone();
        stack.connect_visible_child_notify(move |_| on_change());
    }
    found.len()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_easing_pins_its_endpoints_and_only_rises() {
        assert!((ease_out_cubic(0.0) - 0.0).abs() < f64::EPSILON);
        assert!((ease_out_cubic(1.0) - 1.0).abs() < f64::EPSILON);
        let mut previous = -1.0;
        for step in 0..=20 {
            let value = ease_out_cubic(f64::from(step) / 20.0);
            assert!(value > previous, "not monotonic at {step}");
            previous = value;
        }
    }

    #[test]
    fn a_taller_hidden_page_buys_the_surface_room_for_itself() {
        // The whole point: the surface cannot grow once mapped, so a page
        // nobody has opened yet has to be paid for up front.
        assert_eq!(surface_height(400, &[(100, 260)]), 560);
    }

    #[test]
    fn a_page_that_needs_no_more_room_adds_none() {
        // Content with no stack at all is the common case and must be
        // untouched, or every single-page dropdown grows for no reason.
        assert_eq!(surface_height(400, &[]), 400);
        // The tallest page being the visible one adds nothing.
        assert_eq!(surface_height(400, &[(260, 260)]), 400);
        // A *shorter* hidden page must not shrink the surface below what the
        // visible page already needs.
        assert_eq!(surface_height(400, &[(260, 100)]), 400);
    }

    #[test]
    fn two_stacks_take_the_worst_case_not_the_sum() {
        // Switching one stack's page does not switch another's, so reserving
        // both deltas would leave a permanent dead band at the bottom.
        assert_eq!(surface_height(400, &[(100, 160), (100, 200)]), 500);
    }

    #[test]
    fn ease_out_cubic_front_loads_the_motion() {
        // "Out" easing means most of the distance is covered early; a linear
        // ramp here would make a size change read as a lurch at the end.
        assert!(ease_out_cubic(0.5) > 0.5);
    }
}
