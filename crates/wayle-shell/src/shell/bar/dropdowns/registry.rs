use std::{
    any::Any,
    cell::{Cell, RefCell},
    collections::HashMap,
    rc::Rc,
    sync::Arc,
    time::Duration,
};

use gtk::prelude::*;
use gtk4_layer_shell::{KeyboardMode, LayerShell};
use relm4::{gtk, prelude::*};
use tracing::{debug, warn};
use wayle_config::{
    ClickAction,
    schemas::{
        animations::{AnimSurface, AnimationType},
        bar::Location,
    },
};
use wayle_audio::volume::types::Volume;
use wayle_brightness::{BacklightDevice, Percentage};
use wayle_widgets::prelude::{BarButton, BarButtonInput};

use crate::{process, shell::services::ShellServices};

/// Returns `value` unchanged, logging at debug if it is `None`.
///
/// Use inside dropdown factories that gate on a service dependency: instead of
/// returning `None` silently, the helper records which dropdown failed and the
/// service it was waiting on, so the dispatch-site catch-all has the cause
/// already in the log before it runs.
pub(crate) fn require_service<T>(
    dropdown: &'static str,
    service: &'static str,
    value: Option<T>,
) -> Option<T> {
    if value.is_none() {
        debug!(
            dropdown,
            service, "service unavailable, dropdown disabled on this system"
        );
    }
    value
}

/// Shared dropdown instance for a dropdown name.
///
/// Reuse keeps dropdown state consistent across modules that reference the same
/// dropdown and avoids rebuilding the same component repeatedly.
pub(crate) struct DropdownInstance {
    popover: gtk::Popover,
    /// Wraps the popover content so enter/exit animations can be played. The
    /// popover keeps its own size request, so the revealer animates content
    /// within stable geometry rather than resizing the popover surface.
    revealer: gtk::Revealer,
    _controller: Box<dyn Any>,
    thaw_target: Rc<Cell<Option<relm4::Sender<BarButtonInput>>>>,
    original_height: Cell<Option<i32>>,
}

impl DropdownInstance {
    pub(crate) fn new(popover: gtk::Popover, controller: Box<dyn Any>) -> Self {
        let thaw_target: Rc<Cell<Option<relm4::Sender<BarButtonInput>>>> = Rc::default();
        let original_height = Cell::new(None);

        // Re-parent the popover's content under a revealer so show/hide can be
        // animated like the notification and toast surfaces.
        let revealer = gtk::Revealer::new();
        revealer.set_reveal_child(true);
        if let Some(child) = popover.child() {
            popover.set_child(None::<&gtk::Widget>);
            revealer.set_child(Some(&child));
        }
        popover.set_child(Some(&revealer));

        popover.connect_map(|popover| {
            debug!(
                width = popover.width(),
                height = popover.height(),
                autohide = popover.is_autohide(),
                classes = ?popover.css_classes(),
                "popover mapped"
            );
        });

        let thaw = thaw_target.clone();
        popover.connect_closed(move |popover| {
            debug!(
                width = popover.width(),
                height = popover.height(),
                autohide = popover.is_autohide(),
                classes = ?popover.css_classes(),
                "popover closed"
            );
            let frozen_sender = thaw.take();

            if let Some(sender) = &frozen_sender {
                sender.emit(BarButtonInput::ThawSize);
            }

            if frozen_sender.is_some()
                && let Some(parent) = popover.parent()
            {
                parent.set_size_request(-1, -1);
            }

            set_bar_keyboard_mode(popover, KeyboardMode::None);
        });

        popover.connect_notify_local(Some("height-request"), move |popover, _| {
            let Some(parent) = popover.parent() else {
                return;
            };
            let Some(root) = parent.root().and_then(|r| r.downcast::<gtk::Window>().ok()) else {
                return;
            };
            let display = gtk::prelude::WidgetExt::display(&root);
            let monitor = if let Some(native) = root.dynamic_cast_ref::<gtk::Native>() {
                native
                    .surface()
                    .and_then(|surface| display.monitor_at_surface(&surface))
            } else {
                None
            };
            let monitor = monitor.or_else(|| {
                let monitors = display.monitors();
                if monitors.n_items() > 0 {
                    monitors.item(0).and_downcast::<gtk::gdk::Monitor>()
                } else {
                    None
                }
            });

            if let Some(monitor) = monitor {
                let monitor_height = monitor.geometry().height();
                let max_allowed_height = monitor_height - 100;

                Self::find_and_clamp_scrolled_windows(popover.upcast_ref(), max_allowed_height);

                let current = popover.height_request();
                if current > max_allowed_height {
                    popover.set_height_request(max_allowed_height);
                }
            }
        });

        Self {
            popover,
            revealer,
            _controller: controller,
            thaw_target,
            original_height,
        }
    }

