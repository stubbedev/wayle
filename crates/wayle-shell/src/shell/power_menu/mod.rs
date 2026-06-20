//! Native power menu.
//!
//! A layer-shell overlay with lock / log out / suspend / reboot / shut down
//! buttons, replacing an external logout tool. Opened in-process from the power
//! bar button (`:menu`), animated through the shared `[animations]` system
//! ([`AnimSurface::Power`]). Each button runs a configurable command from
//! `[modules.power]`.

use std::{sync::Arc, time::Duration};

use gtk4_layer_shell::{KeyboardMode, Layer, LayerShell};
use relm4::{
    gtk,
    gtk::{glib, prelude::*, EventControllerKey},
    prelude::*,
};
use wayle_config::{
    ConfigService,
    schemas::animations::{AnimSurface, AnimationType},
};

use crate::process;

/// One power action: its icon, label, and the config command it runs.
struct PowerAction {
    icon: &'static str,
    label: &'static str,
    command: fn(&wayle_config::schemas::modules::PowerConfig) -> String,
}

const ACTIONS: &[PowerAction] = &[
    PowerAction {
        icon: "ld-lock-symbolic",
        label: "Lock",
        command: |c| c.lock_command.get(),
    },
    PowerAction {
        icon: "ld-log-out-symbolic",
        label: "Log out",
        command: |c| c.logout_command.get(),
    },
    PowerAction {
        icon: "ld-moon-symbolic",
        label: "Suspend",
        command: |c| c.suspend_command.get(),
    },
    PowerAction {
        icon: "ld-rotate-ccw-symbolic",
        label: "Reboot",
        command: |c| c.reboot_command.get(),
    },
    PowerAction {
        icon: "ld-power-symbolic",
        label: "Shut down",
        command: |c| c.shutdown_command.get(),
    },
];

/// Messages driving the power menu.
#[derive(Debug)]
pub(crate) enum PowerMenuInput {
    /// Open the menu.
    Show,
    /// Run a command and close.
    Run(String),
    /// Dismiss without acting.
    Cancel,
}

pub(crate) struct PowerMenu {
    config: Arc<ConfigService>,
}

#[relm4::component(pub(crate))]
impl Component for PowerMenu {
    type Init = Arc<ConfigService>;
    type Input = PowerMenuInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            add_css_class: "power-menu-window",
            set_visible: false,

            #[name = "revealer"]
            gtk::Revealer {
                set_reveal_child: false,

                #[name = "surface"]
                gtk::Box {
                    add_css_class: "power-menu-surface",
                    set_orientation: gtk::Orientation::Horizontal,
                    set_halign: gtk::Align::Center,
                    set_valign: gtk::Align::Center,
                    set_spacing: 12,
                },
            }
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let model = PowerMenu { config: init };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-power-menu"));
        root.set_layer(Layer::Overlay);
        root.set_keyboard_mode(KeyboardMode::Exclusive);
        root.set_exclusive_zone(-1);
        for edge in [
            gtk4_layer_shell::Edge::Top,
            gtk4_layer_shell::Edge::Bottom,
            gtk4_layer_shell::Edge::Left,
            gtk4_layer_shell::Edge::Right,
        ] {
            root.set_anchor(edge, true);
        }

        // Build the action buttons once; commands are re-read from config on click.
        for action in ACTIONS {
            let button = gtk::Button::builder()
                .css_classes(["power-menu-button"])
                .build();
            button.set_cursor_from_name(Some("pointer"));

            let content = gtk::Box::builder()
                .orientation(gtk::Orientation::Vertical)
                .spacing(6)
                .build();
            let icon = gtk::Image::from_icon_name(action.icon);
            icon.add_css_class("power-menu-icon");
            let label = gtk::Label::new(Some(action.label));
            label.add_css_class("power-menu-label");
            content.append(&icon);
            content.append(&label);
            button.set_child(Some(&content));

            let config = model.config.clone();
            let input = sender.input_sender().clone();
            let command = action.command;
            button.connect_clicked(move |_| {
                input.emit(PowerMenuInput::Run(command(&config.config().modules.power)));
            });
            widgets.surface.append(&button);
        }

        // Escape cancels.
        let input = sender.input_sender().clone();
        let key = EventControllerKey::new();
        key.connect_key_pressed(move |_, keyval, _, _| {
            if keyval == gtk::gdk::Key::Escape {
                input.emit(PowerMenuInput::Cancel);
                return glib::Propagation::Stop;
            }
            glib::Propagation::Proceed
        });
        root.add_controller(key);

        // Play the enter transition once mapped (see the share picker for why).
        let revealer = widgets.revealer.clone();
        root.connect_map(move |_| {
            let revealer = revealer.clone();
            glib::idle_add_local_once(move || revealer.set_reveal_child(true));
        });

        ComponentParts { model, widgets }
    }

    fn update_with_view(
        &mut self,
        widgets: &mut Self::Widgets,
        msg: PowerMenuInput,
        _sender: ComponentSender<Self>,
        root: &Self::Root,
    ) {
        match msg {
            PowerMenuInput::Show => self.reveal(widgets, root),
            PowerMenuInput::Run(command) => {
                process::run_if_set(&command);
                self.hide_animated(widgets, root);
            }
            PowerMenuInput::Cancel => self.hide_animated(widgets, root),
        }
    }
}

impl PowerMenu {
    fn animation(&self, exiting: bool) -> (gtk::RevealerTransitionType, u32) {
        let animations = &self.config.config().animations;
        (
            revealer_transition(animations.transition_for(AnimSurface::Power, exiting)),
            animations.duration_for(AnimSurface::Power, exiting),
        )
    }

    fn reveal(&self, widgets: &PowerMenuWidgets, root: &gtk::Window) {
        let (transition, duration) = self.animation(false);
        widgets.revealer.set_transition_type(transition);
        widgets.revealer.set_transition_duration(duration);
        widgets.revealer.set_reveal_child(false);
        root.set_visible(true);
        root.present();
    }

    fn hide_animated(&self, widgets: &PowerMenuWidgets, root: &gtk::Window) {
        let (transition, duration) = self.animation(true);
        widgets.revealer.set_transition_type(transition);
        widgets.revealer.set_transition_duration(duration);
        widgets.revealer.set_reveal_child(false);

        let root = root.clone();
        glib::timeout_add_local_once(Duration::from_millis(u64::from(duration)), move || {
            root.set_visible(false);
        });
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
