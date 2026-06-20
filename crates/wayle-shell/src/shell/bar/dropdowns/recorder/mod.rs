mod devices;
mod factory;
mod messages;
mod watchers;

use std::{cell::Cell, rc::Rc, sync::Arc};

use gtk::prelude::*;
use relm4::{gtk, prelude::*};
use wayle_audio::AudioService;
use wayle_config::{ConfigService, schemas::styling::Percentage};
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
    /// Relative webcam position (0-100) within the free space, mirroring
    /// `recorder.webcam_x` / `recorder.webcam_y`.
    webcam_x: u8,
    webcam_y: u8,
    /// Pixel geometry of the drag preview, derived from the popover scale and
    /// the configured webcam size. `cam_x_px`/`cam_y_px` are the frame's offset
    /// computed from `webcam_x`/`webcam_y`.
    preview_w: i32,
    preview_h: i32,
    cam_w: i32,
    cam_h: i32,
    cam_x_px: i32,
    cam_y_px: i32,
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
                                set_hexpand: false,
                                set_halign: gtk::Align::End,
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
                                set_hexpand: false,
                                set_halign: gtk::Align::End,
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

                        // Draggable position preview: the outer frame is the
                        // screen, the inner frame the webcam picture-in-picture.
                        // Drag it anywhere; stored as relative percentages so it
                        // survives resolution/monitor changes. Always editable —
                        // the position can be set up before enabling the webcam.
                        gtk::Box {
                            add_css_class: "recorder-row",
                            set_orientation: gtk::Orientation::Vertical,
                            set_spacing: 6,
                            gtk::Label {
                                set_halign: gtk::Align::Start,
                                set_label: &t!("dropdown-recorder-position"),
                            },
                            #[name = "position_overlay"]
                            gtk::Overlay {
                                set_halign: gtk::Align::Center,

                                #[wrap(Some)]
                                #[name = "position_preview"]
                                set_child = &gtk::Box {
                                    add_css_class: "recorder-position-preview",
                                    #[watch]
                                    set_size_request: (model.preview_w, model.preview_h),
                                },

                                #[name = "position_cam"]
                                add_overlay = &gtk::Box {
                                    add_css_class: "recorder-position-cam",
                                    set_halign: gtk::Align::Start,
                                    set_valign: gtk::Align::Start,
                                    set_cursor_from_name: Some("grab"),
                                    #[watch]
                                    set_size_request: (model.cam_w, model.cam_h),
                                    #[watch]
                                    set_margin_start: model.cam_x_px,
                                    #[watch]
                                    set_margin_top: model.cam_y_px,
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

        let scaled_width = resolve_dimension(None, BASE_WIDTH, scale);
        let webcam_x = recorder.webcam_x.get().value();
        let webcam_y = recorder.webcam_y.get().value();
        let (preview_w, preview_h, cam_w, cam_h) =
            preview_geometry(scaled_width, recorder.webcam_size.get().value());

        let mut model = Self {
            scaled_width,
            active: init.state.active.get(),
            paused: init.state.paused.get(),
            microphone_on: recorder.microphone.get(),
            webcam_on: recorder.webcam_enabled.get(),
            has_camera: cameras.len() > 1,
            webcam_x,
            webcam_y,
            preview_w,
            preview_h,
            cam_w,
            cam_h,
            cam_x_px: 0,
            cam_y_px: 0,
            elapsed_secs: init.state.elapsed_secs.get(),
            mic_sources,
            cameras,
            audio: init.audio,
            config: init.config,
            state: init.state,
        };
        model.reposition_cam();

        let widgets = view_output!();

        // Drag the webcam frame anywhere within the screen preview, persisting
        // the resulting position as relative percentages on release.
        //
        // The gesture is attached to the stationary overlay (not the cam frame
        // itself): dragging a widget by mutating its own margins moves it out
        // from under the pointer mid-drag, which feeds back into the gesture's
        // offsets and makes the drag jump. Tracking the pointer against a fixed
        // surface keeps it smooth. Bounds are read from the live widget sizes so
        // they stay correct after a scale change resizes the preview.
        let drag = gtk::GestureDrag::new();
        let cam = widgets.position_cam.clone();
        let frame = widgets.position_preview.clone();
        // Pointer offset within the cam frame at grab time, so the frame tracks
        // the cursor from wherever it was picked up rather than snapping.
        let grab = Rc::new(Cell::new((0.0_f64, 0.0_f64)));
        {
            let (cam, grab) = (cam.clone(), grab.clone());
            drag.connect_drag_begin(move |_, start_x, start_y| {
                grab.set((
                    start_x - f64::from(cam.margin_start()),
                    start_y - f64::from(cam.margin_top()),
                ));
                cam.set_cursor_from_name(Some("grabbing"));
            });
        }
        {
            let (cam, frame, grab) = (cam.clone(), frame.clone(), grab.clone());
            drag.connect_drag_update(move |gesture, offset_x, offset_y| {
                let (start_x, start_y) = gesture.start_point().unwrap_or((0.0, 0.0));
                let (grab_x, grab_y) = grab.get();
                let free_w = (frame.width_request() - cam.width_request()).max(0);
                let free_h = (frame.height_request() - cam.height_request()).max(0);
                let nx = (start_x + offset_x - grab_x).round() as i32;
                let ny = (start_y + offset_y - grab_y).round() as i32;
                cam.set_margin_start(nx.clamp(0, free_w));
                cam.set_margin_top(ny.clamp(0, free_h));
            });
        }
        {
            let (cam, frame, sender) = (cam.clone(), frame.clone(), sender.clone());
            drag.connect_drag_end(move |_, _, _| {
                cam.set_cursor_from_name(Some("grab"));
                let free_w = (frame.width_request() - cam.width_request()).max(0);
                let free_h = (frame.height_request() - cam.height_request()).max(0);
                let x_percent = pct_from_px(cam.margin_start(), free_w);
                let y_percent = pct_from_px(cam.margin_top(), free_h);
                sender.input(RecorderDropdownMsg::WebcamMoved {
                    x_percent,
                    y_percent,
                });
            });
        }
        widgets.position_overlay.add_controller(drag);

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>, _root: &Self::Root) {
        // Config is accessed inline per-arm rather than via a match-wide binding:
        // `config()` borrows `self`, which would otherwise block the `&mut self`
        // preview helpers below.
        match msg {
            RecorderDropdownMsg::ToggleRecording => {
                let state = self.state.clone();
                relm4::spawn_local(async move { state.toggle().await });
            }
            RecorderDropdownMsg::TogglePause => {
                self.state.set_paused(!self.state.paused.get());
            }
            RecorderDropdownMsg::MicrophoneToggled(active) => {
                self.config.config().modules.recorder.microphone.set(active);
                self.microphone_on = active;
            }
            RecorderDropdownMsg::MicrophoneDeviceSelected(index) => {
                if let Some(choice) = self.mic_sources.get(index as usize) {
                    self.config
                        .config()
                        .modules
                        .recorder
                        .microphone_device
                        .set(choice.id.clone());
                }
            }
            RecorderDropdownMsg::SystemAudioToggled(active) => {
                self.config
                    .config()
                    .modules
                    .recorder
                    .system_audio
                    .set(active);
            }
            RecorderDropdownMsg::WebcamToggled(active) => {
                self.config
                    .config()
                    .modules
                    .recorder
                    .webcam_enabled
                    .set(active);
                self.webcam_on = active;
            }
            RecorderDropdownMsg::WebcamDeviceSelected(index) => {
                if let Some(choice) = self.cameras.get(index as usize) {
                    self.config
                        .config()
                        .modules
                        .recorder
                        .webcam_device
                        .set(choice.id.clone());
                }
            }
            RecorderDropdownMsg::WebcamMoved {
                x_percent,
                y_percent,
            } => {
                let recorder = &self.config.config().modules.recorder;
                recorder.webcam_x.set(Percentage::new(x_percent));
                recorder.webcam_y.set(Percentage::new(y_percent));
                self.webcam_x = x_percent;
                self.webcam_y = y_percent;
                self.reposition_cam();
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
                let size = self.config.config().modules.recorder.webcam_size.get();
                (self.preview_w, self.preview_h, self.cam_w, self.cam_h) =
                    preview_geometry(self.scaled_width, size.value());
                self.reposition_cam();
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

impl RecorderDropdown {
    /// Recomputes the webcam frame's pixel offset in the preview from its
    /// relative position and the current preview geometry.
    fn reposition_cam(&mut self) {
        let free_w = (self.preview_w - self.cam_w).max(0);
        let free_h = (self.preview_h - self.cam_h).max(0);
        self.cam_x_px = free_w * i32::from(self.webcam_x.min(100)) / 100;
        self.cam_y_px = free_h * i32::from(self.webcam_y.min(100)) / 100;
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

/// Pixel geometry of the position preview: the screen frame (16:9, fit to the
/// popover width) and the webcam frame (the configured size percentage of it).
fn preview_geometry(scaled_width: i32, size_percent: u8) -> (i32, i32, i32, i32) {
    let preview_w = (scaled_width - 48).max(160);
    let preview_h = preview_w * 9 / 16;
    let cam_w = (preview_w * i32::from(size_percent) / 100).max(24);
    let cam_h = (cam_w * 9 / 16).max(14);
    (preview_w, preview_h, cam_w, cam_h)
}

/// Converts a pixel offset into a 0-100 percentage of the available `free`
/// span, defaulting to 0 when there is no room to move.
fn pct_from_px(px: i32, free: i32) -> u8 {
    if free <= 0 {
        0
    } else {
        (px.clamp(0, free) * 100 / free) as u8
    }
}
