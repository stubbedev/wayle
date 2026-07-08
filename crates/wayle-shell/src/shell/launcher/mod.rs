//! Launcher surface (rofi replacement).
//!
//! A pre-warmed layer-shell overlay hosting the `wayle-launcher` engine.
//! Sessions arrive from the `wayle launcher` CLI over the launcher socket
//! (see `crate::services::launcher`); the session's terminal frame (result /
//! cancelled) is delivered back through a oneshot carried in
//! [`LauncherInput::OpenSession`]. The engine (modes + matcher) runs on a
//! tokio actor task; this component only renders and forwards input.

mod engine_actor;
mod match_model;
mod setup;
mod views;

use std::{cell::RefCell, rc::Rc, sync::Arc, time::Duration};

use engine_actor::{EngineCmd, EngineEvent};
use gtk4_layer_shell::{Edge, KeyboardMode, Layer, LayerShell};
use match_model::MatchModel;
use relm4::{gtk, gtk::prelude::*, prelude::*};
use setup::UiSettings;
use tokio::sync::{mpsc, oneshot};
use tracing::warn;
use views::{KeyAction, KeyBinding, ROW_PX};
use wayle_config::{
    ConfigService,
    schemas::{
        animations::{AnimSurface, AnimationType},
        launcher::LauncherLocation,
    },
};
use wayle_ipc::launcher_socket::{Selected, ServerFrame, SessionOptions};
use wayle_launcher::{Action, ActivateKind};
use wayle_widgets::prelude::WayleRevealer;

/// Messages driving the launcher surface.
pub(crate) enum LauncherInput {
    /// A CLI session arrived over the socket.
    OpenSession {
        /// Connection identity (guards stale `ClientGone`).
        id: u64,
        /// Merged rofi flags.
        options: Box<SessionOptions>,
        /// Displace a live session instead of reporting busy.
        replace: bool,
        /// Terminal frame back to the CLI.
        reply: oneshot::Sender<ServerFrame>,
        /// dmenu row stream (consumed in the dmenu phase).
        rows: mpsc::Receiver<Vec<String>>,
    },
    /// The CLI's socket died; close if it owns the current session.
    ClientGone {
        /// Connection identity.
        id: u64,
    },
    /// Event from the engine actor.
    Engine(EngineEvent),
    /// Search entry text changed.
    QueryChanged(String),
    /// A list row was activated (position in the matched list).
    RowActivated(u32),
    /// A bound key fired.
    Key(KeyAction),
    /// A sidebar mode tab was clicked.
    TabClicked(usize),
    /// `-dump` debounce fired: matches settled, reply and close.
    DumpReady,
}

impl std::fmt::Debug for LauncherInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::OpenSession { id, replace, .. } => f
                .debug_struct("OpenSession")
                .field("id", id)
                .field("replace", replace)
                .finish_non_exhaustive(),
            Self::ClientGone { id } => f.debug_struct("ClientGone").field("id", id).finish(),
            Self::Engine(event) => f.debug_tuple("Engine").field(event).finish(),
            Self::QueryChanged(_) => f.write_str("QueryChanged"),
            Self::RowActivated(pos) => f.debug_tuple("RowActivated").field(pos).finish(),
            Self::Key(action) => f.debug_tuple("Key").field(action).finish(),
            Self::TabClicked(index) => f.debug_tuple("TabClicked").field(index).finish(),
            Self::DumpReady => f.write_str("DumpReady"),
        }
    }
}

/// The launcher component.
pub(crate) struct Launcher {
    config: Arc<ConfigService>,
    /// Live session state, `None` when hidden.
    session: Option<ActiveSession>,
    model: MatchModel,
    selection: gtk::SingleSelection,
    /// Compiled keybindings, shared with the key controller closure.
    bindings: Rc<RefCell<Vec<KeyBinding>>>,
    /// Multi-select display state, shared with the row factory.
    multi: Rc<RefCell<views::MultiSelect>>,
}

