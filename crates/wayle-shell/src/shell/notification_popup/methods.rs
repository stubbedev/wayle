use std::sync::Arc;

use gtk::prelude::*;
use gtk4_layer_shell::{Edge, LayerShell};
use relm4::{Component, ComponentController, gtk};
use tracing::debug;
use wayle_config::schemas::{
    animations::AnimationType,
    modules::notification::{PopupMonitor, PopupPosition, StackingOrder},
};
use wayle_notification::core::notification::Notification;

use super::{
    NotificationPopupHost,
    card::{CardInit, NotificationPopupCard},
    messages::PopupHostCmd,
};
use crate::shell::helpers::layer_shell::{
    apply_layer as apply_window_layer, apply_monitor_by_connector, apply_primary_monitor,
    reset_anchors,
};

impl NotificationPopupHost {
    /// Reconciles the card list with the current popup state.
    pub(super) fn reconcile(
        &mut self,
        popups: Vec<Arc<Notification>>,
        root: &gtk::Window,
        sender: &relm4::ComponentSender<Self>,
    ) {
        let max_visible = self
            .config
            .config()
            .modules
            .notifications
            .popup_max_visible
            .get() as usize;

        let mut visible_popups = popups;
        visible_popups.truncate(max_visible);

        self.remove_stale_cards(&visible_popups);

        let existing_ids: Vec<u32> = self.cards.iter().map(|(notif, _, _)| notif.id).collect();

        self.insert_new_cards(&visible_popups, &existing_ids);

        debug!(cards = self.cards.len(), "popup reconcile complete");

        if visible_popups.is_empty() {
            // Keep the window mapped while the last card animates out, then hide
            // it once the transition finishes. Hiding now would unmap the window
            // mid-fade and skip the exit animation. The generation guard lets a
            // popup arriving during the fade cancel this pending hide.
            self.hide_gen = self.hide_gen.wrapping_add(1);
            let generation = self.hide_gen;
            let (duration, _) = self.animation();
            if duration == 0 {
                root.set_visible(false);
            } else {
                sender.oneshot_command(async move {
                    tokio::time::sleep(std::time::Duration::from_millis(u64::from(duration))).await;
                    PopupHostCmd::HideWindow(generation)
                });
            }
        } else {
            // Cancel any pending hide and ensure the window is mapped.
            self.hide_gen = self.hide_gen.wrapping_add(1);
            root.set_visible(true);
        }
    }

    fn remove_stale_cards(&mut self, active_popups: &[Arc<Notification>]) {
        let (duration, _) = self.animation();
        let mut kept = Vec::with_capacity(self.cards.len());

        for (stored_notif, controller, revealer) in std::mem::take(&mut self.cards) {
            let still_active = active_popups
                .iter()
                .any(|popup| popup.id == stored_notif.id);

            if still_active {
                kept.push((stored_notif, controller, revealer));
                continue;
            }

            // Animate out, then remove the widget once the transition finishes.
            // The closure runs on the main thread, so it can touch GTK widgets,
            // and it owns the controller + revealer to keep them alive (and the
            // card visible) for the duration of the fade.
            revealer.set_reveal_child(false);
            let container = self.card_container.clone();
            gtk::glib::timeout_add_local_once(
                std::time::Duration::from_millis(u64::from(duration)),
                move || {
                    container.remove(&revealer);
                    drop(controller);
                },
            );
        }

        self.cards = kept;
    }

    /// Animation `(duration_ms, transition)` from the animations config.
    /// Disabled animations collapse to an instant `(0, None)` transition.
    fn animation(&self) -> (u32, gtk::RevealerTransitionType) {
        let config = self.config.config();
        let animations = &config.animations;
        if !animations.enabled.get() {
            return (0, gtk::RevealerTransitionType::None);
        }
        let transition = match animations.transition.get() {
            AnimationType::None => gtk::RevealerTransitionType::None,
            AnimationType::Fade => gtk::RevealerTransitionType::Crossfade,
            AnimationType::SlideUp => gtk::RevealerTransitionType::SlideUp,
            AnimationType::SlideDown => gtk::RevealerTransitionType::SlideDown,
            AnimationType::SlideLeft => gtk::RevealerTransitionType::SlideLeft,
            AnimationType::SlideRight => gtk::RevealerTransitionType::SlideRight,
        };
        (animations.duration.get(), transition)
    }

