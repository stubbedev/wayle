mod factory;
mod messages;
mod watchers;

use std::sync::Arc;

use gtk::prelude::*;
use relm4::{gtk, prelude::*};
use wayle_config::{
    ConfigService,
    schemas::modules::{EncoderPreset, WebcamPosition},
};
use wayle_widgets::prelude::*;

pub(super) use self::factory::Factory;
use self::messages::{RecorderDropdownCmd, RecorderDropdownInit, RecorderDropdownMsg};
use crate::{i18n::t, services::recorder::RecorderState, shell::bar::dropdowns::resolve_dimension};

const BASE_WIDTH: f32 = 360.0;
const MIN_BITRATE: f64 = 500.0;
const MAX_BITRATE: f64 = 50_000.0;
const BITRATE_STEP: f64 = 500.0;
const MIN_AUDIO_BITRATE: f64 = 16.0;
const MAX_AUDIO_BITRATE: f64 = 512.0;
const AUDIO_BITRATE_STEP: f64 = 16.0;

pub(crate) struct RecorderDropdown {
    config: Arc<ConfigService>,
    state: RecorderState,
    scaled_width: i32,
    active: bool,
    paused: bool,
}

#[relm4::component(pub(crate))]
impl Component for RecorderDropdown {
    type Init = RecorderDropdownInit;
    type Input = RecorderDropdownMsg;
    type Output = ();
    type CommandOutput = RecorderDropdownCmd;