struct ActiveSession {
    id: u64,
    engine: Option<mpsc::UnboundedSender<EngineCmd>>,
    reply: Option<oneshot::Sender<ServerFrame>>,
    ui: UiSettings,
    /// Prompt supplied by the active mode (overridden by `-p`).
    mode_prompt: String,
    /// `-e` dialog: message only, any accept/cancel closes with code 0.
    dialog: bool,
    /// `-dump`: reply with the filtered list, never show the surface.
    dump: bool,
    /// Debounce source for the dump reply (fires after matches settle).
    dump_timer: Option<gtk::glib::SourceId>,
    /// Selection to apply on the next Matches (script `new-selection` /
    /// `keep-selection`).
    pending_selection: PendingSelection,
}

#[derive(Debug, Clone, Copy, Default)]
enum PendingSelection {
    /// Select the first row (default).
    #[default]
    First,
    /// Keep the current position.
    Keep,
    /// Select an absolute position.
    At(u32),
}

#[relm4::component(pub(crate))]
impl Component for Launcher {
    type Init = Arc<ConfigService>;
    type Input = LauncherInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            add_css_class: "launcher-window",
            set_visible: false,

            #[name = "revealer"]
            WayleRevealer {
                set_reveal_child: false,

                #[name = "surface"]
                gtk::Box {
                    add_css_class: "launcher-surface",
                    set_orientation: gtk::Orientation::Vertical,

                    #[name = "input_row"]
                    gtk::Box {
                        add_css_class: "launcher-input",
                        set_orientation: gtk::Orientation::Horizontal,

                        #[name = "prompt"]
                        gtk::Label {
                            add_css_class: "launcher-prompt",
                        },

                        #[name = "entry"]
                        gtk::Entry {
                            add_css_class: "launcher-entry",
                            set_hexpand: true,
                        },
                    },

                    #[name = "message"]
                    gtk::Label {
                        add_css_class: "launcher-message",
                        set_xalign: 0.0,
                        set_wrap: true,
                        set_visible: false,
                    },

                    #[name = "scrolled"]
                    gtk::ScrolledWindow {
                        add_css_class: "launcher-list",
                        set_hscrollbar_policy: gtk::PolicyType::Never,
                        set_vexpand: true,
                    },

                    #[name = "tabs"]
                    gtk::Box {
                        add_css_class: "launcher-tabs",
                        set_orientation: gtk::Orientation::Horizontal,
                        set_homogeneous: true,
                        set_visible: false,
                    },
                }
            }
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let model_data = MatchModel::default();
        let selection = gtk::SingleSelection::new(Some(model_data.clone()));
        let model = Launcher {
            config: init,
            session: None,
            model: model_data,
            selection,
            bindings: Rc::new(RefCell::new(Vec::new())),
            multi: Rc::new(RefCell::new(views::MultiSelect::default())),
        };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-launcher"));
        root.set_layer(Layer::Overlay);
        root.set_keyboard_mode(KeyboardMode::OnDemand);
        root.set_exclusive_zone(-1);

        let list = gtk::ListView::new(Some(model.selection.clone()), None::<gtk::ListItemFactory>);
        list.add_css_class("launcher-listview");
        list.set_single_click_activate(true);
        {
            let sender = sender.input_sender().clone();
            list.connect_activate(move |_, position| {
                sender.emit(LauncherInput::RowActivated(position));
            });
        }
        widgets.scrolled.set_child(Some(&list));

        {
            let sender = sender.input_sender().clone();
            widgets.entry.connect_changed(move |entry| {
                sender.emit(LauncherInput::QueryChanged(entry.text().to_string()));
            });
        }

        let bindings = model.bindings.clone();
        views::add_key_controller(&root, sender.input_sender().clone(), move || {
            bindings.borrow().clone()
        });

        let revealer = widgets.revealer.clone();
        root.connect_map(move |_| {
            let revealer = revealer.clone();
            gtk::glib::idle_add_local_once(move || revealer.set_reveal_child(true));
        });

