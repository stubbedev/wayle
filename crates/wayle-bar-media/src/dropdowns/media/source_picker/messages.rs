use std::sync::Arc;

use wayle_media::{MediaService, core::player::Player, types::PlayerId};

pub struct SourcePickerInit {
    pub media: Arc<MediaService>,
}

#[derive(Debug)]
pub enum SourcePickerInput {
    BackClicked,
    SourceSelected(usize),
}

#[derive(Debug)]
pub enum SourcePickerOutput {
    NavigateBack,
}

#[derive(Debug)]
pub enum SourcePickerCmd {
    PlayerListChanged {
        players: Vec<Arc<Player>>,
        active_id: Option<PlayerId>,
    },
}
