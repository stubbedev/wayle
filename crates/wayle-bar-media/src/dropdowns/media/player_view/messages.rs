use std::{sync::Arc, time::Duration};

use wayle_media::{
    MediaService,
    core::player::Player,
    types::{LoopMode, PlaybackState, ShuffleMode},
};

pub struct PlayerViewInit {
    pub media: Arc<MediaService>,
}

#[derive(Debug)]
pub enum PlayerViewInput {
    SetActive(bool),
    ShowSourcePickerClicked,
    PlayPauseClicked,
    NextClicked,
    PreviousClicked,
    ShuffleClicked,
    LoopClicked,
    SeekCommitted(f64),
}

#[derive(Debug)]
pub enum PlayerViewOutput {
    ShowSourcePicker,
}

#[derive(Debug)]
pub enum PlayerViewCmd {
    PlayerChanged(Option<Arc<Player>>),
    MetadataChanged,
    PlaybackStateChanged(PlaybackState),
    PositionTick(Duration),
    CapabilitiesChanged,
    LoopModeChanged(LoopMode),
    ShuffleModeChanged(ShuffleMode),
    Noop,
}
