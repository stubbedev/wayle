//! sway implementation of [`KeyboardLayoutSource`].
//!
//! sway exposes the active layout as a single reactive property (refreshed
//! from `GET_INPUTS` on each `input` event), so the source watches it directly.

use std::sync::Arc;

use futures::{StreamExt, stream::BoxStream};
use wayle_sway::SwayService;

use super::{CurrentLayout, KeyboardLayoutSource};

pub struct SwayKeyboardLayoutSource {
    service: Arc<SwayService>,
}

impl SwayKeyboardLayoutSource {
    pub fn new(service: Arc<SwayService>) -> Self {
        Self { service }
    }
}

impl KeyboardLayoutSource for SwayKeyboardLayoutSource {
    fn snapshot(&self) -> Option<CurrentLayout> {
        self.service.keyboard_layout.get().map(current_layout_from)
    }

    fn changes(&self) -> BoxStream<'static, Option<CurrentLayout>> {
        let updates = self
            .service
            .keyboard_layout
            .watch()
            .skip(1)
            .map(|layout| layout.map(current_layout_from));
        Box::pin(updates)
    }
}

fn current_layout_from(label: String) -> CurrentLayout {
    CurrentLayout { label }
}