    /// Plays the enter animation: collapse instantly, then reveal on the next
    /// main-loop tick so the transition actually runs (a same-tick false→true
    /// does not animate). With animations disabled this reveals immediately.
    fn animate_in(&self, style: &DropdownStyle) {
        let (duration, transition) = style.enter;
        self.revealer.set_transition_type(transition);
        self.revealer.set_transition_duration(duration);

        if duration == 0 {
            self.revealer.set_reveal_child(true);
            return;
        }

        self.revealer.set_reveal_child(false);
        let revealer = self.revealer.clone();
        gtk::glib::idle_add_local_once(move || revealer.set_reveal_child(true));
    }

    /// Plays the exit animation, then pops the popover down once it finishes.
    ///
    /// Only programmatic closes (toggling the bar button, re-anchoring) run
    /// through here; native autohide outside-clicks close the popover instantly
    /// because they rely on the Wayland popup grab, which has no exit hook.
    fn animate_out(&self, style: &DropdownStyle) {
        let (duration, transition) = style.exit;
        if duration == 0 {
            self.popover.popdown();
            return;
        }

        self.revealer.set_transition_type(transition);
        self.revealer.set_transition_duration(duration);
        self.revealer.set_reveal_child(false);

        let popover = self.popover.clone();
        gtk::glib::timeout_add_local_once(Duration::from_millis(u64::from(duration)), move || {
            popover.popdown();
        });
    }

    /// Toggles popover visibility for the given bar button.
    ///
    /// If the popover is already open for this button, it closes; otherwise it
    /// opens anchored to the current button. Margins are applied from the
    /// registry so individual dropdowns never handle positioning.
    fn toggle_for(&self, bar_button: &Controller<BarButton>, style: DropdownStyle) {
        let widget = bar_button.widget();
        let widget_ref = widget.upcast_ref::<gtk::Widget>();
        let visible = self.popover.is_visible();
        let same_parent = self.popover.parent().as_ref() == Some(widget_ref);

        debug!(
            visible,
            same_parent,
            has_parent = self.popover.parent().is_some(),
            classes = ?self.popover.css_classes(),
            "toggle_for"
        );

        if visible && same_parent {
            self.animate_out(&style);
            return;
        }

        if visible {
            self.reparent_and_show(bar_button, style);
            return;
        }

        self.ensure_parent(widget_ref);
        self.freeze_and_show(bar_button, style);
    }

    /// Toggles popover visibility anchored to an arbitrary widget.
    ///
    /// Unlike `toggle_for`, this does not freeze/thaw a `BarButton` or lock
    /// parent size.
    fn toggle_for_widget(&self, widget: &impl IsA<gtk::Widget>, style: DropdownStyle) {
        let widget_ref = widget.upcast_ref::<gtk::Widget>();
        let same_parent = self.popover.parent().as_ref() == Some(widget_ref);

        if self.popover.is_visible() && same_parent {
            self.animate_out(&style);
            return;
        }

        self.ensure_parent(widget_ref);
        self.show_for_widget(style);
    }

    fn show_for_widget(&self, style: DropdownStyle) {
        self.apply_position();
        self.apply_margins(style.margins);
        self.apply_style(&style);
        self.clamp_height();
        set_bar_keyboard_mode(&self.popover, KeyboardMode::OnDemand);
        debug!(
            classes = ?self.popover.css_classes(),
            autohide = self.popover.is_autohide(),
            parent_size = ?self.popover.parent().map(|p| (p.width(), p.height())),
            "popup (widget path)"
        );
        self.popover.popup();
        self.animate_in(&style);
    }

