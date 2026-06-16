mod card;
pub(crate) mod helpers;
pub(crate) mod messages;
mod methods;
mod templates;
mod watchers;

use std::sync::Arc;

use gtk::prelude::*;
use gtk4_layer_shell::{KeyboardMode, LayerShell};
use relm4::{gtk, prelude::*};
use wayle_config::ConfigService;
use wayle_notification::{NotificationService, core::notification::Notification};

pub(crate) use self::messages::PopupHostInit;
use self::{card::NotificationPopupCard, messages::PopupHostCmd};

pub(crate) struct NotificationPopupHost {
    notification: Arc<NotificationService>,
    config: Arc<ConfigService>,
    cards: Vec<(
        Arc<Notification>,
        Controller<NotificationPopupCard>,
        gtk::Revealer,
    )>,
    card_container: gtk::Box,
    /// Bumped each time a window hide is scheduled or cancelled, so a stale
    /// deferred hide can't unmap the window after a new popup re-shows it.
    hide_gen: u32,
}

#[relm4::component(pub(crate))]
impl Component for NotificationPopupHost {
    type Init = PopupHostInit;
    type Input = ();
    type Output = ();
    type CommandOutput = PopupHostCmd;

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            add_css_class: "notification-popup-host",
            set_default_size: (1, 1),
            set_visible: false,

            #[local_ref]
            card_container -> gtk::Box {
                add_css_class: "notification-popup-list",
                set_orientation: gtk::Orientation::Vertical,
            },
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        root.init_layer_shell();
        root.set_keyboard_mode(KeyboardMode::None);
        root.set_namespace(Some("wayle-notification-popup"));

        let config = init.config.config();
        let notif_config = &config.modules.notifications;
        let scale = config.styling.scale.get().value();
        let gap = (notif_config.popup_gap.get().value() * scale) as i32;

        init.notification
            .set_popup_duration(notif_config.popup_duration.get());

        let model = Self {
            notification: init.notification.clone(),
            config: init.config.clone(),
            cards: Vec::new(),
            card_container: gtk::Box::default(),
            hide_gen: 0,
        };

        model.apply_position(&root);
        model.apply_layer(&root);

        let card_container = &model.card_container;
        card_container.set_spacing(gap);
        let widgets = view_output!();

        watchers::spawn(&sender, &init.notification, &init.config);

        ComponentParts { model, widgets }
    }

    fn update_cmd(&mut self, msg: PopupHostCmd, sender: ComponentSender<Self>, root: &Self::Root) {
        match msg {
            PopupHostCmd::PopupsChanged(popups) => {
                self.reconcile(popups, root, &sender);
            }

            PopupHostCmd::HideWindow(generation) => {
                // Only hide if no popup re-showed the window since this hide was
                // scheduled, and nothing is on screen.
                if generation == self.hide_gen && self.cards.is_empty() {
                    root.set_visible(false);
                }
            }

            PopupHostCmd::ConfigChanged => {
                self.apply_position(root);
                self.apply_layer(root);

                let config = self.config.config();
                let notif_config = &config.modules.notifications;
                let scale = config.styling.scale.get().value();
                let gap = (notif_config.popup_gap.get().value() * scale) as i32;
                self.card_container.set_spacing(gap);

                self.notification
                    .set_popup_duration(notif_config.popup_duration.get());

                let popups = self.notification.popups.get();
                self.reconcile(popups, root, &sender);
            }
        }
    }
}
