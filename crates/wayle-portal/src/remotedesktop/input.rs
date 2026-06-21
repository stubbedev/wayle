//! Virtual input injection for RemoteDesktop.
//!
//! Runs a dedicated thread owning a Wayland connection and the
//! `zwlr_virtual_pointer_v1` + `zwp_virtual_keyboard_v1` devices. The portal's
//! `Notify*` methods send [`InputCommand`]s over a channel; the thread replays
//! them and flushes. The virtual keyboard mirrors the seat's current keymap (we
//! capture it from `wl_keyboard.keymap`), so evdev keycodes from the client map
//! correctly without pulling in libxkbcommon.

use std::{
    os::fd::{AsFd, OwnedFd},
    sync::mpsc,
    time::Instant,
};

use tracing::{debug, warn};
use wayland_client::{
    Connection, Dispatch, QueueHandle, delegate_noop,
    protocol::{
        wl_keyboard::{self, WlKeyboard},
        wl_pointer::{Axis, ButtonState},
        wl_registry,
        wl_seat::{self, WlSeat},
    },
};
use wayland_protocols_misc::zwp_virtual_keyboard_v1::client::{
    zwp_virtual_keyboard_manager_v1::ZwpVirtualKeyboardManagerV1,
    zwp_virtual_keyboard_v1::ZwpVirtualKeyboardV1,
};
use wayland_protocols_wlr::virtual_pointer::v1::client::{
    zwlr_virtual_pointer_manager_v1::ZwlrVirtualPointerManagerV1,
    zwlr_virtual_pointer_v1::ZwlrVirtualPointerV1,
};

/// One input event to replay onto the virtual devices.
#[derive(Debug, Clone)]
pub enum InputCommand {
    /// Relative pointer motion in logical pixels.
    PointerMotion { dx: f64, dy: f64 },
    /// Absolute pointer motion within a stream's pixel extent.
    PointerMotionAbsolute {
        x: f64,
        y: f64,
        width: u32,
        height: u32,
    },
    /// Pointer button (evdev code) press/release.
    PointerButton { button: u32, pressed: bool },
    /// Smooth scroll on an axis (0 = vertical, 1 = horizontal).
    PointerAxis { axis: u32, value: f64 },
    /// Discrete scroll steps on an axis.
    PointerAxisDiscrete { axis: u32, steps: i32 },
    /// Key (evdev keycode) press/release.
    Key { keycode: u32, pressed: bool },
    /// Key by keysym press/release (resolved to a keycode via the keymap).
    Keysym { keysym: u32, pressed: bool },
}

/// Handle to the virtual-input thread; dropping it tears the thread down.
pub struct VirtualInput {
    tx: mpsc::Sender<InputCommand>,
}

impl VirtualInput {
    /// Spawns the virtual-input thread and binds the devices.
    ///
    /// # Errors
    ///
    /// Returns an error if Wayland is unreachable or the compositor lacks the
    /// virtual-pointer / virtual-keyboard protocols.
    pub fn spawn() -> Result<Self, String> {
        let (tx, rx) = mpsc::channel::<InputCommand>();
        let (ready_tx, ready_rx) = mpsc::channel::<Result<(), String>>();
        std::thread::Builder::new()
            .name("wayle-virtual-input".to_owned())
            .spawn(move || run(&rx, &ready_tx))
            .map_err(|e| format!("cannot spawn virtual-input thread: {e}"))?;
        ready_rx
            .recv()
            .map_err(|_| "virtual-input thread exited during setup".to_owned())??;
        Ok(Self { tx })
    }

    /// Queues a command for the input thread. Best-effort: a closed channel
    /// (thread gone) is logged and dropped.
    pub fn send(&self, command: InputCommand) {
        if self.tx.send(command).is_err() {
            warn!("virtual-input thread is gone; dropping input event");
        }
    }
}

/// Globals bound during setup.
#[derive(Default)]
struct Globals {
    seat: Option<WlSeat>,
    pointer_manager: Option<ZwlrVirtualPointerManagerV1>,
    keyboard_manager: Option<ZwpVirtualKeyboardManagerV1>,
    keymap: Option<(u32, OwnedFd, u32)>,
}

/// Thread body: bind globals, create devices, then replay commands.
fn run(rx: &mpsc::Receiver<InputCommand>, ready: &mpsc::Sender<Result<(), String>>) {
    let (connection, devices) = match setup() {
        Ok(parts) => parts,
        Err(err) => {
            let _ = ready.send(Err(err));
            return;
        }
    };
    let _ = ready.send(Ok(()));

    let mut start = None;
    for command in rx.iter() {
        let time = elapsed_ms(&mut start);
        devices.apply(&command, time);
        // Virtual devices emit no events; flushing pushes the requests out.
        let _ = connection.flush();
    }
    debug!("virtual-input thread stopping");
}