    fn reparent_and_show(&self, bar_button: &Controller<BarButton>, style: DropdownStyle) {
        if let Some(sender) = self.thaw_target.take() {
            sender.emit(BarButtonInput::ThawSize);
        }
        self.ensure_parent(bar_button.widget().upcast_ref());
        self.freeze_and_show(bar_button, style);
    }

    fn ensure_parent(&self, target: &gtk::Widget) {
        if self.popover.parent().as_ref() == Some(target) {
            return;
        }
        if self.popover.parent().is_some() {
            self.popover.unparent();
        }
        self.popover.set_parent(target);

        let popover = self.popover.downgrade();
        target.connect_destroy(move |destroyed| {
            let Some(popover) = popover.upgrade() else {
                return;
            };
            if popover.parent().as_ref() == Some(destroyed) {
                popover.unparent();
            }
        });
    }

    fn freeze_and_show(&self, bar_button: &Controller<BarButton>, style: DropdownStyle) {
        if style.freeze_label {
            self.thaw_target.set(Some(bar_button.sender().clone()));
            bar_button.emit(BarButtonInput::FreezeSize);
            self.lock_parent_size();
        }

        self.apply_position();
        self.apply_margins(style.margins);
        self.apply_style(&style);
        self.clamp_height();
        set_bar_keyboard_mode(&self.popover, KeyboardMode::OnDemand);
        debug!(
            classes = ?self.popover.css_classes(),
            autohide = self.popover.is_autohide(),
            parent_size = ?self.popover.parent().map(|p| (p.width(), p.height())),
            "popup (button path)"
        );
        self.popover.popup();
        self.animate_in(&style);
    }

    fn clamp_height(&self) {
        let current_height = self.popover.height_request();

        if self.original_height.get().is_none() && current_height > 0 {
            self.original_height.set(Some(current_height));
        }

        let height_to_clamp = self.original_height.get().unwrap_or(current_height);

        let Some(parent) = self.popover.parent() else {
            return;
        };
        let Some(root) = parent.root().and_then(|r| r.downcast::<gtk::Window>().ok()) else {
            return;
        };
        let display = gtk::prelude::WidgetExt::display(&root);
        let monitor = if let Some(native) = root.dynamic_cast_ref::<gtk::Native>() {
            native
                .surface()
                .and_then(|surface| display.monitor_at_surface(&surface))
        } else {
            None
        };
        let monitor = monitor.or_else(|| {
            let monitors = display.monitors();
            if monitors.n_items() > 0 {
                monitors.item(0).and_downcast::<gtk::gdk::Monitor>()
            } else {
                None
            }
        });

        if let Some(monitor) = monitor {
            let monitor_height = monitor.geometry().height();
            let max_allowed_height = monitor_height - 100;

            Self::find_and_clamp_scrolled_windows(self.popover.upcast_ref(), max_allowed_height);

            let height_to_check = if height_to_clamp > 0 {
                height_to_clamp
            } else {
                self.popover.preferred_size().1.height()
            };

            if height_to_check > max_allowed_height {
                self.popover.set_height_request(max_allowed_height);
                debug!(
                    clamped_height = max_allowed_height,
                    original_height = height_to_check,
                    "clamped popover height request"
                );
            } else if height_to_clamp > 0 {
                self.popover.set_height_request(height_to_clamp);
            } else {
                self.popover.set_height_request(-1);
            }
        }
    }

    fn find_and_clamp_scrolled_windows(widget: &gtk::Widget, max_allowed_height: i32) {
        if let Some(scrolled) = widget.downcast_ref::<gtk::ScrolledWindow>() {
            let content_max = max_allowed_height - 80;
            let current_min = scrolled.min_content_height();
            if current_min > content_max {
                scrolled.set_min_content_height(content_max);
            }
            scrolled.set_max_content_height(content_max);
            scrolled.set_propagate_natural_height(true);
            return;
        }
        let mut child = widget.first_child();
        while let Some(c) = child {
            Self::find_and_clamp_scrolled_windows(&c, max_allowed_height);
            child = c.next_sibling();
        }
    }

    fn apply_style(&self, style: &DropdownStyle) {
        self.popover.set_autohide(style.autohide);
        if style.shadow_enabled {
            self.popover.add_css_class("shadow");
        } else {
            self.popover.remove_css_class("shadow");
        }
    }

