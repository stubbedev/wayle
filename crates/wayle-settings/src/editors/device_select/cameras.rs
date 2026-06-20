//! V4L2 camera enumeration from `/sys/class/video4linux` plus a `VIDIOC_QUERYCAP`
//! probe per node.
//!
//! Mirrors the recorder popover's camera scan so the settings page offers the
//! same list. A single physical camera exposes several `/dev/videoN` nodes
//! (capture, metadata, output); only those that actually advertise
//! `V4L2_CAP_VIDEO_CAPTURE` can be recorded, so the metadata/output nodes are
//! filtered out — picking the lowest node blindly would otherwise select a node
//! that produces no picture. The card name comes straight from the driver.

use std::{fs, mem, os::fd::AsRawFd};

use super::DeviceChoice;

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
    let end = cap.card.iter().position(|&b| b == 0).unwrap_or(cap.card.len());
    let card = String::from_utf8_lossy(&cap.card[..end]).trim().to_owned();
    (!card.is_empty()).then_some(card)
}

/// Lists capture-capable V4L2 cameras as `(/dev/videoN, card)` choices, one per
/// distinct card (lowest node), prefixed with an "Automatic" entry (empty id =
/// auto-select first camera).
pub(super) fn cameras() -> Vec<DeviceChoice> {
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