        ComponentParts { model, widgets }
    }

    #[allow(clippy::too_many_lines)] // one arm per message; the flow reads top-down
    fn update_with_view(
        &mut self,
        widgets: &mut Self::Widgets,
        msg: LauncherInput,
        sender: ComponentSender<Self>,
        root: &Self::Root,
    ) {
        match msg {
            LauncherInput::OpenSession {
                id,
                options,
                replace,
                reply,
                rows,
            } => self.open_session(id, &options, replace, reply, rows, widgets, root, &sender),

            LauncherInput::ClientGone { id } => {
                if self.session.as_ref().is_some_and(|s| s.id == id) {
                    self.end_session(None, widgets, root);
                }
            }

            LauncherInput::Engine(event) => self.on_engine_event(event, widgets, root, &sender),

            LauncherInput::QueryChanged(query) => {
                if let Some(engine) = self.engine() {
                    let _ = engine.send(EngineCmd::SetQuery(query));
                }
            }

            LauncherInput::RowActivated(position) => self.activate_position(Some(position)),

            LauncherInput::Key(action) => self.on_key(action, widgets, root),

            LauncherInput::TabClicked(index) => {
                if let Some(engine) = self.engine() {
                    let _ = engine.send(EngineCmd::ModeTo(index));
                }
            }

            LauncherInput::DumpReady => {
                let items = self.model.texts();
                self.end_session(Some(ServerFrame::Dump { items }), widgets, root);
            }
        }
    }
}

impl Launcher {
    fn engine(&self) -> Option<&mpsc::UnboundedSender<EngineCmd>> {
        self.session.as_ref().and_then(|s| s.engine.as_ref())
    }

    #[allow(clippy::too_many_arguments)] // session handoff carries exactly these
    fn open_session(
        &mut self,
        id: u64,
        options: &SessionOptions,
        replace: bool,
        reply: oneshot::Sender<ServerFrame>,
        rows: mpsc::Receiver<Vec<String>>,
        widgets: &LauncherWidgets,
        root: &gtk::Window,
        sender: &ComponentSender<Self>,
    ) {
        if self.session.is_some() {
            if replace {
                self.end_session(Some(ServerFrame::Cancelled { code: 1 }), widgets, root);
            } else {
                let _ = reply.send(ServerFrame::Busy);
                return;
            }
        }

        let config = self.config.config();
        let setup = setup::build(options, config, options.dmenu.then_some(rows));
        let dialog = setup.ui.error_message.is_some();
        if setup.modes.is_empty() && !dialog {
            warn!("launcher: no usable modes in session request");
            let _ = reply.send(ServerFrame::Cancelled { code: 1 });
            return;
        }

        *self.bindings.borrow_mut() = views::compile_bindings(&setup.ui.keybindings);
        {
            let mut multi = self.multi.borrow_mut();
            multi.enabled = false;
            multi.picked.clear();
            multi.ballot_selected = options
                .ballot_selected
                .clone()
                .unwrap_or_else(|| "☑ ".to_owned());
            multi.ballot_unselected = options
                .ballot_unselected
                .clone()
                .unwrap_or_else(|| "☐ ".to_owned());
        }

        let engine = (!dialog).then(|| {
            engine_actor::spawn(
                setup.modes,
                setup.initial_mode,
                setup.matcher,
                sender.input_sender().clone(),
            )
        });

        let dump = options.dump;
        self.apply_ui(&setup.ui, dialog, widgets, root);
        self.session = Some(ActiveSession {
            id,
            engine,
            reply: Some(reply),
            ui: setup.ui,
            mode_prompt: String::new(),
            dialog,
            dump,
            dump_timer: None,
            pending_selection: PendingSelection::First,
        });
        self.model.update(Arc::new(Vec::new()), Vec::new());
        if let (true, Some(filter), Some(engine)) = (
            dump,
            self.session.as_ref().and_then(|s| s.ui.filter.clone()),
            self.engine(),
        ) {
            let _ = engine.send(EngineCmd::SetQuery(filter));
        }
        if !dump {
            self.reveal(widgets, root);
            widgets.entry.grab_focus();
        }
    }