    fn apply_position(&self) {
        let Some(parent) = self.popover.parent() else {
            return;
        };
        let position = Self::detect_popover_position(&parent);
        self.popover.set_position(position);

        for class in &[
            "position-top",
            "position-bottom",
            "position-left",
            "position-right",
        ] {
            self.popover.remove_css_class(class);
        }
        let class = match position {
            gtk::PositionType::Top => "position-top",
            gtk::PositionType::Bottom => "position-bottom",
            gtk::PositionType::Left => "position-left",
            gtk::PositionType::Right => "position-right",
            _ => "position-bottom",
        };
        self.popover.add_css_class(class);
    }

    fn apply_margins(&self, margins: DropdownMargins) {
        let Some(child) = self.popover.child() else {
            return;
        };
        child.set_margin_top(margins.top);
        child.set_margin_bottom(margins.bottom);
        child.set_margin_start(margins.start);
        child.set_margin_end(margins.end);
    }

    fn lock_parent_size(&self) {
        let Some(parent) = self.popover.parent() else {
            return;
        };
        parent.set_size_request(parent.width(), parent.height());
    }

    fn detect_popover_position(widget: &gtk::Widget) -> gtk::PositionType {
        let Some(window) = widget.root().and_then(|r| r.downcast::<gtk::Window>().ok()) else {
            return gtk::PositionType::Bottom;
        };

        if window.has_css_class("bottom") {
            gtk::PositionType::Top
        } else if window.has_css_class("left") {
            gtk::PositionType::Right
        } else if window.has_css_class("right") {
            gtk::PositionType::Left
        } else {
            gtk::PositionType::Bottom
        }
    }
}

impl Drop for DropdownInstance {
    fn drop(&mut self) {
        if self.popover.parent().is_some() {
            self.popover.unparent();
        }
    }
}

struct DropdownStyle {
    margins: DropdownMargins,
    shadow_enabled: bool,
    autohide: bool,
    freeze_label: bool,
    /// `(duration_ms, transition)` for the enter animation.
    enter: (u32, gtk::RevealerTransitionType),
    /// `(duration_ms, transition)` for the exit animation.
    exit: (u32, gtk::RevealerTransitionType),
}

fn revealer_transition(anim: AnimationType) -> gtk::RevealerTransitionType {
    match anim {
        AnimationType::None => gtk::RevealerTransitionType::None,
        AnimationType::Fade => gtk::RevealerTransitionType::Crossfade,
        AnimationType::SlideUp => gtk::RevealerTransitionType::SlideUp,
        AnimationType::SlideDown => gtk::RevealerTransitionType::SlideDown,
        AnimationType::SlideLeft => gtk::RevealerTransitionType::SlideLeft,
        AnimationType::SlideRight => gtk::RevealerTransitionType::SlideRight,
        AnimationType::SwingUp => gtk::RevealerTransitionType::SwingUp,
        AnimationType::SwingDown => gtk::RevealerTransitionType::SwingDown,
        AnimationType::SwingLeft => gtk::RevealerTransitionType::SwingLeft,
        AnimationType::SwingRight => gtk::RevealerTransitionType::SwingRight,
    }
}

const REM_PX: f32 = 16.0;

/// Pixel margins applied to dropdown containers.
///
/// Values are rounded to whole pixels so popover content stays visually crisp.
/// The bar-facing edge gets a smaller gap; the opposite edge and sides get
/// standard content padding.
#[derive(Debug, Clone, Copy)]
struct DropdownMargins {
    top: i32,
    bottom: i32,
    start: i32,
    end: i32,
}

impl DropdownMargins {
    const GAP_REM: f32 = 0.275;
    const CONTENT_REM: f32 = 1.0;

    fn new(scale: f32, location: Location) -> Self {
        let gap = Self::round(Self::GAP_REM, scale);
        let content = Self::round(Self::CONTENT_REM, scale);

        match location {
            Location::Top => Self {
                top: gap,
                bottom: content,
                start: content,
                end: content,
            },
            Location::Bottom => Self {
                top: content,
                bottom: gap,
                start: content,
                end: content,
            },
            Location::Left => Self {
                top: content,
                bottom: content,
                start: gap,
                end: content,
            },
            Location::Right => Self {
                top: content,
                bottom: content,
                start: content,
                end: gap,
            },
        }
    }

