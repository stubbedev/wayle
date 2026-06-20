use std::{sync::Arc, time::Duration};

use gtk4_layer_shell::{Edge, LayerShell};
use relm4::{ComponentSender, gtk};
use wayle_audio::core::device::{input::InputDevice, output::OutputDevice};
use wayle_brightness::BacklightDevice;
use wayle_config::schemas::{
    animations::{AnimSurface, AnimationType},
    osd::{OsdMonitor, OsdPosition, OsdTextAlign},
};

use super::{
    BRIGHTNESS_ICON, Osd, messages,
    messages::{OsdCmd, OsdEvent},
    watchers,
};
use crate::{
    i18n::t,
    shell::helpers::layer_shell::{
        apply_layer as apply_window_layer, apply_monitor_by_connector, apply_primary_monitor,
        reset_anchors,
    },
};

impl Osd {
    pub(super) fn show_event(
        &mut self,
        event: OsdEvent,
        sender: &ComponentSender<Self>,
        root: &gtk::Window,
    ) {
        if !self.ready {
            return;
        }

        let duration_override = match &event {
            OsdEvent::Custom { duration_ms, .. } => *duration_ms,
            _ => None,
        };

        self.current_event = Some(event);
        self.dismiss_id = self.dismiss_id.wrapping_add(1);

        // Toasts and OSD events share this single window but can target
        // different positions/monitors/layers, so re-anchor per event before
        // mapping it.
        self.apply_position(root);
        self.apply_layer(root);

        // Map the window, then reveal the child so the revealer animates it in.
        // Both are model-driven; the view applies them via `#[watch]`.
        self.visible = true;
        self.revealed = true;

        let duration = duration_override.unwrap_or_else(|| {
            let config = self.config.config();
            if matches!(self.current_event, Some(OsdEvent::Custom { .. })) {
                config.toasts.duration.get()
            } else {
                config.osd.duration.get()
            }
        });
        Self::schedule_dismiss(sender, duration, self.dismiss_id);
    }

    /// Begins the exit animation for `dismiss_id`, then schedules the window to
    /// hide once the animation has finished.
    pub(super) fn begin_dismiss(&mut self, dismiss_id: u32, sender: &ComponentSender<Self>) {
        if dismiss_id != self.dismiss_id {
            return;
        }
        self.revealed = false;
        let hide_after = anim_duration(self);
        sender.oneshot_command(async move {
            tokio::time::sleep(Duration::from_millis(u64::from(hide_after))).await;
            OsdCmd::Hide(dismiss_id)
        });
    }

    /// Unmaps the window after the exit animation, unless a newer event has
    /// re-shown the OSD in the meantime.
    pub(super) fn finish_hide(&mut self, dismiss_id: u32) {
        if dismiss_id == self.dismiss_id {
            self.visible = false;
        }
    }

    pub(super) fn handle_show_toast(
        &mut self,
        toast: crate::services::widget_ipc::ToastRequest,
        sender: &ComponentSender<Self>,
        root: &gtk::Window,
    ) {
        // Resolve a named preset (if any), then let explicit request fields
        // override the preset's values.
        let preset = toast.preset.as_ref().and_then(|id| {
            self.config
                .config()
                .toasts
                .presets
                .get()
                .into_iter()
                .find(|p| &p.id == id)
        });

        let label = toast
            .label
            .or_else(|| preset.as_ref().and_then(|p| p.label.clone()))
            .unwrap_or_default();
        let icon = toast
            .icon
            .or_else(|| preset.as_ref().and_then(|p| p.icon.clone()));
        let percentage = toast
            .percentage
            .or_else(|| preset.as_ref().and_then(|p| p.percentage));
        let duration_ms = toast
            .duration_ms
            .or_else(|| preset.as_ref().and_then(|p| p.duration_ms));
        let class = toast
            .class
            .or_else(|| preset.as_ref().and_then(|p| p.class.clone()));

        self.show_event(
            OsdEvent::Custom {
                label,
                icon,
                percentage,
                duration_ms,
                class,
            },
            sender,
            root,
        );
    }

    pub(super) fn handle_device_changed(
        &mut self,
        device: Option<Arc<OutputDevice>>,
        sender: &ComponentSender<Self>,
    ) {
        let token = self.device_watcher.reset();

        if let Some(device) = &device {
            watchers::spawn_device_watchers(sender, device, token);
        }
    }