    /// Apply per-session UI settings to the widget tree.
    fn apply_ui(
        &self,
        ui: &UiSettings,
        dialog: bool,
        widgets: &LauncherWidgets,
        root: &gtk::Window,
    ) {
        widgets.surface.set_size_request(ui.width, -1);
        apply_location(root, ui.location);

        widgets.entry.set_text(ui.filter.as_deref().unwrap_or(""));
        widgets.entry.set_position(-1);
        widgets.entry.set_visibility(!ui.password);
        widgets.entry.set_input_purpose(if ui.password {
            gtk::InputPurpose::Password
        } else {
            gtk::InputPurpose::FreeForm
        });

        widgets.prompt.set_text(ui.prompt.as_deref().unwrap_or(""));
        widgets
            .prompt
            .set_visible(ui.prompt.as_deref().is_some_and(|p| !p.is_empty()));

        let message = ui.error_message.as_deref().or(ui.mesg.as_deref());
        match message {
            Some(text) => {
                widgets.message.set_markup(text);
                widgets.message.set_visible(true);
            }
            None => widgets.message.set_visible(false),
        }

        widgets.input_row.set_visible(!dialog);
        widgets.scrolled.set_visible(!dialog);
        let lines = i32::try_from(ui.lines).unwrap_or(10) * ROW_PX;
        if ui.fixed_num_lines {
            widgets.scrolled.set_min_content_height(lines);
            widgets.scrolled.set_max_content_height(lines);
        } else {
            widgets.scrolled.set_min_content_height(0);
            widgets.scrolled.set_max_content_height(lines);
            widgets.scrolled.set_propagate_natural_height(true);
        }

        if let Some(list) = widgets.scrolled.child().and_downcast::<gtk::ListView>() {
            let display = views::RowDisplay {
                columns: ui.display_columns.clone(),
                separator: ui.column_separator.clone(),
                ellipsize: ui.ellipsize.clone(),
            };
            list.set_factory(Some(&views::row_factory(
                ui.show_icons,
                display,
                self.multi.clone(),
            )));
        }

        widgets.tabs.set_visible(ui.sidebar);
    }

    fn on_engine_event(
        &mut self,
        event: EngineEvent,
        widgets: &LauncherWidgets,
        root: &gtk::Window,
        sender: &ComponentSender<Self>,
    ) {
        match event {
            EngineEvent::State { .. } => self.on_state(event, widgets, sender),
            EngineEvent::Matches { items, matched } => {
                let previous = self.selection.selected();
                self.model.update(items, matched);
                if self.model.len() > 0 {
                    let target = match self
                        .session
                        .as_ref()
                        .map_or(PendingSelection::First, |s| s.pending_selection)
                    {
                        PendingSelection::First => 0,
                        PendingSelection::Keep => previous.min(self.model.len() - 1),
                        PendingSelection::At(position) => position.min(self.model.len() - 1),
                    };
                    self.selection.set_selected(target);
                    self.apply_one_shot_selection();
                    // rofi -auto-select: accept when one result remains.
                    let auto = self.session.as_ref().is_some_and(|s| s.ui.auto_select);
                    if auto && self.model.len() == 1 && !widgets.entry.text().is_empty() {
                        self.activate_position(Some(0));
                    }
                }
                self.arm_dump_reply(sender);
            }
            EngineEvent::Action(action) => match action {
                Action::Close => {
                    self.end_session(
                        Some(ServerFrame::Result {
                            code: 0,
                            selected: Vec::new(),
                            filter: widgets.entry.text().to_string(),
                        }),
                        widgets,
                        root,
                    );
                }
                Action::Exit { code, selected } => {
                    let selected = selected
                        .into_iter()
                        .map(|(index, text)| Selected { index, text })
                        .collect();
                    self.end_session(
                        Some(ServerFrame::Result {
                            code,
                            selected,
                            filter: widgets.entry.text().to_string(),
                        }),
                        widgets,
                        root,
                    );
                }
                Action::SetInput(text) => {
                    widgets.entry.set_text(&text);
                    widgets.entry.set_position(-1);
                }
                Action::Reload(_) | Action::SwitchMode(_) | Action::Nothing => {}
            },
        }
    }

