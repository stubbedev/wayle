use serde::{Deserialize, Deserializer};

use crate::{
    Address, FocusHistoryId, MonitorId, ProcessId, WorkspaceInfo, deserialize_optional_address,
    deserialize_optional_string,
};

/// Window dimensions in pixels.
#[derive(Debug, Deserialize, Clone, PartialEq)]
pub struct ClientSize {
    /// Width in pixels.
    ///
    /// Hyprland may transiently report negative values during resize/animation churn.
    pub width: i32,
    /// Height in pixels.
    ///
    /// Hyprland may transiently report negative values during resize/animation churn.
    pub height: i32,
}

pub(crate) fn deserialize_window_size<'de, D>(deserializer: D) -> Result<ClientSize, D::Error>
where
    D: Deserializer<'de>,
{
    let [width, height]: [i32; 2] = Deserialize::deserialize(deserializer)?;

    Ok(ClientSize { width, height })
}

/// Window position in screen coordinates.
#[derive(Debug, Deserialize, Clone, PartialEq)]
pub struct ClientLocation {
    /// X coordinate in pixels.
    pub x: i32,
    /// Y coordinate in pixels.
    pub y: i32,
}

pub(crate) fn deserialize_window_location<'de, D>(
    deserializer: D,
) -> Result<ClientLocation, D::Error>
where
    D: Deserializer<'de>,
{
    let [x, y]: [i32; 2] = Deserialize::deserialize(deserializer)?;

    Ok(ClientLocation { x, y })
}

/// Window fullscreen state matching Hyprland's `eFullscreenMode`.
#[derive(Debug, Default, Deserialize, Clone, PartialEq)]
#[serde(from = "u8")]
pub enum FullscreenMode {
    /// Not fullscreen.
    #[default]
    None = 0,
    /// Maximized.
    Maximized = 1,
    /// Fullscreen.
    Fullscreen = 2,
    /// Both maximized and fullscreen.
    MaximizedFullscreen = 3,
}

impl From<u8> for FullscreenMode {
    fn from(value: u8) -> Self {
        match value {
            1 => Self::Maximized,
            2 => Self::Fullscreen,
            3 => Self::MaximizedFullscreen,
            _ => Self::None,
        }
    }
}

/// A single window as reported by `j/clients`.
///
/// Only the fields that identify and place a window are required: `address`,
/// `at`, `size`, `workspace`, `monitor`, `class`, `title` and `pid`. Everything
/// else is `#[serde(default)]` so that a Hyprland-side rename costs that one
/// field instead of the whole client list — the failure mode of 0.56.2's
/// `overFullscreen` -> `allowedOverFullscreen` rename, which emptied the client
/// model for the entire session.
#[derive(Debug, Deserialize, Clone, PartialEq)]
#[serde(rename_all = "camelCase")]
pub(crate) struct ClientData {
    pub address: Address,
    #[serde(default)]
    pub mapped: bool,
    #[serde(default)]
    pub hidden: bool,
    #[serde(deserialize_with = "deserialize_window_location")]
    pub at: ClientLocation,
    #[serde(deserialize_with = "deserialize_window_size")]
    pub size: ClientSize,
    pub workspace: WorkspaceInfo,
    #[serde(default)]
    pub floating: bool,
    pub monitor: MonitorId,
    pub class: String,
    pub title: String,
    #[serde(default)]
    pub initial_class: String,
    #[serde(default)]
    pub initial_title: String,
    pub pid: ProcessId,
    #[serde(default)]
    pub xwayland: bool,
    #[serde(default)]
    pub pinned: bool,
    #[serde(default)]
    pub fullscreen: FullscreenMode,
    #[serde(default)]
    pub fullscreen_client: FullscreenMode,
    /// Hyprland 0.56.2 renamed this from `overFullscreen` to
    /// `allowedOverFullscreen`; both spellings are accepted.
    #[serde(rename = "allowedOverFullscreen", alias = "overFullscreen", default)]
    pub over_fullscreen: bool,
    #[serde(default)]
    pub grouped: Vec<Address>,
    #[serde(default)]
    pub tags: Vec<String>,
    #[serde(default, deserialize_with = "deserialize_optional_address")]
    pub swallowing: Option<Address>,
    #[serde(rename = "focusHistoryID", default)]
    pub focus_history_id: FocusHistoryId,
    #[serde(default)]
    pub inhibiting_idle: bool,
    #[serde(default, deserialize_with = "deserialize_optional_string")]
    pub xdg_tag: Option<String>,
    #[serde(default, deserialize_with = "deserialize_optional_string")]
    pub xdg_description: Option<String>,
    #[serde(default)]
    pub content_type: String,
    #[serde(default)]
    pub stable_id: String,
}

#[cfg(test)]
mod tests {
    use serde::Deserialize;

    use super::*;

    #[test]
    fn fullscreen_mode_from_u8_converts_maximized() {
        let mode = FullscreenMode::from(1u8);

        assert_eq!(mode, FullscreenMode::Maximized);
    }