/// The created virtual devices.
/// `KEY_LEFTSHIFT` evdev keycode, for typing shifted keysyms.
#[cfg(feature = "keysym")]
const KEY_LEFTSHIFT: u32 = 42;

struct Devices {
    pointer: ZwlrVirtualPointerV1,
    keyboard: ZwpVirtualKeyboardV1,
    /// keysym -> (evdev keycode, needs shift), from the seat keymap.
    #[cfg(feature = "keysym")]
    keysym_map: std::collections::HashMap<u32, (u32, bool)>,
}

impl Devices {
    /// Replays one command at `time` (ms).
    fn apply(&self, command: &InputCommand, time: u32) {
        match *command {
            InputCommand::PointerMotion { dx, dy } => {
                self.pointer.motion(time, dx, dy);
                self.pointer.frame();
            }
            InputCommand::PointerMotionAbsolute {
                x,
                y,
                width,
                height,
            } => {
                self.pointer.motion_absolute(
                    time,
                    x.max(0.0) as u32,
                    y.max(0.0) as u32,
                    width.max(1),
                    height.max(1),
                );
                self.pointer.frame();
            }
            InputCommand::PointerButton { button, pressed } => {
                self.pointer.button(time, button, button_state(pressed));
                self.pointer.frame();
            }
            InputCommand::PointerAxis { axis, value } => {
                self.pointer.axis(time, axis_of(axis), value);
                self.pointer.frame();
            }
            InputCommand::PointerAxisDiscrete { axis, steps } => {
                // 120 units per detent is the standard high-resolution step.
                self.pointer
                    .axis_discrete(time, axis_of(axis), f64::from(steps) * 15.0, steps);
                self.pointer.frame();
            }
            InputCommand::Key { keycode, pressed } => {
                self.keyboard.key(time, keycode, u32::from(pressed));
            }
            InputCommand::Keysym { keysym, pressed } => self.inject_keysym(keysym, pressed, time),
        }
    }

    /// Types a keysym by resolving it to a keycode (+ Shift) via the keymap.
    fn inject_keysym(&self, keysym: u32, pressed: bool, time: u32) {
        #[cfg(feature = "keysym")]
        if let Some(&(keycode, needs_shift)) = self.keysym_map.get(&keysym) {
            if pressed {
                if needs_shift {
                    self.keyboard.key(time, KEY_LEFTSHIFT, 1);
                }
                self.keyboard.key(time, keycode, 1);
            } else {
                self.keyboard.key(time, keycode, 0);
                if needs_shift {
                    self.keyboard.key(time, KEY_LEFTSHIFT, 0);
                }
            }
        } else {
            tracing::debug!(keysym, "no keycode for keysym in the active keymap");
        }
        #[cfg(not(feature = "keysym"))]
        {
            let _ = (keysym, pressed, time);
            tracing::debug!("keysym injection needs the `keysym` feature");
        }
    }
}

/// Connects, binds globals, and creates the virtual devices + keymap.
fn setup() -> Result<(Connection, Devices), String> {
    let connection =
        Connection::connect_to_env().map_err(|e| format!("cannot connect to wayland: {e}"))?;
    let mut queue = connection.new_event_queue();
    let handle = queue.handle();
    connection.display().get_registry(&handle, ());

    let mut globals = Globals::default();
    queue
        .roundtrip(&mut globals)
        .map_err(|e| format!("wayland roundtrip failed: {e}"))?;
    // A second roundtrip lets the seat advertise its keyboard + keymap.
    queue
        .roundtrip(&mut globals)
        .map_err(|e| format!("wayland roundtrip failed: {e}"))?;

    let seat = globals.seat.clone().ok_or("no wl_seat")?;
    let pointer_manager = globals
        .pointer_manager
        .clone()
        .ok_or("compositor lacks zwlr_virtual_pointer_manager_v1")?;
    let keyboard_manager = globals
        .keyboard_manager
        .clone()
        .ok_or("compositor lacks zwp_virtual_keyboard_manager_v1")?;

    let pointer = pointer_manager.create_virtual_pointer(Some(&seat), &handle, ());
    let keyboard = keyboard_manager.create_virtual_keyboard(&seat, &handle, ());

    #[cfg(feature = "keysym")]
    let mut keysym_map = std::collections::HashMap::new();
    if let Some((format, fd, size)) = &globals.keymap {
        keyboard.keymap(*format, fd.as_fd(), *size);
        queue
            .roundtrip(&mut globals)
            .map_err(|e| format!("wayland roundtrip failed: {e}"))?;
        #[cfg(feature = "keysym")]
        {
            keysym_map = build_keysym_map(fd, *size);
        }
    } else {
        warn!("no seat keymap captured; virtual keyboard may not map keys");
    }

    Ok((
        connection,
        Devices {
            pointer,
            keyboard,
            #[cfg(feature = "keysym")]
            keysym_map,
        },
    ))
}