    pub(super) fn handle_volume_changed(
        &mut self,
        sender: &ComponentSender<Self>,
        root: &gtk::Window,
    ) {
        let Some(audio) = &self.audio else {
            return;
        };

        let Some(device) = audio.default_output.get() else {
            return;
        };

        let percentage = device.volume.get().average_percentage();
        let muted = device.muted.get();
        let rounded = percentage.round() as u32;

        let snapshot = (rounded, muted);

        if self.last_volume == Some(snapshot) {
            return;
        }

        self.last_volume = Some(snapshot);

        let description = device.description.get();
        let icon = volume_icon(percentage, muted);

        let event = OsdEvent::Slider {
            label: description,
            icon: icon.to_string(),
            percentage,
            muted,
        };

        self.show_event(event, sender, root);
    }

    pub(super) fn handle_brightness_device_changed(
        &mut self,
        device: Option<Arc<BacklightDevice>>,
        sender: &ComponentSender<Self>,
    ) {
        let token = self.brightness_watcher.reset();

        if let Some(device) = &device {
            watchers::spawn_brightness_watcher(sender, device, token);
        }
    }

    pub(super) fn handle_brightness_changed(
        &mut self,
        sender: &ComponentSender<Self>,
        root: &gtk::Window,
    ) {
        let Some(brightness) = &self.brightness else {
            return;
        };

        let Some(device) = brightness.primary.get() else {
            return;
        };

        let percentage = device.percentage().value();
        let rounded = percentage.round() as u32;

        if self.last_brightness == Some(rounded) {
            return;
        }

        self.last_brightness = Some(rounded);

        let event = OsdEvent::Slider {
            label: device.name.to_string(),
            icon: BRIGHTNESS_ICON.to_string(),
            percentage,
            muted: false,
        };

        self.show_event(event, sender, root);
    }

    pub(super) fn handle_input_device_changed(
        &mut self,
        device: Option<Arc<InputDevice>>,
        sender: &ComponentSender<Self>,
    ) {
        let token = self.input_device_watcher.reset();

        if let Some(device) = &device {
            watchers::spawn_input_device_watchers(sender, device, token);
        }
    }

    pub(super) fn handle_input_volume_changed(
        &mut self,
        sender: &ComponentSender<Self>,
        root: &gtk::Window,
    ) {
        let Some(audio) = &self.audio else {
            return;
        };

        let Some(device) = audio.default_input.get() else {
            return;
        };

        let percentage = device.volume.get().average_percentage();
        let muted = device.muted.get();
        let rounded = percentage.round() as u32;

        let snapshot = (rounded, muted);

        if self.last_input_volume == Some(snapshot) {
            return;
        }

        self.last_input_volume = Some(snapshot);

        let description = device.description.get();

        let icon = if muted {
            "ld-mic-off-symbolic"
        } else {
            "ld-mic-symbolic"
        };

        let event = OsdEvent::Slider {
            label: description,
            icon: icon.to_string(),
            percentage,
            muted,
        };

        self.show_event(event, sender, root);
    }

    pub(super) fn handle_toggle_changed(
        &mut self,
        toggle: messages::ToggleEvent,
        sender: &ComponentSender<Self>,
        root: &gtk::Window,
    ) {
        let (label, icon) = match toggle.key {
            messages::ToggleKey::CapsLock => (t!("osd-caps-lock"), "ld-a-large-small-symbolic"),
            messages::ToggleKey::NumLock => (t!("osd-num-lock"), "ld-hash-symbolic"),
            messages::ToggleKey::ScrollLock => (t!("osd-scroll-lock"), "ld-arrow-up-down-symbolic"),
        };

        let event = OsdEvent::Toggle {
            label,
            icon: icon.to_string(),
            active: toggle.active,
        };

        self.show_event(event, sender, root);
    }

