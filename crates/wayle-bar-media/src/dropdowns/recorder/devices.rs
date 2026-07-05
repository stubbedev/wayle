//! Capture-device enumeration for the recorder popover.
//!
//! Microphone sources come from the reactive [`AudioService`]; webcams are read
//! from `/sys/class/video4linux` (no extra dependency, no `v4l2` ioctls). Both
//! are presented as `(id, label)` pairs where an empty `id` means "default /
//! auto-select", matching how the recorder pipeline treats an empty device.

use std::{fs, mem, os::fd::AsRawFd, sync::Arc};

use wayle_audio::AudioService;

/// A selectable capture device: `id` is what the pipeline consumes
/// (`pulsesrc device=<id>` / `v4l2src device=<id>`), `label` is shown to the
/// user. An empty `id` is the "default" / "auto" entry.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DeviceChoice {
    pub id: String,
    pub label: String,
}

/// Lists microphone sources from the audio service, newest snapshot.
///
/// Monitor sources (loopback of outputs) are excluded — they belong to "system
/// audio", not the microphone. The list is prefixed with a "Default" entry
/// (empty id) so the user can defer to the server's default source.
pub fn microphone_sources(audio: Option<&Arc<AudioService>>) -> Vec<DeviceChoice> {
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

/// `struct v4l2_capability` from `linux/videodev2.h` (104 bytes).
#[repr(C)]
struct V4l2Capability {
    driver: [u8; 16],
    card: [u8; 32],
    bus_info: [u8; 32],
    version: u32,
    capabilities: u32,
    device_caps: u32,
    reserved: [u32; 3],
}

/// `VIDIOC_QUERYCAP` = `_IOR('V', 0, struct v4l2_capability)`.
const VIDIOC_QUERYCAP: u64 = 0x8068_5600;
/// `V4L2_CAP_VIDEO_CAPTURE`: the node can capture video.
const V4L2_CAP_VIDEO_CAPTURE: u32 = 0x0000_0001;
/// `V4L2_CAP_DEVICE_CAPS`: `device_caps` is filled and per-node accurate.
const V4L2_CAP_DEVICE_CAPS: u32 = 0x8000_0000;

/// Queries a `/dev/videoN` node; returns its card name if it can capture video.
#[allow(unsafe_code)]
fn capture_card(path: &str) -> Option<String> {
    let file = fs::File::open(path).ok()?;
    let mut cap: V4l2Capability = unsafe { mem::zeroed() };
    // SAFETY: `cap` is a correctly-sized, zeroed buffer for VIDIOC_QUERYCAP.
    let rc = unsafe { libc::ioctl(file.as_raw_fd(), VIDIOC_QUERYCAP as libc::c_ulong, &mut cap) };
    if rc != 0 {
        return None;
    }
    let caps = if cap.capabilities & V4L2_CAP_DEVICE_CAPS != 0 {
        cap.device_caps
    } else {
        cap.capabilities
    };
    if caps & V4L2_CAP_VIDEO_CAPTURE == 0 {
        return None;
    }
    let end = cap
        .card
        .iter()
        .position(|&b| b == 0)
        .unwrap_or(cap.card.len());
    let card = String::from_utf8_lossy(&cap.card[..end]).trim().to_owned();
    (!card.is_empty()).then_some(card)
}

/// Lists V4L2 capture cameras by reading `/sys/class/video4linux` and probing
/// each node with `VIDIOC_QUERYCAP`.
///
/// A single physical camera often exposes several `/dev/videoN` nodes (capture,
/// metadata, output); only nodes that advertise `V4L2_CAP_VIDEO_CAPTURE` yield a
/// picture, so the others are filtered out (picking the lowest node blindly
/// could select a metadata node that records nothing). One entry per distinct
/// card name (lowest capture node), prefixed with an "Automatic" entry (empty
/// id). Returns just the prefix entry when no cameras exist, so callers can hide
/// the webcam UI on `len() == 1`.
pub fn cameras() -> Vec<DeviceChoice> {
    let mut choices = vec![DeviceChoice {
        id: String::new(),
        label: String::from("Automatic"),
    }];

    let Ok(entries) = fs::read_dir("/sys/class/video4linux") else {
        return choices;
    };

    let mut nodes: Vec<u32> = entries
        .flatten()
        .filter_map(|entry| {
            entry
                .file_name()
                .to_string_lossy()
                .strip_prefix("video")
                .and_then(|n| n.parse::<u32>().ok())
        })
        .collect();
    nodes.sort_unstable();

    let mut seen: Vec<String> = Vec::new();
    for index in nodes {
        let path = format!("/dev/video{index}");
        let Some(card) = capture_card(&path) else {
            continue;
        };
        if seen.contains(&card) {
            continue;
        }
        seen.push(card.clone());
        choices.push(DeviceChoice {
            id: path,
            label: card,
        });
    }
    choices
}

/// Index of `id` within `choices`, defaulting to 0 (the "default" entry) when
/// the saved device is no longer present.
pub fn index_of(choices: &[DeviceChoice], id: &str) -> u32 {
    choices
        .iter()
        .position(|choice| choice.id == id)
        .unwrap_or(0) as u32
}