    /// Apply an [`EngineEvent::State`]: prompt, message, tabs, selection
    /// intent, multi-select flag, filter clearing.
    fn on_state(
        &mut self,
        event: EngineEvent,
        widgets: &LauncherWidgets,
        sender: &ComponentSender<Self>,
    ) {
        let EngineEvent::State {
            prompt,
            message,
            active_mode,
            mode_names,
            multi_select,
            after_activate,
            keep_filter,
            keep_selection,
            new_selection,
        } = event
        else {
            return;
        };
        {
            let mut multi = self.multi.borrow_mut();
            multi.enabled = multi_select;
            if !multi_select {
                multi.picked.clear();
            }
        }
        if after_activate && !keep_filter && !widgets.entry.text().is_empty() {
            // Script reload without keep-filter clears the query.
            widgets.entry.set_text("");
        }
        let Some(session) = &mut self.session else {
            return;
        };
        session.pending_selection = match new_selection {
            Some(position) => PendingSelection::At(position),
            None if keep_selection => PendingSelection::Keep,
            None => PendingSelection::First,
        };
        session.mode_prompt.clone_from(&prompt);
        let shown = session
            .ui
            .prompt
            .clone()
            .or_else(|| session.ui.display_names.get(&prompt).cloned())
            .unwrap_or(prompt);
        widgets.prompt.set_text(&shown);
        widgets.prompt.set_visible(!shown.is_empty());

        let message = session
            .ui
            .error_message
            .as_deref()
            .or(session.ui.mesg.as_deref())
            .map(ToOwned::to_owned)
            .or(message);
        match message {
            Some(text) => {
                widgets.message.set_markup(&text);
                widgets.message.set_visible(true);
            }
            None => widgets.message.set_visible(false),
        }

        if session.ui.sidebar {
            rebuild_tabs(
                widgets,
                &mode_names,
                active_mode,
                &session.ui,
                sender.input_sender(),
            );
        }
    }

    fn on_key(&mut self, action: KeyAction, widgets: &LauncherWidgets, root: &gtk::Window) {
        if self.session.as_ref().is_some_and(|s| s.dialog) {
            // `-e` dialog: any bound action dismisses with success.
            self.end_session(
                Some(ServerFrame::Result {
                    code: 0,
                    selected: Vec::new(),
                    filter: String::new(),
                }),
                widgets,
                root,
            );
            return;
        }
        match action {
            KeyAction::Cancel => {
                self.end_session(Some(ServerFrame::Cancelled { code: 1 }), widgets, root);
            }
            KeyAction::Accept => {
                let picked: Vec<u32> = self.multi.borrow().picked.iter().copied().collect();
                if !picked.is_empty() {
                    if let Some(engine) = self.engine() {
                        let _ = engine.send(EngineCmd::ActivateMulti(picked));
                    }
                } else if self.model.len() == 0 {
                    self.activate_custom(widgets, ActivateKind::Custom(String::new()));
                } else {
                    self.activate_position(None);
                }
            }
            KeyAction::AcceptAlt => {
                if self.multi.borrow().enabled {
                    // rofi multi-select: Shift+Enter toggles + advances.
                    self.toggle_picked();
                    self.move_selection(1);
                } else {
                    self.activate_selected(ActivateKind::Alt);
                }
            }
            KeyAction::AcceptCustom => {
                self.activate_custom(widgets, ActivateKind::Custom(String::new()));
            }
            KeyAction::Custom(n) => {
                let index = self.selected_item_index();
                if let Some(engine) = self.engine() {
                    let _ = engine.send(EngineCmd::Activate(index, ActivateKind::KbCustom(n)));
                }
            }
            KeyAction::DeleteEntry => {
                if let (Some(index), Some(engine)) = (self.selected_item_index(), self.engine()) {
                    let _ = engine.send(EngineCmd::Delete(index));
                }
            }
            KeyAction::ModeNext => {
                if let Some(engine) = self.engine() {
                    let _ = engine.send(EngineCmd::ModeNext);
                }
            }
            KeyAction::ModePrevious => {
                if let Some(engine) = self.engine() {
                    let _ = engine.send(EngineCmd::ModePrevious);
                }
            }
            KeyAction::RowUp => self.move_selection(-1),
            KeyAction::RowDown => self.move_selection(1),
            KeyAction::RowFirst => self.selection.set_selected(0),
            KeyAction::RowLast => {
                let len = self.model.len();
                if len > 0 {
                    self.selection.set_selected(len - 1);
                }
            }
            KeyAction::PagePrev => {
                let lines = self.session.as_ref().map_or(10, |s| s.ui.lines);
                self.move_selection(-i64::from(lines));
            }
            KeyAction::PageNext => {
                let lines = self.session.as_ref().map_or(10, |s| s.ui.lines);
                self.move_selection(i64::from(lines));
            }
        }
    }