    pub(super) fn apply_position(&self, root: &gtk::Window) {
        let config = self.config.config();
        // Toasts (`Custom`) anchor against `[toasts]`; everything else `[osd]`.
        let is_toast = matches!(self.current_event, Some(OsdEvent::Custom { .. }));
        let (position, monitor, margin_spacing, margin_base) = if is_toast {
            (
                config.toasts.position.get(),
                config.toasts.monitor.get(),
                config.toasts.margin.get(),
                wayle_config::schemas::toasts::MARGIN_BASE_REM,
            )
        } else {
            (
                config.osd.position.get(),
                config.osd.monitor.get(),
                config.osd.margin.get(),
                wayle_config::schemas::osd::MARGIN_BASE_REM,
            )
        };
        let scale = config.styling.scale.get().value();
        let margin = margin_spacing.resolve_rem(margin_base, scale) as i32;

        reset_anchors(root);

        match position {
            OsdPosition::TopLeft => {
                root.set_anchor(Edge::Top, true);
                root.set_anchor(Edge::Left, true);
                root.set_margin(Edge::Top, margin);
                root.set_margin(Edge::Left, margin);
            }

            OsdPosition::Top => {
                root.set_anchor(Edge::Top, true);
                root.set_margin(Edge::Top, margin);
            }

            OsdPosition::TopRight => {
                root.set_anchor(Edge::Top, true);
                root.set_anchor(Edge::Right, true);
                root.set_margin(Edge::Top, margin);
                root.set_margin(Edge::Right, margin);
            }

            OsdPosition::Right => {
                root.set_anchor(Edge::Right, true);
                root.set_margin(Edge::Right, margin);
            }

            OsdPosition::BottomRight => {
                root.set_anchor(Edge::Bottom, true);
                root.set_anchor(Edge::Right, true);
                root.set_margin(Edge::Bottom, margin);
                root.set_margin(Edge::Right, margin);
            }

            OsdPosition::Bottom => {
                root.set_anchor(Edge::Bottom, true);
                root.set_margin(Edge::Bottom, margin);
            }

            OsdPosition::BottomLeft => {
                root.set_anchor(Edge::Bottom, true);
                root.set_anchor(Edge::Left, true);
                root.set_margin(Edge::Bottom, margin);
                root.set_margin(Edge::Left, margin);
            }

            OsdPosition::Left => {
                root.set_anchor(Edge::Left, true);
                root.set_margin(Edge::Left, margin);
            }
        }

        match &monitor {
            OsdMonitor::Primary => apply_primary_monitor(root),
            OsdMonitor::Connector(name) => {
                apply_monitor_by_connector(root, name);
            }
        }
    }

    pub(super) fn apply_layer(&self, root: &gtk::Window) {
        let config = self.config.config();
        let configured = if matches!(self.current_event, Some(OsdEvent::Custom { .. })) {
            config.toasts.layer.get()
        } else {
            config.osd.layer.get()
        };
        apply_window_layer(root, configured, &self.config);
    }

    pub(super) fn schedule_dismiss(
        sender: &ComponentSender<Osd>,
        duration_ms: u32,
        dismiss_id: u32,
    ) {
        sender.oneshot_command(async move {
            tokio::time::sleep(Duration::from_millis(duration_ms as u64)).await;
            OsdCmd::Dismiss(dismiss_id)
        });
    }
}

