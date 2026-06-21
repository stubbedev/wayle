//! EIS (libei) server for RemoteDesktop `ConnectToEIS`.
//!
//! Modern remote-desktop clients prefer an EIS socket over the `Notify*` D-Bus
//! methods. `ConnectToEIS` hands the app one end of a socketpair; this runs a
//! pure-Rust [`reis`] EIS server on the other end and replays the events the
//! client emits (pointer, button, scroll, key, keysym) onto the session's
//! [`VirtualInput`].

use std::{os::fd::OwnedFd, os::unix::net::UnixStream, sync::Arc, time::Duration};

use calloop::EventLoop;
use reis::{
    calloop::{EisRequestSource, EisRequestSourceEvent},
    eis::{self, button::ButtonState, device::DeviceType, keyboard::KeyState},
    request::{Connection, DeviceCapability, EisRequest, Seat},
};
use tracing::{debug, warn};

use super::input::{InputCommand, VirtualInput};

/// Creates a socketpair, runs an EIS server on one end bound to `input`, and
/// returns the other end for the application.
///
/// # Errors
///
/// Returns an error if the socketpair or the server thread cannot be created.
pub fn connect(input: Arc<VirtualInput>) -> Result<OwnedFd, String> {
    let (server, client) = UnixStream::pair().map_err(|e| format!("socketpair: {e}"))?;
    std::thread::Builder::new()
        .name("wayle-eis".to_owned())
        .spawn(move || {
            if let Err(err) = run(server, input) {
                warn!(%err, "eis server ended");
            }
        })
        .map_err(|e| format!("cannot spawn eis thread: {e}"))?;
    Ok(OwnedFd::from(client))
}

/// Runs the EIS event loop until the client disconnects.
fn run(stream: UnixStream, input: Arc<VirtualInput>) -> Result<(), String> {
    let context = eis::Context::new(stream).map_err(|e| format!("eis context: {e}"))?;
    let mut event_loop = EventLoop::<ServerState>::try_new().map_err(|e| format!("calloop: {e}"))?;
    let source = EisRequestSource::new(context, 1);

    let mut ctx = ContextState {
        input,
        bound: false,
        seat: None,
    };
    event_loop
        .handle()
        .insert_source(source, move |event, connection, state: &mut ServerState| {
            Ok(match event {
                Ok(event) => {
                    let action = ctx.handle(connection, event);
                    if action == calloop::PostAction::Remove {
                        state.done = true;
                    }
                    action
                }
                Err(err) => {
                    debug!(%err, "eis client error");
                    state.done = true;
                    calloop::PostAction::Remove
                }
            })
        })
        .map_err(|e| format!("insert eis source: {e}"))?;

    let mut state = ServerState { done: false };
    while !state.done {
        event_loop
            .dispatch(Duration::from_millis(200), &mut state)
            .map_err(|e| format!("eis dispatch: {e}"))?;
    }
    Ok(())
}

/// calloop loop data.
struct ServerState {
    done: bool,
}

/// Per-connection EIS state.
struct ContextState {
    input: Arc<VirtualInput>,
    bound: bool,
    /// The advertised seat, kept alive for the connection's lifetime.
    seat: Option<Seat>,
}

impl ContextState {
    fn handle(&mut self, connection: &Connection, event: EisRequestSourceEvent) -> calloop::PostAction {
        match event {
            EisRequestSourceEvent::Connected => {
                self.seat = Some(connection.add_seat(
                    Some("wayle"),
                    DeviceCapability::Pointer
                        | DeviceCapability::PointerAbsolute
                        | DeviceCapability::Keyboard
                        | DeviceCapability::Scroll
                        | DeviceCapability::Button,
                ));
            }
            EisRequestSourceEvent::Request(EisRequest::Disconnect) => {
                return calloop::PostAction::Remove;
            }
            EisRequestSourceEvent::Request(EisRequest::Bind(bind)) => {
                self.add_devices(connection, &bind);
            }
            EisRequestSourceEvent::Request(request) => self.inject(request),
        }
        let _ = connection.flush();
        calloop::PostAction::Continue
    }

    /// Advertises virtual devices for the capabilities the client bound.
    fn add_devices(&mut self, _connection: &Connection, bind: &reis::request::Bind) {
        if self.bound {
            return;
        }
        let caps = bind.capabilities;
        if caps.contains(DeviceCapability::Keyboard) {
            bind.seat
                .add_device(Some("wayle-keyboard"), DeviceType::Virtual, DeviceCapability::Keyboard.into(), |_| {});
        }
        if caps.contains(DeviceCapability::Pointer) {
            bind.seat.add_device(
                Some("wayle-pointer"),
                DeviceType::Virtual,
                DeviceCapability::Pointer | DeviceCapability::Button | DeviceCapability::Scroll,
                |_| {},
            );
        }
        self.bound = true;
    }

    /// Maps an EIS input request onto a [`VirtualInput`] command.
    #[allow(clippy::cognitive_complexity)]
    fn inject(&self, request: EisRequest) {
        match request {
            EisRequest::PointerMotion(motion) => self.input.send(InputCommand::PointerMotion {
                dx: f64::from(motion.dx),
                dy: f64::from(motion.dy),
            }),
            EisRequest::Button(button) => self.input.send(InputCommand::PointerButton {
                button: button.button,
                pressed: button.state == ButtonState::Press,
            }),
            EisRequest::ScrollDelta(scroll) => {
                if scroll.dy != 0.0 {
                    self.input
                        .send(InputCommand::PointerAxis { axis: 0, value: f64::from(scroll.dy) });
                }
                if scroll.dx != 0.0 {
                    self.input
                        .send(InputCommand::PointerAxis { axis: 1, value: f64::from(scroll.dx) });
                }
            }
            EisRequest::ScrollDiscrete(scroll) => {
                // EIS discrete scroll is in 120ths of a step.
                if scroll.discrete_dy != 0 {
                    self.input.send(InputCommand::PointerAxisDiscrete {
                        axis: 0,
                        steps: scroll.discrete_dy / 120,
                    });
                }
                if scroll.discrete_dx != 0 {
                    self.input.send(InputCommand::PointerAxisDiscrete {
                        axis: 1,
                        steps: scroll.discrete_dx / 120,
                    });
                }
            }
            EisRequest::KeyboardKey(key) => self.input.send(InputCommand::Key {
                keycode: key.key,
                pressed: key.state == KeyState::Press,
            }),
            EisRequest::TextKeysym(text) => self.input.send(InputCommand::Keysym {
                keysym: text.keysym,
                pressed: text.state == KeyState::Press,
            }),
            _ => {}
        }
    }
}