    view! {
        #[root]
        gtk::Popover {
            set_css_classes: &["dropdown", "recorder-dropdown"],
            set_has_arrow: false,
            #[watch]
            set_width_request: model.scaled_width,

            #[template]
            Dropdown {
                #[template]
                DropdownHeader {
                    #[template_child]
                    icon {
                        set_visible: true,
                        set_icon_name: Some("ld-video-symbolic"),
                    },
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-recorder-title"),
                    },
                },

                #[template]
                DropdownContent {
                    add_css_class: "recorder-dropdown-content",

                    gtk::Box {
                        add_css_class: "recorder-controls",
                        set_spacing: 8,

                        gtk::Button {
                            add_css_class: "recorder-record-button",
                            set_hexpand: true,
                            #[watch]
                            set_label: &if model.active {
                                t!("dropdown-recorder-stop")
                            } else {
                                t!("dropdown-recorder-record")
                            },
                            connect_clicked => RecorderDropdownMsg::ToggleRecording,
                        },

                        gtk::Button {
                            add_css_class: "recorder-pause-button",
                            #[watch]
                            set_sensitive: model.active,
                            #[watch]
                            set_label: &if model.paused {
                                t!("dropdown-recorder-resume")
                            } else {
                                t!("dropdown-recorder-pause")
                            },
                            connect_clicked => RecorderDropdownMsg::TogglePause,
                        },
                    },

                    gtk::Box {
                        add_css_class: "recorder-row",
                        gtk::Label {
                            set_hexpand: true,
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-microphone"),
                        },
                        #[template]
                        Switch {
                            #[block_signal(mic_toggle)]
                            set_active: model.config.config().modules.recorder.microphone.get(),
                            connect_state_set[sender] => move |switch, active| {
                                sender.input(RecorderDropdownMsg::MicrophoneToggled(active));
                                switch.set_state(active);
                                gtk::glib::Propagation::Stop
                            } @mic_toggle,
                        },
                    },

                    gtk::Box {
                        add_css_class: "recorder-row",
                        gtk::Label {
                            set_hexpand: true,
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-system-audio"),
                        },
                        #[template]
                        Switch {
                            #[block_signal(sys_toggle)]
                            set_active: model.config.config().modules.recorder.system_audio.get(),
                            connect_state_set[sender] => move |switch, active| {
                                sender.input(RecorderDropdownMsg::SystemAudioToggled(active));
                                switch.set_state(active);
                                gtk::glib::Propagation::Stop
                            } @sys_toggle,
                        },
                    },

                    gtk::Box {
                        add_css_class: "recorder-row",
                        gtk::Label {
                            set_hexpand: true,
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-webcam"),
                        },
                        #[template]
                        Switch {
                            #[block_signal(cam_toggle)]
                            set_active: model.config.config().modules.recorder.webcam_enabled.get(),
                            connect_state_set[sender] => move |switch, active| {
                                sender.input(RecorderDropdownMsg::WebcamToggled(active));
                                switch.set_state(active);
                                gtk::glib::Propagation::Stop
                            } @cam_toggle,
                        },
                    },

                    gtk::Box {
                        add_css_class: "recorder-row",
                        gtk::Label {
                            set_hexpand: true,
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-position"),
                        },
                        gtk::DropDown {
                            set_model: Some(&gtk::StringList::new(&[
                                "Top Left",
                                "Top Right",
                                "Bottom Left",
                                "Bottom Right",
                            ])),
                            set_selected: position_index(
                                model.config.config().modules.recorder.webcam_position.get(),
                            ),
                            connect_selected_notify[sender] => move |dropdown| {
                                sender.input(RecorderDropdownMsg::PositionSelected(dropdown.selected()));
                            },
                        },
                    },

                    gtk::Box {
                        add_css_class: "recorder-row",
                        gtk::Label {
                            set_hexpand: true,
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-bitrate"),
                        },
                        gtk::SpinButton {
                            set_adjustment: &gtk::Adjustment::new(
                                f64::from(model.config.config().modules.recorder.bitrate_kbps.get()),
                                MIN_BITRATE,
                                MAX_BITRATE,
                                BITRATE_STEP,
                                BITRATE_STEP,
                                0.0,
                            ),
                            connect_value_changed[sender] => move |spin| {
                                sender.input(RecorderDropdownMsg::BitrateChanged(spin.value() as u32));
                            },
                        },
                    },

                    gtk::Box {
                        add_css_class: "recorder-row",
                        gtk::Label {
                            set_hexpand: true,
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-audio-bitrate"),
                        },
                        gtk::SpinButton {
                            set_adjustment: &gtk::Adjustment::new(
                                f64::from(model.config.config().modules.recorder.audio_bitrate_kbps.get()),
                                MIN_AUDIO_BITRATE,
                                MAX_AUDIO_BITRATE,
                                AUDIO_BITRATE_STEP,
                                AUDIO_BITRATE_STEP,
                                0.0,
                            ),
                            connect_value_changed[sender] => move |spin| {
                                sender.input(RecorderDropdownMsg::AudioBitrateChanged(spin.value() as u32));
                            },
                        },
                    },

                    gtk::Box {
                        add_css_class: "recorder-row",
                        gtk::Label {
                            set_hexpand: true,
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-separate-tracks"),
                        },
                        #[template]
                        Switch {
                            #[block_signal(sep_toggle)]
                            set_active: model.config.config().modules.recorder.separate_audio_tracks.get(),
                            connect_state_set[sender] => move |switch, active| {
                                sender.input(RecorderDropdownMsg::SeparateTracksToggled(active));
                                switch.set_state(active);
                                gtk::glib::Propagation::Stop
                            } @sep_toggle,
                        },
                    },

                    gtk::Box {
                        add_css_class: "recorder-row",
                        gtk::Label {
                            set_hexpand: true,
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-preset"),
                        },
                        gtk::DropDown {
                            set_model: Some(&gtk::StringList::new(&["Speed", "Balanced", "Quality"])),
                            set_selected: preset_index(
                                model.config.config().modules.recorder.encoder_preset.get(),
                            ),
                            connect_selected_notify[sender] => move |dropdown| {
                                sender.input(RecorderDropdownMsg::PresetSelected(dropdown.selected()));
                            },
                        },
                    },
                },
            },
        }
    }

    fn init(
        init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let scale = init.config.config().styling.scale.get().value();

        watchers::spawn(&sender, &init.config, &init.state);

        let model = Self {
            scaled_width: resolve_dimension(None, BASE_WIDTH, scale),
            active: init.state.active.get(),
            paused: init.state.paused.get(),
            config: init.config,
            state: init.state,
        };

        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>, _root: &Self::Root) {
        let recorder = &self.config.config().modules.recorder;
        match msg {
            RecorderDropdownMsg::ToggleRecording => {
                let state = self.state.clone();
                relm4::spawn_local(async move { state.toggle().await });
            }
            RecorderDropdownMsg::TogglePause => {
                self.state.set_paused(!self.state.paused.get());
            }
            RecorderDropdownMsg::MicrophoneToggled(active) => recorder.microphone.set(active),
            RecorderDropdownMsg::SystemAudioToggled(active) => recorder.system_audio.set(active),
            RecorderDropdownMsg::WebcamToggled(active) => recorder.webcam_enabled.set(active),
            RecorderDropdownMsg::PositionSelected(index) => {
                recorder.webcam_position.set(position_from_index(index));
            }
            RecorderDropdownMsg::BitrateChanged(kbps) => recorder.bitrate_kbps.set(kbps),
            RecorderDropdownMsg::AudioBitrateChanged(kbps) => {
                recorder.audio_bitrate_kbps.set(kbps);
            }
            RecorderDropdownMsg::SeparateTracksToggled(active) => {
                recorder.separate_audio_tracks.set(active);
            }
            RecorderDropdownMsg::PresetSelected(index) => {
                recorder.encoder_preset.set(preset_from_index(index));
            }
        }
    }

    fn update_cmd(
        &mut self,
        msg: Self::CommandOutput,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            RecorderDropdownCmd::StateChanged => {
                self.active = self.state.active.get();
                self.paused = self.state.paused.get();
            }
            RecorderDropdownCmd::ScaleChanged(scale) => {
                self.scaled_width = resolve_dimension(None, BASE_WIDTH, scale);
            }
        }
    }
}

fn position_index(position: WebcamPosition) -> u32 {
    match position {
        WebcamPosition::TopLeft => 0,
        WebcamPosition::TopRight => 1,
        WebcamPosition::BottomLeft => 2,
        WebcamPosition::BottomRight => 3,
    }
}

fn position_from_index(index: u32) -> WebcamPosition {
    match index {
        0 => WebcamPosition::TopLeft,
        1 => WebcamPosition::TopRight,
        2 => WebcamPosition::BottomLeft,
        _ => WebcamPosition::BottomRight,
    }
}

fn preset_index(preset: EncoderPreset) -> u32 {
    match preset {
        EncoderPreset::Speed => 0,
        EncoderPreset::Balanced => 1,
        EncoderPreset::Quality => 2,
    }
}

fn preset_from_index(index: u32) -> EncoderPreset {
    match index {
        0 => EncoderPreset::Speed,
        2 => EncoderPreset::Quality,
        _ => EncoderPreset::Balanced,
    }
}