    fn round(rem: f32, scale: f32) -> i32 {
        (rem * REM_PX * scale).round() as i32
    }
}

/// Factory trait for creating dropdown component instances.
pub(crate) trait DropdownFactory {
    /// Creates a dropdown component, returning `None` if required services are unavailable.
    fn create(services: &ShellServices) -> Option<DropdownInstance>;
}

/// Cache of dropdown instances keyed by dropdown name.
///
/// Dropdowns are created lazily on first use and reused afterward so repeated
/// interactions resolve to the same logical dropdown instance.
pub(crate) struct DropdownRegistry {
    services: ShellServices,
    cache: RefCell<HashMap<String, Rc<DropdownInstance>>>,
}

impl DropdownRegistry {
    pub(crate) fn new(services: &ShellServices) -> Self {
        Self {
            services: services.clone(),
            cache: RefCell::default(),
        }
    }

    /// Updates autohide on all cached dropdown popovers.
    pub(crate) fn set_all_autohide(&self, autohide: bool) {
        for instance in self.cache.borrow().values() {
            instance.popover.set_autohide(autohide);
        }
    }

    pub(crate) fn warm_all(&self) {
        for name in super::DROPDOWN_NAMES {
            let _ = self.get_or_create(name);
        }
    }

    #[allow(clippy::cognitive_complexity)]
    fn get_or_create(&self, name: &str) -> Option<Rc<DropdownInstance>> {
        let mut cache = self.cache.borrow_mut();
        if let Some(instance) = cache.get(name) {
            debug!(dropdown = name, "cache hit");
            return Some(instance.clone());
        }

        debug!(dropdown = name, "creating dropdown");
        let Some(raw) = super::create(name, &self.services) else {
            debug!(
                dropdown = name,
                "no instance created (factory declined, usually due to a missing service or dependency \
                 -- see preceding debug log from the factory for the specific cause)"
            );
            return None;
        };
        let instance = Rc::new(raw);
        cache.insert(name.to_owned(), instance.clone());
        debug!(dropdown = name, "dropdown cached");
        Some(instance)
    }
}

/// Dispatches a click action: toggles dropdown, runs shell command, or no-ops.
pub(crate) fn dispatch_click(
    action: &ClickAction,
    registry: &DropdownRegistry,
    bar_button: &Controller<BarButton>,
) {
    dispatch_action(action, registry, |dropdown, style| {
        dropdown.toggle_for(bar_button, style);
    });
}

/// Dispatches a click action anchored to an arbitrary widget instead of a `BarButton`.
pub(crate) fn dispatch_click_widget(
    action: &ClickAction,
    registry: &DropdownRegistry,
    widget: &impl IsA<gtk::Widget>,
) {
    dispatch_action(action, registry, |dropdown, style| {
        dropdown.toggle_for_widget(widget, style);
    });
}

#[allow(clippy::cognitive_complexity)]
fn dispatch_action(
    action: &ClickAction,
    registry: &DropdownRegistry,
    toggle: impl FnOnce(&DropdownInstance, DropdownStyle),
) {
    match action {
        ClickAction::Dropdown(name) => {
            debug!(dropdown = %name, "click: dropdown");
            if let Some(dropdown) = registry.get_or_create(name) {
                let style = dropdown_style(registry);
                toggle(&dropdown, style);
            } else {
                warn!(
                    dropdown = %name,
                    "click dropped: no dropdown available (dropdown is either unregistered or its \
                     backing service is unavailable on this system)"
                );
            }
        }
        ClickAction::Shell(cmd) => {
            // Builtin `wayle …` actions are handled in-process (no subprocess,
            // no dependence on `wayle` being on $PATH); anything else shells out.
            if try_builtin(cmd, registry) {
                debug!(command = %cmd, "click: builtin");
            } else {
                debug!(command = %cmd, "click: shell");
                process::run_if_set(cmd);
            }
        }
        ClickAction::Brightness(delta) => {
            let Some(device) = primary_backlight(registry) else {
                return;
            };
            // Floor at the configured minimum so a dimmer never scrolls fully
            // dark; reaching 0% is reserved for BrightnessToggle.
            let min = f64::from(
                registry
                    .services
                    .config
                    .config()
                    .modules
                    .brightness
                    .min_brightness
                    .get(),
            )
            .clamp(0.0, 100.0);
            let delta = *delta;
            debug!(delta, min, "click: brightness");
            relm4::spawn(async move {
                let target = (device.percentage().value() + f64::from(delta)).clamp(min, 100.0);
                if let Err(error) = device.set_percentage(Percentage::new(target)).await {
                    warn!(%error, "brightness action failed");
                }
            });
        }
        ClickAction::BrightnessToggle => {
            let Some(device) = primary_backlight(registry) else {
                return;
            };
            debug!("click: brightness toggle");
            relm4::spawn(async move {
                if let Err(error) = device.toggle_blackout().await {
                    warn!(%error, "brightness toggle failed");
                }
            });
        }
        ClickAction::None => debug!("click: none"),
    }
}

