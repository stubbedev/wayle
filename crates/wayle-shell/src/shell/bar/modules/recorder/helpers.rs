use serde_json::json;

use crate::i18n::t;

/// Recorder state used to render the bar label.
pub(crate) struct LabelContext {
    pub active: bool,
    pub paused: bool,
    pub elapsed_secs: u32,
}

fn format_elapsed(total_secs: u32) -> String {
    let hours = total_secs / 3600;
    let minutes = (total_secs % 3600) / 60;
    let seconds = total_secs % 60;
    if hours > 0 {
        format!("{hours}:{minutes:02}:{seconds:02}")
    } else {
        format!("{minutes}:{seconds:02}")
    }
}

pub(super) fn build_label(format: &str, ctx: &LabelContext) -> String {
    let state = if !ctx.active {
        t!("bar-recorder-idle")
    } else if ctx.paused {
        t!("bar-recorder-paused")
    } else {
        t!("bar-recorder-recording")
    };
    let elapsed = if ctx.active {
        format_elapsed(ctx.elapsed_secs)
    } else {
        String::from("-")
    };

    let template_ctx = json!({
        "state": state,
        "elapsed": elapsed,
    });
    crate::template::render(format, template_ctx).unwrap_or_default()
}

/// Selects the icon for the current recorder state.
///
/// While `preparing` (the pre-capture delay) the recording icon is shown and
/// the bar pulses it via the `recorder-preparing` CSS class.
pub(super) fn select_icon(
    active: bool,
    preparing: bool,
    paused: bool,
    icon_idle: &str,
    icon_recording: &str,
    icon_paused: &str,
) -> String {
    if active {
        if paused {
            icon_paused.to_string()
        } else {
            icon_recording.to_string()
        }
    } else if preparing {
        icon_recording.to_string()
    } else {
        icon_idle.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ctx(active: bool, paused: bool, elapsed_secs: u32) -> LabelContext {
        LabelContext {
            active,
            paused,
            elapsed_secs,
        }
    }

    #[test]
    fn elapsed_formats() {
        assert_eq!(format_elapsed(5), "0:05");
        assert_eq!(format_elapsed(65), "1:05");
        assert_eq!(format_elapsed(3661), "1:01:01");
    }

    #[test]
    fn label_idle_shows_dash() {
        assert_eq!(build_label("{{ elapsed }}", &ctx(false, false, 0)), "-");
    }

    #[test]
    fn label_recording_elapsed() {
        assert_eq!(build_label("{{ elapsed }}", &ctx(true, false, 65)), "1:05");
    }

    #[test]
    fn select_icon_switches() {
        assert_eq!(
            select_icon(false, false, false, "idle", "rec", "pause"),
            "idle"
        );
        assert_eq!(
            select_icon(false, true, false, "idle", "rec", "pause"),
            "rec"
        );
        assert_eq!(
            select_icon(true, false, false, "idle", "rec", "pause"),
            "rec"
        );
        assert_eq!(
            select_icon(true, false, true, "idle", "rec", "pause"),
            "pause"
        );
    }
}