    /// Apply `-select`/`-selected-row` once, on the first populated match
    /// list of the session.
    fn apply_one_shot_selection(&mut self) {
        let Some(session) = &mut self.session else {
            return;
        };
        if let Some(row) = session.ui.selected_row.take() {
            self.selection.set_selected(row.min(self.model.len() - 1));
        } else if let Some(needle) = session.ui.select.take()
            && let Some(position) = self.model.find_position(&needle)
        {
            self.selection.set_selected(position);
        }
    }

    /// Toggle the current row in the multi-select set and re-bind it.
    fn toggle_picked(&mut self) {
        let position = self.selection.selected();
        let Some(item_index) = self.model.item_index(position) else {
            return;
        };
        {
            let mut multi = self.multi.borrow_mut();
            if !multi.picked.remove(&item_index) {
                multi.picked.insert(item_index);
            }
        }
        self.model.refresh(position);
    }

    /// `-dump` sessions: reply with the filtered list once matches have
    /// settled (150ms debounce), then close.
    fn arm_dump_reply(&mut self, sender: &ComponentSender<Self>) {
        let Some(session) = &mut self.session else {
            return;
        };
        if !session.dump {
            return;
        }
        if let Some(source) = session.dump_timer.take() {
            source.remove();
        }
        let sender = sender.input_sender().clone();
        session.dump_timer = Some(gtk::glib::timeout_add_local_once(
            Duration::from_millis(150),
            move || sender.emit(LauncherInput::DumpReady),
        ));
    }

    fn move_selection(&self, delta: i64) {
        let len = i64::from(self.model.len());
        if len == 0 {
            return;
        }
        let cycle = self.session.as_ref().is_some_and(|s| s.ui.cycle);
        let current = i64::from(self.selection.selected());
        let target = if cycle && delta.abs() == 1 {
            (current + delta).rem_euclid(len)
        } else {
            (current + delta).clamp(0, len - 1)
        };
        #[allow(clippy::cast_sign_loss, clippy::cast_possible_truncation)]
        self.selection.set_selected(target as u32);
    }

    /// Activate the current selection with an explicit kind (alt accept).
    fn activate_selected(&mut self, kind: ActivateKind) {
        let Some(index) = self.selected_item_index() else {
            return;
        };
        if let Some(engine) = self.engine() {
            let _ = engine.send(EngineCmd::Activate(Some(index), kind));
        }
    }

    fn selected_item_index(&self) -> Option<u32> {
        let position = self.selection.selected();
        if position == gtk::INVALID_LIST_POSITION {
            return None;
        }
        self.model.item_index(position)
    }

    /// Activate the row at `position` (or the current selection).
    fn activate_position(&mut self, position: Option<u32>) {
        let position = position.unwrap_or_else(|| self.selection.selected());
        let Some(item_index) = self.model.item_index(position) else {
            return;
        };
        if let Some(engine) = self.engine() {
            let _ = engine.send(EngineCmd::Activate(Some(item_index), ActivateKind::Default));
        }
    }