/// Resolves the primary backlight device for native brightness actions,
/// logging the reason when unavailable so the caller can bail quietly.
fn primary_backlight(registry: &DropdownRegistry) -> Option<Arc<BacklightDevice>> {
    let Some(brightness) = registry.services.brightness.as_ref() else {
        warn!("brightness action dropped: brightness service unavailable");
        return None;
    };
    let device = brightness.primary.get();
    if device.is_none() {
        warn!("brightness action dropped: no primary backlight device");
    }
    device
}

/// Routes recognized `wayle …` commands to their in-process service instead of
/// spawning a subprocess (no `wayle`-on-$PATH dependency). Each arm mirrors the
/// corresponding D-Bus daemon's call. Returns `true` when handled; anything not
/// recognized falls through to a shell-out.
fn try_builtin(cmd: &str, registry: &DropdownRegistry) -> bool {
    let parts: Vec<&str> = cmd.split_whitespace().collect();
    if parts.first() != Some(&"wayle") {
        return false;
    }
    let services = &registry.services;
    let verb = parts.get(2).copied();

    match parts.get(1).copied() {
        Some("screenshot") => {
            let Some(sender) = crate::services::screenshot::host_sender() else {
                return false;
            };
            let mode = parts.get(2).copied().unwrap_or("region").to_owned();
            let target = parts.get(3).copied().unwrap_or("").to_owned();
            let (reply, _rx) = tokio::sync::oneshot::channel();
            sender.emit(crate::shell::screenshot::ScreenshotInput::Capture {
                mode,
                target,
                reply,
            });
            true
        }
        Some("recorder") => {
            let Some(recorder) = services.recorder.as_ref() else {
                return false;
            };
            let state = recorder.state();
            match verb {
                Some("toggle") => {
                    relm4::spawn(async move {
                        state.toggle().await;
                    });
                }
                Some("start") => {
                    relm4::spawn(async move {
                        state.start().await;
                    });
                }
                Some("stop") => state.stop(),
                Some("pause") => state.set_paused(true),
                Some("resume") => state.set_paused(false),
                _ => return false,
            }
            true
        }
        Some("audio") => {
            let Some(audio) = services.audio.as_ref() else {
                return false;
            };
            match verb {
                Some("output-mute") => {
                    let Some(device) = audio.default_output.get() else {
                        return false;
                    };
                    relm4::spawn(async move {
                        let _ = device.set_mute(!device.muted.get()).await;
                    });
                }
                Some("input-mute") => {
                    let Some(device) = audio.default_input.get() else {
                        return false;
                    };
                    relm4::spawn(async move {
                        let _ = device.set_mute(!device.muted.get()).await;
                    });
                }
                Some("output-volume") => {
                    let (Some(level), Some(device)) =
                        (parts.get(3).copied(), audio.default_output.get())
                    else {
                        return false;
                    };
                    let level = level.to_owned();
                    relm4::spawn(async move {
                        let current = device.volume.get();
                        if let Some(pct) = adjusted_pct(&level, current.average() * 100.0) {
                            let vol = Volume::from_percentage(pct, current.channels());
                            let _ = device.set_volume(vol).await;
                        }
                    });
                }
                Some("input-volume") => {
                    let (Some(level), Some(device)) =
                        (parts.get(3).copied(), audio.default_input.get())
                    else {
                        return false;
                    };
                    let level = level.to_owned();
                    relm4::spawn(async move {
                        let current = device.volume.get();
                        if let Some(pct) = adjusted_pct(&level, current.average() * 100.0) {
                            let vol = Volume::from_percentage(pct, current.channels());
                            let _ = device.set_volume(vol).await;
                        }
                    });
                }
                _ => return false,
            }
            true
        }
        Some("media") => {
            let Some(media) = services.media.as_ref() else {
                return false;
            };
            let Some(player) = media.active_player() else {
                return false;
            };
            match verb {
                Some("play-pause") => {
                    relm4::spawn(async move {
                        let _ = player.play_pause().await;
                    });
                }
                Some("next") => {
                    relm4::spawn(async move {
                        let _ = player.next().await;
                    });
                }
                Some("previous") => {
                    relm4::spawn(async move {
                        let _ = player.previous().await;
                    });
                }
                _ => return false,
            }
            true
        }
        Some("idle") => {
            let state = services.idle_inhibit.state();
            let indefinite = parts.iter().any(|p| *p == "--indefinite");
            match verb {
                Some("toggle") => {
                    if state.active.get() {
                        state.disable();
                    } else {
                        state.enable(indefinite);
                    }
                }
                Some("on") => state.enable(indefinite),
                Some("off") => state.disable(),
                _ => return false,
            }
            true
        }
        Some("notify") => {
            let Some(notification) = services.notification.as_ref() else {
                return false;
            };
            match verb {
                Some("dnd") => notification.set_dnd(!notification.dnd.get()),
                _ => return false,
            }
            true
        }
        Some("wallpaper") => {
            let Some(wallpaper) = services.wallpaper.as_ref() else {
                return false;
            };
            match verb {
                Some("next") => {
                    let wallpaper = wallpaper.clone();
                    relm4::spawn(async move {
                        let _ = wallpaper.advance_cycle().await;
                    });
                }
                Some("previous") => {
                    let wallpaper = wallpaper.clone();
                    relm4::spawn(async move {
                        let _ = wallpaper.rewind_cycle().await;
                    });
                }
                Some("stop") => wallpaper.stop_cycling(),
                _ => return false,
            }
            true
        }
        _ => false,
    }
}

