//! V4L2 camera enumeration from `/sys/class/video4linux` (no `v4l2` ioctls).
//!
//! Mirrors the recorder popover's camera scan so the settings page offers the
//! same list. Kept here rather than shared because the settings binary does not
//! depend on the shell crate.

use std::fs;

use super::DeviceChoice;

/// Lists V4L2 cameras as `(/dev/videoN, name)` choices, one per distinct name,
/// prefixed with an "Automatic" entry (empty id = auto-select first camera).
pub(super) fn cameras() -> Vec<DeviceChoice> {
    let mut choices = vec![DeviceChoice {
        id: String::new(),
        label: String::from("Automatic"),
    }];

    let Ok(entries) = fs::read_dir("/sys/class/video4linux") else {
        return choices;
    };

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
