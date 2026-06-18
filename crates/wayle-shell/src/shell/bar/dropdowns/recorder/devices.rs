//! Capture-device enumeration for the recorder popover.
//!
//! Microphone sources come from the reactive [`AudioService`]; webcams are read
//! from `/sys/class/video4linux` (no extra dependency, no `v4l2` ioctls). Both
//! are presented as `(id, label)` pairs where an empty `id` means "default /
//! auto-select", matching how the recorder pipeline treats an empty device.

use std::{fs, sync::Arc};

use wayle_audio::AudioService;

/// A selectable capture device: `id` is what the pipeline consumes
/// (`pulsesrc device=<id>` / `v4l2src device=<id>`), `label` is shown to the
/// user. An empty `id` is the "default" / "auto" entry.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(super) struct DeviceChoice {
    pub id: String,
    pub label: String,
}

/// Lists microphone sources from the audio service, newest snapshot.
///
/// Monitor sources (loopback of outputs) are excluded — they belong to "system
/// audio", not the microphone. The list is prefixed with a "Default" entry
/// (empty id) so the user can defer to the server's default source.
pub(super) fn microphone_sources(audio: Option<&Arc<AudioService>>) -> Vec<DeviceChoice> {
    let mut choices = vec![DeviceChoice {
        id: String::new(),
        label: String::from("Default"),
    }];
    let Some(audio) = audio else {
        return choices;
    };
    for device in audio.input_devices.get() {
        if device.is_monitor.get() {
            continue;
        }
        let description = device.description.get();
        let label = if description.is_empty() {
            device.name.get()
        } else {
            description
        };
        choices.push(DeviceChoice {
            id: device.name.get(),
            label,
        });
    }
    choices
}

/// Lists V4L2 capture cameras by reading `/sys/class/video4linux`.
///
/// Each `videoN` node exposes a human-readable `name`; we keep one entry per
/// distinct name (a single physical camera often exposes several `/dev/videoN`
/// nodes — metadata, encoded, raw — that share a name) and point it at the
/// lowest-numbered node, which is conventionally the capture node. The list is
/// prefixed with an "Automatic" entry (empty id). Returns just the prefix entry
/// when no cameras exist, so callers can hide the webcam UI on `len() == 1`.
pub(super) fn cameras() -> Vec<DeviceChoice> {
    let mut choices = vec![DeviceChoice {
        id: String::new(),
        label: String::from("Automatic"),
    }];

    let Ok(entries) = fs::read_dir("/sys/class/video4linux") else {
        return choices;
    };

    // Collect (node_index, name) then dedupe by name keeping the lowest node.
    let mut nodes: Vec<(u32, String)> = Vec::new();
    for entry in entries.flatten() {
        let file_name = entry.file_name();
        let node = file_name.to_string_lossy();
        let Some(index) = node
            .strip_prefix("video")
            .and_then(|n| n.parse::<u32>().ok())
        else {
            continue;
        };
        let name = fs::read_to_string(entry.path().join("name"))
            .map(|n| n.trim().to_owned())
            .unwrap_or_default();
        if name.is_empty() {
            continue;
        }
        nodes.push((index, name));
    }
    nodes.sort_by_key(|(index, _)| *index);

    let mut seen: Vec<String> = Vec::new();
    for (index, name) in nodes {
        if seen.contains(&name) {
            continue;
        }
        seen.push(name.clone());
        choices.push(DeviceChoice {
            id: format!("/dev/video{index}"),
            label: name,
        });
    }
    choices
}

/// Index of `id` within `choices`, defaulting to 0 (the "default" entry) when
/// the saved device is no longer present.
pub(super) fn index_of(choices: &[DeviceChoice], id: &str) -> u32 {
    choices
        .iter()
        .position(|choice| choice.id == id)
        .unwrap_or(0) as u32
}
