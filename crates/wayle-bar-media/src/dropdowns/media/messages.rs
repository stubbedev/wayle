use std::sync::Arc;

use wayle_config::ConfigService;
use wayle_media::MediaService;

use super::{player_view::PlayerViewOutput, source_picker::SourcePickerOutput};

pub struct MediaDropdownInit {
    pub media: Arc<MediaService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum MediaDropdownMsg {
    PlayerView(PlayerViewOutput),
    SourcePicker(SourcePickerOutput),
    VisibilityChanged(bool),
}

#[derive(Debug)]
pub enum MediaDropdownCmd {
    ScaleChanged(f32),
}