/// The animation surface for the current event: toasts (`Custom`) resolve
/// against `[animations.toast]`, everything else against `[animations.osd]`.
fn anim_surface(model: &Osd) -> AnimSurface {
    match model.current_event {
        Some(OsdEvent::Custom { .. }) => AnimSurface::Toast,
        _ => AnimSurface::Osd,
    }
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

/// Revealer transition for the current direction. `revealed` means entering;
/// otherwise exiting. Resolved per-surface with the global fallback cascade.
pub(super) fn anim_transition(model: &Osd) -> gtk::RevealerTransitionType {
    let anim = model
        .config
        .config()
        .animations
        .transition_for(anim_surface(model), !model.revealed);
    revealer_transition(anim)
}

/// Animation duration in ms for the current direction (`0` when disabled).
pub(super) fn anim_duration(model: &Osd) -> u32 {
    model
        .config
        .config()
        .animations
        .duration_for(anim_surface(model), !model.revealed)
}

pub(super) fn osd_classes(model: &Osd) -> Vec<String> {
    let mut classes = vec![String::from("osd")];

    let is_toast = matches!(model.current_event, Some(OsdEvent::Custom { .. }));

    if model
        .current_event
        .as_ref()
        .is_some_and(|event| matches!(event, OsdEvent::Slider { muted: true, .. }))
    {
        classes.push(String::from("muted"));
    }

    if model
        .current_event
        .as_ref()
        .is_some_and(|event| matches!(event, OsdEvent::Toggle { active: false, .. }))
    {
        classes.push(String::from("toggle-off"));
    }

    let bordered = if is_toast {
        model.config.config().toasts.border.get()
    } else {
        model.config.config().osd.border.get()
    };
    if bordered {
        classes.push(String::from("bordered"));
    }

    if let Some(OsdEvent::Custom {
        class: Some(class), ..
    }) = &model.current_event
    {
        classes.push(class.clone());
    }

    classes
}

pub(super) fn is_slider(event: &Option<OsdEvent>) -> bool {
    event.as_ref().is_some_and(|event| {
        matches!(
            event,
            OsdEvent::Slider { .. }
                | OsdEvent::Custom {
                    percentage: Some(_),
                    ..
                }
        )
    })
}

pub(super) fn is_toggle(event: &Option<OsdEvent>) -> bool {
    event.as_ref().is_some_and(|event| {
        matches!(
            event,
            OsdEvent::Toggle { .. }
                | OsdEvent::Custom {
                    percentage: None,
                    ..
                }
        )
    })
}

/// Horizontal alignment for the toast/toggle header. Toasts read
/// `toasts.text-align`; OSD toggles read `osd.text-align`.
pub(super) fn toast_align(model: &Osd) -> gtk::Align {
    let config = model.config.config();
    let align = if matches!(model.current_event, Some(OsdEvent::Custom { .. })) {
        config.toasts.text_align.get()
    } else {
        config.osd.text_align.get()
    };
    match align {
        OsdTextAlign::Start => gtk::Align::Start,
        OsdTextAlign::Center => gtk::Align::Center,
        OsdTextAlign::End => gtk::Align::End,
    }
}

pub(super) fn event_icon(event: &Option<OsdEvent>) -> Option<&str> {
    match event {
        Some(OsdEvent::Slider { icon, .. }) | Some(OsdEvent::Toggle { icon, .. }) => {
            Some(icon.as_str())
        }
        Some(OsdEvent::Custom { icon, .. }) => icon.as_deref(),
        None => None,
    }
}

pub(super) fn event_slider_label(event: &Option<OsdEvent>) -> String {
    match event {
        Some(OsdEvent::Slider { label, .. })
        | Some(OsdEvent::Custom {
            label,
            percentage: Some(_),
            ..
        }) => label.clone(),
        _ => String::new(),
    }
}

pub(super) fn event_label(event: &Option<OsdEvent>) -> String {
    match event {
        Some(OsdEvent::Slider { label, .. }) => label.clone(),

        Some(OsdEvent::Toggle {
            label,
            active: true,
            ..
        }) => t!("osd-toggle-on", label = label.clone()),

        Some(OsdEvent::Toggle {
            label,
            active: false,
            ..
        }) => t!("osd-toggle-off", label = label.clone()),

        Some(OsdEvent::Custom {
            label,
            percentage: None,
            ..
        }) => label.clone(),

        _ => String::new(),
    }
}

pub(super) fn event_value(event: &Option<OsdEvent>) -> String {
    match event {
        Some(OsdEvent::Slider { percentage, .. })
        | Some(OsdEvent::Custom {
            percentage: Some(percentage),
            ..
        }) => format!("{}%", percentage.round() as u32),
        _ => String::new(),
    }
}

pub(super) fn event_fraction(event: &Option<OsdEvent>) -> f64 {
    match event {
        Some(OsdEvent::Slider { percentage, .. })
        | Some(OsdEvent::Custom {
            percentage: Some(percentage),
            ..
        }) => (*percentage / 100.0).clamp(0.0, 1.0),
        _ => 0.0,
    }
}

fn volume_icon(percentage: f64, muted: bool) -> &'static str {
    if muted || percentage <= 0.0 {
        "ld-volume-x-symbolic"
    } else if percentage < 34.0 {
        "ld-volume-symbolic"
    } else if percentage < 67.0 {
        "ld-volume-1-symbolic"
    } else {
        "ld-volume-2-symbolic"
    }
}
