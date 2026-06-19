mod devices;
mod factory;
mod messages;
mod watchers;

use std::sync::Arc;

use gtk::prelude::*;
use relm4::{gtk, prelude::*};
use wayle_audio::AudioService;
use wayle_config::{ConfigService, schemas::modules::WebcamPosition};
use wayle_widgets::prelude::*;

pub(super) use self::factory::Factory;
use self::{
    devices::DeviceChoice,
    messages::{RecorderDropdownCmd, RecorderDropdownInit, RecorderDropdownMsg},
};
use crate::{i18n::t, services::recorder::RecorderState, shell::bar::dropdowns::resolve_dimension};

const BASE_WIDTH: f32 = 360.0;

pub(crate) struct RecorderDropdown {
    config: Arc<ConfigService>,
    state: RecorderState,
    /// Audio service for enumerating microphone sources (may be absent).
    audio: Option<Arc<AudioService>>,
    scaled_width: i32,
    active: bool,
    paused: bool,
    /// Mirrors `recorder.microphone`; gates the source picker's sensitivity.
    microphone_on: bool,
    /// Mirrors `recorder.webcam_enabled`; gates the webcam rows' sensitivity.
    webcam_on: bool,
    /// Whether at least one V4L2 camera exists; the whole webcam group is
    /// hidden when false.
    has_camera: bool,
    /// Mirrors `recorder.webcam_position`; highlights the preview corner.
    webcam_position: WebcamPosition,
    /// Elapsed seconds of the active recording, for the live status row.
    elapsed_secs: u32,
    /// Snapshot of selectable microphone sources (index 0 = Default).
    mic_sources: Vec<DeviceChoice>,
    /// Snapshot of selectable cameras (index 0 = Automatic).
    cameras: Vec<DeviceChoice>,
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
                    #[template_child]
                    actions {
                        set_visible: true,

                        gtk::Box {
                            add_css_class: "recorder-status",
                            #[watch]
                            set_visible: model.active,

                            gtk::Box {
                                add_css_class: "recorder-status-dot",
                                #[watch]
                                set_class_active: ("paused", model.paused),
                            },
                            gtk::Label {
                                add_css_class: "recorder-status-time",
                                #[watch]
                                set_label: &format_elapsed(model.elapsed_secs),
                            },
                        },
                    },
                },

                #[template]
                DropdownContent {
                    add_css_class: "recorder-dropdown-content",

                    // Primary actions: a full-width record/stop toggle plus an
                    // icon-only pause/resume button that lights up while active.
                    gtk::Box {
                        add_css_class: "recorder-controls",
                        set_spacing: 8,

                        gtk::Button {
                            add_css_class: "recorder-record-button",
                            set_hexpand: true,
                            #[watch]
                            set_class_active: ("danger", model.active),
                            #[watch]
                            set_class_active: ("primary", !model.active),

                            gtk::Box {
                                set_halign: gtk::Align::Center,
                                set_spacing: 8,
                                gtk::Image {
                                    #[watch]
                                    set_icon_name: Some(if model.active {
                                        "ld-square-symbolic"
                                    } else {
                                        "ld-circle-dot-symbolic"
                                    }),
                                },
                                gtk::Label {
                                    #[watch]
                                    set_label: &if model.active {
                                        t!("dropdown-recorder-stop")
                                    } else {
                                        t!("dropdown-recorder-record")
                                    },
                                },
                            },
                            connect_clicked => RecorderDropdownMsg::ToggleRecording,
                        },

                        gtk::Button {
                            add_css_class: "secondary",
                            add_css_class: "recorder-pause-button",
                            #[watch]
                            set_sensitive: model.active,
                            #[watch]
                            set_icon_name: if model.paused {
                                "ld-play-symbolic"
                            } else {
                                "ld-pause-symbolic"
                            },
                            #[watch]
                            set_tooltip_text: Some(&if model.paused {
                                t!("dropdown-recorder-resume")
                            } else {
                                t!("dropdown-recorder-pause")
                            }),
                            connect_clicked => RecorderDropdownMsg::TogglePause,
                        },
                    },

                    // --- Audio -------------------------------------------------
                    gtk::Box {
                        add_css_class: "recorder-section-header",
                        set_spacing: 6,
                        gtk::Image { set_icon_name: Some("ld-mic-symbolic") },
                        gtk::Label {
                            add_css_class: "section-label",
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-section-audio"),
                        },
                    },

                    gtk::Box {
                        set_css_classes: &["card", "recorder-card"],
                        set_orientation: gtk::Orientation::Vertical,

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
                            #[watch]
                            set_sensitive: model.microphone_on,
                            gtk::Label {
                                set_hexpand: true,
                                set_halign: gtk::Align::Start,
                                set_label: &t!("dropdown-recorder-microphone-device"),
                            },
                            #[name = "mic_device_dropdown"]
                            gtk::DropDown {
                                set_factory: Some(&ellipsizing_string_factory()),
                                set_model: Some(&string_list(&model.mic_sources)),
                                set_selected: devices::index_of(
                                    &model.mic_sources,
                                    &model.config.config().modules.recorder.microphone_device.get(),
                                ),
                                connect_selected_notify[sender] => move |dropdown| {
                                    sender.input(RecorderDropdownMsg::MicrophoneDeviceSelected(dropdown.selected()));
                                },
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
                    },

                    // --- Webcam (hidden entirely when no camera is present) ----
                    gtk::Box {
                        add_css_class: "recorder-section-header",
                        #[watch]
                        set_visible: model.has_camera,
                        set_spacing: 6,
                        gtk::Image { set_icon_name: Some("ld-camera-symbolic") },
                        gtk::Label {
                            add_css_class: "section-label",
                            set_halign: gtk::Align::Start,
                            set_label: &t!("dropdown-recorder-section-webcam"),
                        },
                    },

                    gtk::Box {
                        set_css_classes: &["card", "recorder-card"],
                        set_orientation: gtk::Orientation::Vertical,
                        #[watch]
                        set_visible: model.has_camera,

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
                            #[watch]
                            set_sensitive: model.webcam_on,
                            gtk::Label {
                                set_hexpand: true,
                                set_halign: gtk::Align::Start,
                                set_label: &t!("dropdown-recorder-webcam-device"),
                            },
                            gtk::DropDown {
                                set_factory: Some(&ellipsizing_string_factory()),
                                set_model: Some(&string_list(&model.cameras)),
                                set_selected: devices::index_of(
                                    &model.cameras,
                                    &model.config.config().modules.recorder.webcam_device.get(),
                                ),
                                connect_selected_notify[sender] => move |dropdown| {
                                    sender.input(RecorderDropdownMsg::WebcamDeviceSelected(dropdown.selected()));
                                },
                            },
                        },

                        // Visual corner picker replacing the position dropdown.
                        gtk::Box {
                            add_css_class: "recorder-row",
                            #[watch]
                            set_sensitive: model.webcam_on,
                            gtk::Label {
                                set_hexpand: true,
                                set_halign: gtk::Align::Start,
                                set_label: &t!("dropdown-recorder-position"),
                            },
                            gtk::Grid {
                                add_css_class: "recorder-position-preview",
                                set_row_spacing: 4,
                                set_column_spacing: 4,

                                attach[0, 0, 1, 1] = &gtk::Button {
                                    add_css_class: "recorder-position-cell",
                                    #[watch]
                                    set_class_active: ("active", model.webcam_position == WebcamPosition::TopLeft),
                                    set_tooltip_text: Some("Top Left"),
                                    connect_clicked => RecorderDropdownMsg::PositionSelected(0),
                                },
                                attach[1, 0, 1, 1] = &gtk::Button {
                                    add_css_class: "recorder-position-cell",
                                    #[watch]
                                    set_class_active: ("active", model.webcam_position == WebcamPosition::TopRight),
                                    set_tooltip_text: Some("Top Right"),
                                    connect_clicked => RecorderDropdownMsg::PositionSelected(1),
                                },
                                attach[0, 1, 1, 1] = &gtk::Button {
                                    add_css_class: "recorder-position-cell",
                                    #[watch]
                                    set_class_active: ("active", model.webcam_position == WebcamPosition::BottomLeft),
                                    set_tooltip_text: Some("Bottom Left"),
                                    connect_clicked => RecorderDropdownMsg::PositionSelected(2),
                                },
                                attach[1, 1, 1, 1] = &gtk::Button {
                                    add_css_class: "recorder-position-cell",
                                    #[watch]
                                    set_class_active: ("active", model.webcam_position == WebcamPosition::BottomRight),
                                    set_tooltip_text: Some("Bottom Right"),
                                    connect_clicked => RecorderDropdownMsg::PositionSelected(3),
                                },
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
        let recorder = &init.config.config().modules.recorder;

        watchers::spawn(&sender, &init.config, &init.state, init.audio.as_ref());

        let mic_sources = devices::microphone_sources(init.audio.as_ref());
        let cameras = devices::cameras();

        let model = Self {
            scaled_width: resolve_dimension(None, BASE_WIDTH, scale),
            active: init.state.active.get(),
            paused: init.state.paused.get(),
            microphone_on: recorder.microphone.get(),
            webcam_on: recorder.webcam_enabled.get(),
            has_camera: cameras.len() > 1,
            webcam_position: recorder.webcam_position.get(),
            elapsed_secs: init.state.elapsed_secs.get(),
            mic_sources,
            cameras,
            audio: init.audio,
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
            RecorderDropdownMsg::MicrophoneToggled(active) => {
                recorder.microphone.set(active);
                self.microphone_on = active;
            }
            RecorderDropdownMsg::MicrophoneDeviceSelected(index) => {
                if let Some(choice) = self.mic_sources.get(index as usize) {
                    recorder.microphone_device.set(choice.id.clone());
                }
            }
            RecorderDropdownMsg::SystemAudioToggled(active) => recorder.system_audio.set(active),
            RecorderDropdownMsg::WebcamToggled(active) => {
                recorder.webcam_enabled.set(active);
                self.webcam_on = active;
            }
            RecorderDropdownMsg::WebcamDeviceSelected(index) => {
                if let Some(choice) = self.cameras.get(index as usize) {
                    recorder.webcam_device.set(choice.id.clone());
                }
            }
            RecorderDropdownMsg::PositionSelected(index) => {
                let position = position_from_index(index);
                recorder.webcam_position.set(position);
                self.webcam_position = position;
            }
            RecorderDropdownMsg::SeparateTracksToggled(active) => {
                recorder.separate_audio_tracks.set(active);
            }
        }
    }

    fn update_cmd_with_view(
        &mut self,
        widgets: &mut Self::Widgets,
        msg: Self::CommandOutput,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            RecorderDropdownCmd::StateChanged => {
                self.active = self.state.active.get();
                self.paused = self.state.paused.get();
                self.elapsed_secs = self.state.elapsed_secs.get();
            }
            RecorderDropdownCmd::ScaleChanged(scale) => {
                self.scaled_width = resolve_dimension(None, BASE_WIDTH, scale);
            }
            RecorderDropdownCmd::MicrophonesUpdated => {
                // Rebuild the microphone-source list on device hotplug, keeping
                // the saved selection if it is still present.
                self.mic_sources = devices::microphone_sources(self.audio.as_ref());
                let selected = devices::index_of(
                    &self.mic_sources,
                    &self
                        .config
                        .config()
                        .modules
                        .recorder
                        .microphone_device
                        .get(),
                );
                widgets
                    .mic_device_dropdown
                    .set_model(Some(&string_list(&self.mic_sources)));
                widgets.mic_device_dropdown.set_selected(selected);
            }
        }
        self.update_view(widgets, _sender);
    }
}

/// Builds a `gtk::StringList` from device choice labels.
fn string_list(choices: &[DeviceChoice]) -> gtk::StringList {
    let labels: Vec<&str> = choices.iter().map(|c| c.label.as_str()).collect();
    gtk::StringList::new(&labels)
}

/// Formats elapsed seconds as `M:SS` (or `H:MM:SS` past an hour) for the
/// live status readout in the header.
fn format_elapsed(secs: u32) -> String {
    let hours = secs / 3600;
    let minutes = (secs % 3600) / 60;
    let seconds = secs % 60;
    if hours > 0 {
        format!("{hours}:{minutes:02}:{seconds:02}")
    } else {
        format!("{minutes}:{seconds:02}")
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