    fn insert_new_cards(&mut self, popups: &[Arc<Notification>], existing_ids: &[u32]) {
        let config = self.config.config();
        let notif_config = &config.modules.notifications;

        let hover_pause = notif_config.popup_hover_pause.get();
        let close_behavior = notif_config.popup_close_behavior.get();
        let urgency_bar = notif_config.popup_urgency_bar.get();
        let icon_source = notif_config.icon_source.get();
        let shadow = notif_config.popup_shadow.get();
        let stacking_order = notif_config.popup_stacking_order.get();
        let use_prepend = matches!(stacking_order, StackingOrder::NewestFirst);

        for notif in popups {
            if existing_ids.contains(&notif.id) {
                continue;
            }

            let controller = NotificationPopupCard::builder()
                .launch(CardInit {
                    notification: notif.clone(),
                    service: self.notification.clone(),
                    config: self.config.clone(),
                    hover_pause,
                    close_behavior,
                    urgency_bar,
                    icon_source,
                    shadow,
                })
                .detach();

            let (duration, transition) = self.animation();
            let revealer = gtk::Revealer::new();
            revealer.set_transition_type(transition);
            revealer.set_transition_duration(duration);
            revealer.set_child(Some(controller.widget()));
            // Start collapsed, then reveal on the next main-loop tick so the
            // transition actually plays (a same-tick false→true does not animate).
            revealer.set_reveal_child(false);
            let reveal_target = revealer.clone();
            gtk::glib::idle_add_local_once(move || reveal_target.set_reveal_child(true));

            if use_prepend {
                self.card_container.prepend(&revealer);
                self.cards.insert(0, (notif.clone(), controller, revealer));
            } else {
                self.card_container.append(&revealer);
                self.cards.push((notif.clone(), controller, revealer));
            }
        }
    }

    /// Applies layer-shell anchors, margins, and monitor based on config.
    pub(super) fn apply_position(&self, root: &gtk::Window) {
        let config = self.config.config();
        let notif_config = &config.modules.notifications;
        let position = notif_config.popup_position.get();
        let scale = config.styling.scale.get().value();
        let mx = (notif_config.popup_margin_x.get().value() * scale) as i32;
        let my = (notif_config.popup_margin_y.get().value() * scale) as i32;

        reset_anchors(root);

        match position {
            PopupPosition::TopLeft => {
                root.set_anchor(Edge::Top, true);
                root.set_anchor(Edge::Left, true);
                root.set_margin(Edge::Top, my);
                root.set_margin(Edge::Left, mx);
            }

            PopupPosition::TopCenter => {
                root.set_anchor(Edge::Top, true);
                root.set_margin(Edge::Top, my);
            }

            PopupPosition::TopRight => {
                root.set_anchor(Edge::Top, true);
                root.set_anchor(Edge::Right, true);
                root.set_margin(Edge::Top, my);
                root.set_margin(Edge::Right, mx);
            }

            PopupPosition::BottomLeft => {
                root.set_anchor(Edge::Bottom, true);
                root.set_anchor(Edge::Left, true);
                root.set_margin(Edge::Bottom, my);
                root.set_margin(Edge::Left, mx);
            }

            PopupPosition::BottomCenter => {
                root.set_anchor(Edge::Bottom, true);
                root.set_margin(Edge::Bottom, my);
            }

            PopupPosition::BottomRight => {
                root.set_anchor(Edge::Bottom, true);
                root.set_anchor(Edge::Right, true);
                root.set_margin(Edge::Bottom, my);
                root.set_margin(Edge::Right, mx);
            }

            PopupPosition::CenterLeft => {
                root.set_anchor(Edge::Left, true);
                root.set_margin(Edge::Left, mx);
            }

            PopupPosition::CenterRight => {
                root.set_anchor(Edge::Right, true);
                root.set_margin(Edge::Right, mx);
            }
        }

        let monitor = notif_config.popup_monitor.get();

        match &monitor {
            PopupMonitor::Primary => {
                apply_primary_monitor(root);
            }
            PopupMonitor::Connector(name) => {
                apply_monitor_by_connector(root, name);
            }
        }
    }

    pub(super) fn apply_layer(&self, root: &gtk::Window) {
        let configured = self.config.config().modules.notifications.popup_layer.get();
        apply_window_layer(root, configured, &self.config);
    }
}
