//! Microphone enumeration via the shell's audio D-Bus service.
//!
//! Mirrors the recorder popover's microphone scan so the settings page offers
//! the same list. The recorder pipeline consumes a PulseAudio/PipeWire *source
//! name* (`pulsesrc device=<id>`); those names cannot be derived from the
//! kernel, so rather than shelling out to `pactl` (absent on PipeWire-only
//! systems) we query the running shell's audio service over the session bus —
//! `zbus` is already a dependency, so this adds no external binary. Returns just
//! the "Default" entry when the service is not running.

use super::DeviceChoice;

/// Minimal proxy for the shell's audio service; only `list_sources` is needed.
/// The full interface lives in `wayle-audio`, but settings does not depend on
/// that crate (it would pull in `libpulse`), so the one method is re-declared.
#[zbus::proxy(
    interface = "com.wayle.Audio1",
    default_service = "com.wayle.Audio1",
    default_path = "/com/wayle/Audio"
)]
trait Audio {
    /// Lists input devices as `(index, name, description)`.
    fn list_sources(&self) -> zbus::Result<Vec<(u32, String, String)>>;
}

/// Lists microphone sources from the audio service, prefixed with a "Default"
/// entry (empty id = the server's default source). Monitor sources (loopback of
/// outputs) are excluded — they belong to "system audio", not the microphone.
/// The stored `id` is the source name consumed by the recorder pipeline as
/// `pulsesrc device=<id>`; the human-readable description is shown as the label.
pub(super) async fn microphones() -> Vec<DeviceChoice> {
    let mut choices = vec![DeviceChoice {
        id: String::new(),
        label: String::from("Default"),
    }];

    let Ok(connection) = zbus::Connection::session().await else {
        return choices;
    };
    let Ok(proxy) = AudioProxy::new(&connection).await else {
        return choices;
    };
    let Ok(sources) = proxy.list_sources().await else {
        return choices;
    };

    for (_index, name, description) in sources {
        if name.is_empty() || name.ends_with(".monitor") {
            continue;
        }
        let label = if description.is_empty() {
            name.clone()
        } else {
            description
        };
        choices.push(DeviceChoice { id: name, label });
    }
    choices
}
