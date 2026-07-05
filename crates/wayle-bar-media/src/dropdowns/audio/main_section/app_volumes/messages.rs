use std::sync::Arc;

use wayle_audio::{AudioService, core::stream::AudioStream};
use wayle_config::ConfigService;

pub struct AppVolumesInit {
    pub audio: Arc<AudioService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum AppVolumesInput {
    AppVolumeChanged(u32, f64),
    ToggleAppMute(u32),
}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum AppVolumesCmd {
    PlaybackStreamsChanged(Vec<Arc<AudioStream>>),
    AppStreamPropertyChanged(u32),
    AppIconSourceChanged,
}