/// Builds a `keysym -> (evdev keycode, needs shift)` map from the seat's xkb
/// keymap (read from the keymap memfd). Lower levels win, so unshifted bindings
/// take precedence.
#[cfg(feature = "keysym")]
#[allow(clippy::cognitive_complexity)]
fn build_keysym_map(fd: &OwnedFd, _size: u32) -> std::collections::HashMap<u32, (u32, bool)> {
    use std::io::Read;

    use xkbcommon::xkb;

    let mut map = std::collections::HashMap::new();
    let Ok(clone) = fd.try_clone() else {
        return map;
    };
    let mut text = String::new();
    if std::fs::File::from(clone).read_to_string(&mut text).is_err() {
        return map;
    }
    let text = text.trim_end_matches('\0').to_owned();

    let context = xkb::Context::new(xkb::CONTEXT_NO_FLAGS);
    let Some(keymap) = xkb::Keymap::new_from_string(
        &context,
        text,
        xkb::KEYMAP_FORMAT_TEXT_V1,
        xkb::KEYMAP_COMPILE_NO_FLAGS,
    ) else {
        return map;
    };

    for raw_kc in keymap.min_keycode().raw()..=keymap.max_keycode().raw() {
        // wl_keyboard / virtual-keyboard use evdev codes = xkb keycode - 8.
        let Some(evdev) = raw_kc.checked_sub(8) else {
            continue;
        };
        let keycode = xkb::Keycode::new(raw_kc);
        let levels = keymap.num_levels_for_key(keycode, 0);
        for level in 0..levels {
            for sym in keymap.key_get_syms_by_level(keycode, 0, level) {
                let raw = sym.raw();
                if raw != 0 {
                    map.entry(raw).or_insert((evdev, level >= 1));
                }
            }
        }
    }
    map
}

/// Milliseconds since the first call (lazily started).
fn elapsed_ms(start: &mut Option<Instant>) -> u32 {
    let start = start.get_or_insert_with(Instant::now);
    start.elapsed().as_millis() as u32
}

/// Maps a portal axis index to a `wl_pointer` axis.
fn axis_of(axis: u32) -> Axis {
    if axis == 1 {
        Axis::HorizontalScroll
    } else {
        Axis::VerticalScroll
    }
}

/// Maps pressed/released to the `wl_pointer` button state.
fn button_state(pressed: bool) -> ButtonState {
    if pressed {
        ButtonState::Pressed
    } else {
        ButtonState::Released
    }
}

impl Dispatch<wl_registry::WlRegistry, ()> for Globals {
    fn event(
        state: &mut Self,
        registry: &wl_registry::WlRegistry,
        event: wl_registry::Event,
        _data: &(),
        _conn: &Connection,
        handle: &QueueHandle<Self>,
    ) {
        let wl_registry::Event::Global {
            name,
            interface,
            version,
        } = event
        else {
            return;
        };
        match interface.as_str() {
            "wl_seat" => {
                let seat: WlSeat = registry.bind(name, version.min(7), handle, ());
                state.seat = Some(seat);
            }
            "zwlr_virtual_pointer_manager_v1" => {
                state.pointer_manager = Some(registry.bind(name, version.min(2), handle, ()));
            }
            "zwp_virtual_keyboard_manager_v1" => {
                state.keyboard_manager = Some(registry.bind(name, version.min(1), handle, ()));
            }
            _ => {}
        }
    }
}

impl Dispatch<WlSeat, ()> for Globals {
    fn event(
        _state: &mut Self,
        seat: &WlSeat,
        event: wl_seat::Event,
        _data: &(),
        _conn: &Connection,
        handle: &QueueHandle<Self>,
    ) {
        if let wl_seat::Event::Capabilities { capabilities } = event
            && let wayland_client::WEnum::Value(caps) = capabilities
            && caps.contains(wl_seat::Capability::Keyboard)
        {
            // Grab a wl_keyboard so the compositor sends us its keymap.
            seat.get_keyboard(handle, ());
        }
    }
}

impl Dispatch<WlKeyboard, ()> for Globals {
    fn event(
        state: &mut Self,
        _keyboard: &WlKeyboard,
        event: wl_keyboard::Event,
        _data: &(),
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        if let wl_keyboard::Event::Keymap { format, fd, size } = event {
            let format = match format {
                wayland_client::WEnum::Value(value) => value as u32,
                wayland_client::WEnum::Unknown(value) => value,
            };
            state.keymap = Some((format, fd, size));
        }
    }
}

delegate_noop!(Globals: ignore ZwlrVirtualPointerManagerV1);
delegate_noop!(Globals: ignore ZwlrVirtualPointerV1);
delegate_noop!(Globals: ignore ZwpVirtualKeyboardManagerV1);
delegate_noop!(Globals: ignore ZwpVirtualKeyboardV1);
