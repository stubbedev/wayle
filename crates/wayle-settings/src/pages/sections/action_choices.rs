//! Predefined click/scroll action choices offered per module in the action
//! editor. "None" and "Custom command…" are appended by the editor itself, so
//! they are not listed here.

use wayle_config::ClickAction;

use crate::editors::action::ActionChoice;

fn shell(label: &str, command: &str) -> ActionChoice {
    ActionChoice {
        label: label.to_owned(),
        action: ClickAction::Shell(command.to_owned()),
    }
}

fn dropdown(label: &str, id: &str) -> ActionChoice {
    ActionChoice {
        label: label.to_owned(),
        action: ClickAction::Dropdown(id.to_owned()),
    }
}

/// Predefined actions for a module's click/scroll fields, keyed by the module's
/// page id. Unknown ids get no predefined choices (just None + Custom).
pub(crate) fn choices_for(module_id: &str) -> Vec<ActionChoice> {
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
        "media" => vec![dropdown("Open media panel", "media")],
        "notifications" | "notification" => {
            vec![dropdown("Open notifications panel", "notification")]
        }
        "mail" => vec![dropdown("Open mail panel", "mail")],
        "weather" => vec![dropdown("Open weather panel", "weather")],
        "dashboard" => vec![dropdown("Open dashboard", "dashboard")],
        "clock" | "world-clock" => vec![dropdown("Open calendar", "calendar")],
        "volume" | "microphone" | "cava" => vec![dropdown("Open audio panel", "audio")],
        _ => Vec::new(),
    }
}
