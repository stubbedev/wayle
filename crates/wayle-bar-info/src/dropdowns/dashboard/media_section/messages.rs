use std::{sync::Arc, time::Duration};

use wayle_media::{MediaService, types::PlaybackState};

pub struct MediaSectionInit {
    pub media: Option<Arc<MediaService>>,
}

#[derive(Debug)]
pub enum MediaSectionInput {
    SetActive(bool),
    PreviousClicked,
    PlayPauseClicked,
    NextClicked,
    SwitchPlayerClicked,
    SeekCommitted(f64),
}

#[derive(Debug)]
pub enum MediaSectionCmd {
    PlayerChanged,
    PlayerListChanged(usize),
    MetadataChanged {
        title: String,
        artist: String,
        cover_art: Option<String>,
        length: Option<Duration>,
    },
    PlaybackStateChanged(PlaybackState),
    PositionTick(Duration),
    CanSeekChanged(bool),
    Noop,
}
