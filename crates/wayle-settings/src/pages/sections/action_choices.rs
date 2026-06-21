//! Predefined click/scroll action choices offered per module in the action
//! editor. "None" and "Custom command…" are appended by the editor itself, so
//! they are not listed here.

use wayle_config::{ClickAction, schemas::modules::WorkspaceClickAction};

use crate::editors::action::ActionChoice;

fn shell(label: &str, command: &str) -> ActionChoice<ClickAction> {
    ActionChoice {
        label: label.to_owned(),
        action: ClickAction::Shell(command.to_owned()),
    }
}

fn dropdown(label: &str, id: &str) -> ActionChoice<ClickAction> {
    ActionChoice {
        label: label.to_owned(),
        action: ClickAction::Dropdown(id.to_owned()),
    }
}

/// Predefined actions for a module's click/scroll fields, keyed by the module's
/// page id. Unknown ids get no predefined choices (just None + Custom).
pub(crate) fn choices_for(module_id: &str) -> Vec<ActionChoice<ClickAction>> {
    match module_id {
        "recorder" => vec![
            shell("Toggle recording", "wayle recorder toggle"),
            shell("Start recording", "wayle recorder start"),
            shell("Stop recording", "wayle recorder stop"),
            shell("Pause recording", "wayle recorder pause"),
            shell("Resume recording", "wayle recorder resume"),
            dropdown("Open recorder panel", "recorder"),
        ],
        "screenshot" => vec![
            shell("Capture region", "wayle screenshot region"),
            shell("Capture output", "wayle screenshot output"),
            shell("Capture window", "wayle screenshot window"),
        ],
        "battery" => vec![dropdown("Open battery panel", "battery")],
        "bluetooth" => vec![dropdown("Open bluetooth panel", "bluetooth")],
        "brightness" => vec![dropdown("Open brightness panel", "brightness")],
        "network" => vec![dropdown("Open network panel", "network")],
        "media" => vec![
            shell("Play / pause", "wayle media play-pause"),
            shell("Next track", "wayle media next"),
            shell("Previous track", "wayle media previous"),
            dropdown("Open media panel", "media"),
        ],
        "notifications" | "notification" => vec![
            shell("Toggle do not disturb", "wayle notify dnd"),
            dropdown("Open notifications panel", "notification"),
        ],
        "mail" => vec![dropdown("Open mail panel", "mail")],
        "weather" => vec![dropdown("Open weather panel", "weather")],
        "dashboard" => vec![dropdown("Open dashboard", "dashboard")],
        "clock" => vec![
            dropdown("Open calendar", "calendar"),
            dropdown("Open weather panel", "weather"),
        ],
        "world-clock" => vec![dropdown("Open calendar", "calendar")],
        "hyprsunset" => vec![shell("Toggle night light", ":toggle")],
        "power-profiles" => vec![shell("Cycle profile", ":cycle")],
        "power" => vec![shell("Open power menu", ":menu")],
        "idle-inhibit" => vec![
            shell("Toggle idle inhibit", "wayle idle toggle"),
            shell(
                "Toggle idle inhibit (indefinite)",
                "wayle idle toggle --indefinite",
            ),
        ],
        "volume" => vec![
            shell("Toggle output mute", "wayle audio output-mute"),
            shell("Volume up (+5%)", "wayle audio output-volume +5"),
            shell("Volume down (-5%)", "wayle audio output-volume -5"),
            dropdown("Open audio panel", "audio"),
        ],
        "microphone" => vec![
            shell("Toggle input mute", "wayle audio input-mute"),
            shell("Mic volume up (+5%)", "wayle audio input-volume +5"),
            shell("Mic volume down (-5%)", "wayle audio input-volume -5"),
            dropdown("Open audio panel", "audio"),
        ],
        "cava" => vec![dropdown("Open audio panel", "audio")],
        _ => Vec::new(),
    }
}

/// Predefined actions for workspace modules (niri / mango / hyprland), which
/// use [`WorkspaceClickAction`] (focus actions on top of dropdown/shell).
pub(crate) fn workspace_choices() -> Vec<ActionChoice<WorkspaceClickAction>> {
    vec![
        ActionChoice {
            label: "Focus workspace".to_owned(),
            action: WorkspaceClickAction::FocusWorkspace,
        },
        ActionChoice {
            label: "Focus next".to_owned(),
            action: WorkspaceClickAction::FocusNext,
        },
        ActionChoice {
            label: "Focus previous".to_owned(),
            action: WorkspaceClickAction::FocusPrevious,
        },
        ActionChoice {
            label: "Focus last".to_owned(),
            action: WorkspaceClickAction::FocusLast,
        },
    ]
}