    /// Accept the typed text as custom input.
    fn activate_custom(&mut self, widgets: &LauncherWidgets, _kind: ActivateKind) {
        let text = widgets.entry.text().to_string();
        if let Some(engine) = self.engine() {
            let _ = engine.send(EngineCmd::Activate(None, ActivateKind::Custom(text)));
        }
    }

    /// Tear the session down: stop the engine, answer the CLI (when a frame
    /// is given), and hide the surface.
    fn end_session(
        &mut self,
        frame: Option<ServerFrame>,
        widgets: &LauncherWidgets,
        root: &gtk::Window,
    ) {
        if let Some(mut session) = self.session.take() {
            if let Some(engine) = session.engine.take() {
                let _ = engine.send(EngineCmd::Stop);
            }
            if let Some(source) = session.dump_timer.take() {
                source.remove();
            }
            if let (Some(reply), Some(frame)) = (session.reply.take(), frame) {
                let _ = reply.send(frame);
            }
        }
        self.multi.borrow_mut().picked.clear();
        self.model.update(Arc::new(Vec::new()), Vec::new());
        self.hide_animated(widgets, root);
    }

    fn animation(&self, exiting: bool) -> (AnimationType, u32) {
        let animations = &self.config.config().animations;
        (
            animations.transition_for(AnimSurface::Launcher, exiting),
            animations.duration_for(AnimSurface::Launcher, exiting),
        )
    }

    fn reveal(&self, widgets: &LauncherWidgets, root: &gtk::Window) {
        let (transition, duration) = self.animation(false);
        widgets.revealer.set_transition(transition);
        widgets.revealer.set_transition_duration(duration);
        widgets.revealer.set_reveal_child(false);
        root.set_visible(true);
        root.present();
    }

    fn hide_animated(&self, widgets: &LauncherWidgets, root: &gtk::Window) {
        let (transition, duration) = self.animation(true);
        widgets.revealer.set_transition(transition);
        widgets.revealer.set_transition_duration(duration);
        widgets.revealer.set_reveal_child(false);
        let root = root.clone();
        gtk::glib::timeout_add_local_once(Duration::from_millis(u64::from(duration)), move || {
            root.set_visible(false);
        });
    }
}

/// Map the location enum onto layer-shell anchors.
fn apply_location(root: &gtk::Window, location: LauncherLocation) {
    for edge in [Edge::Top, Edge::Bottom, Edge::Left, Edge::Right] {
        root.set_anchor(edge, false);
    }
    let anchors: &[Edge] = match location {
        LauncherLocation::Center => &[],
        LauncherLocation::North => &[Edge::Top],
        LauncherLocation::NorthEast => &[Edge::Top, Edge::Right],
        LauncherLocation::East => &[Edge::Right],
        LauncherLocation::SouthEast => &[Edge::Bottom, Edge::Right],
        LauncherLocation::South => &[Edge::Bottom],
        LauncherLocation::SouthWest => &[Edge::Bottom, Edge::Left],
        LauncherLocation::West => &[Edge::Left],
        LauncherLocation::NorthWest => &[Edge::Top, Edge::Left],
    };
    for edge in anchors {
        root.set_anchor(*edge, true);
    }
}

/// Rebuild the sidebar mode tabs.
fn rebuild_tabs(
    widgets: &LauncherWidgets,
    mode_names: &[String],
    active: usize,
    ui: &UiSettings,
    sender: &relm4::Sender<LauncherInput>,
) {
    while let Some(child) = widgets.tabs.first_child() {
        widgets.tabs.remove(&child);
    }
    for (index, name) in mode_names.iter().enumerate() {
        let label = ui
            .display_names
            .get(name)
            .cloned()
            .unwrap_or_else(|| name.clone());
        let button = gtk::Button::with_label(&label);
        button.add_css_class("launcher-tab");
        if index == active {
            button.add_css_class("active");
        }
        let sender = sender.clone();
        button.connect_clicked(move |_| {
            sender.emit(LauncherInput::TabClicked(index));
        });
        widgets.tabs.append(&button);
    }
}