/// Resolves a volume level string (`"+5"`, `"-10"`, or `"50"`) against the
/// current percentage, clamped to 0–100.
fn adjusted_pct(level: &str, current_pct: f64) -> Option<f64> {
    if let Some(delta) = level.strip_prefix('+') {
        delta.parse::<f64>().ok().map(|d| (current_pct + d).clamp(0.0, 100.0))
    } else if let Some(delta) = level.strip_prefix('-') {
        delta.parse::<f64>().ok().map(|d| (current_pct - d).clamp(0.0, 100.0))
    } else {
        level.parse::<f64>().ok().map(|v| v.clamp(0.0, 100.0))
    }
}

fn set_bar_keyboard_mode(popover: &gtk::Popover, mode: KeyboardMode) {
    let Some(parent) = popover.parent() else {
        return;
    };

    let Some(window) = parent
        .root()
        .and_then(|root| root.downcast::<gtk::Window>().ok())
    else {
        return;
    };

    window.set_keyboard_mode(mode);
}

fn dropdown_style(registry: &DropdownRegistry) -> DropdownStyle {
    let config = registry.services.config.config();
    let bar = &config.bar;
    let scale = bar.scale.get().value();
    let animations = &config.animations;
    DropdownStyle {
        margins: DropdownMargins::new(scale, bar.location.get()),
        shadow_enabled: bar.dropdown_shadow.get(),
        autohide: bar.dropdown_autohide.get(),
        freeze_label: bar.dropdown_freeze_label.get(),
        enter: (
            animations.duration_for(AnimSurface::Dropdown, false),
            revealer_transition(animations.transition_for(AnimSurface::Dropdown, false)),
        ),
        exit: (
            animations.duration_for(AnimSurface::Dropdown, true),
            revealer_transition(animations.transition_for(AnimSurface::Dropdown, true)),
        ),
    }
}