    #[test]
    fn fullscreen_mode_from_u8_converts_fullscreen() {
        let mode = FullscreenMode::from(2u8);

        assert_eq!(mode, FullscreenMode::Fullscreen);
    }

    #[test]
    fn fullscreen_mode_from_u8_converts_combined() {
        let mode = FullscreenMode::from(3u8);

        assert_eq!(mode, FullscreenMode::MaximizedFullscreen);
    }

    #[test]
    fn fullscreen_mode_from_u8_defaults_to_none() {
        assert_eq!(FullscreenMode::from(0u8), FullscreenMode::None);
        assert_eq!(FullscreenMode::from(99u8), FullscreenMode::None);
    }

    #[test]
    fn deserialize_window_size_creates_correct_struct() {
        #[derive(Deserialize)]
        struct TestStruct {
            #[serde(deserialize_with = "deserialize_window_size")]
            size: ClientSize,
        }

        let json = r#"{"size": [1920, 1080]}"#;
        let result: TestStruct = serde_json::from_str(json).unwrap();

        assert_eq!(result.size.width, 1920);
        assert_eq!(result.size.height, 1080);
    }

    #[test]
    fn deserialize_window_location_creates_correct_struct() {
        #[derive(Deserialize)]
        struct TestStruct {
            #[serde(deserialize_with = "deserialize_window_location")]
            location: ClientLocation,
        }

        let json = r#"{"location": [100, 200]}"#;
        let result: TestStruct = serde_json::from_str(json).unwrap();

        assert_eq!(result.location.x, 100);
        assert_eq!(result.location.y, 200);
    }

    /// A real `hyprctl clients -j` entry from Hyprland 0.56.2, which emits
    /// `allowedOverFullscreen` and no `overFullscreen`.
    const CLIENT_0_56_2: &str = r#"{
        "address": "0x55d1a0", "mapped": true, "hidden": false, "visible": true,
        "acceptsInput": true, "at": [1, 29], "size": [992, 1410],
        "workspace": {"id": 1, "name": "1"}, "floating": false, "monitor": 1,
        "class": "Alacritty", "title": "Alacritty", "initialClass": "Alacritty",
        "initialTitle": "Alacritty", "pid": 4700, "xwayland": false,
        "pinned": false, "pinFullscreened": false, "fullscreen": 0,
        "fullscreenClient": 0, "fullscreenHandler": "default",
        "allowedOverFullscreen": true, "grouped": [], "tags": [],
        "swallowing": "0x0", "focusHistoryID": 0, "inhibitingIdle": false,
        "xdgTag": "", "xdgDescription": "", "contentType": "none",
        "tearingHint": false, "stableId": "18000003"
    }"#;

    #[test]
    fn client_data_parses_hyprland_0_56_2_allowed_over_fullscreen() {
        let client: ClientData = serde_json::from_str(CLIENT_0_56_2).unwrap();

        assert!(client.over_fullscreen);
        assert_eq!(client.class, "Alacritty");
        assert_eq!(client.stable_id, "18000003");
    }

    #[test]
    fn client_data_parses_legacy_over_fullscreen_spelling() {
        let json = CLIENT_0_56_2.replace("allowedOverFullscreen", "overFullscreen");

        let client: ClientData = serde_json::from_str(&json).unwrap();

        assert!(client.over_fullscreen);
    }

    #[test]
    fn client_data_defaults_over_fullscreen_when_neither_spelling_is_sent() {
        let json = CLIENT_0_56_2.replace("\"allowedOverFullscreen\": true,", "");

        let client: ClientData = serde_json::from_str(&json).unwrap();

        assert!(!client.over_fullscreen);
    }

    #[test]
    fn client_data_survives_a_renamed_optional_field() {
        let json = CLIENT_0_56_2.replace("\"stableId\"", "\"stableIdentifier\"");

        let client: ClientData = serde_json::from_str(&json).unwrap();

        assert_eq!(client.stable_id, "");
        assert_eq!(client.class, "Alacritty");
    }

    #[test]
    fn client_data_still_rejects_a_missing_identifying_field() {
        let json = CLIENT_0_56_2.replace("\"class\"", "\"klass\"");

        let error = serde_json::from_str::<ClientData>(&json).unwrap_err();

        assert!(
            error.to_string().contains("missing field `class`"),
            "expected a missing-field error for `class`, got: {error}"
        );
    }

    #[test]
    fn deserialize_window_size_accepts_negative_values() {
        #[derive(Deserialize)]
        struct TestStruct {
            #[serde(deserialize_with = "deserialize_window_size")]
            size: ClientSize,
        }

        let json = r#"{"size": [-3, -1]}"#;
        let result: TestStruct = serde_json::from_str(json).unwrap();

        assert_eq!(result.size.width, -3);
        assert_eq!(result.size.height, -1);
    }
}
