//! Niri implementation of [`KeyboardLayoutSource`].
//!
//! Niri's `events()` returns a `'static` stream, so we adapt it directly.

use std::sync::Arc;

use futures::{
    StreamExt,
    stream::{self, BoxStream},
};
use wayle_niri::{Event, NiriService};

use super::{CurrentLayout, KeyboardLayoutSource};

pub struct NiriKeyboardLayoutSource {
    service: Arc<NiriService>,
}

impl NiriKeyboardLayoutSource {
    pub const fn new(service: Arc<NiriService>) -> Self {
        Self { service }
    }
}

impl KeyboardLayoutSource for NiriKeyboardLayoutSource {
    fn snapshot(&self) -> Option<CurrentLayout> {
        current_layout_from(&self.service)
    }

    fn changes(&self) -> BoxStream<'static, Option<CurrentLayout>> {
        let service = Arc::clone(&self.service);
        let initial = current_layout_from(&service);
        let updates = service.events().filter_map(move |event| {
            let relevant = matches!(
                event,
                Event::KeyboardLayoutsChanged { .. } | Event::KeyboardLayoutSwitched { .. }
            );
            let layout = relevant.then(|| current_layout_from(&service));
            async move { layout }
        });

        Box::pin(stream::once(async move { initial }).chain(updates))
    }
}

fn current_layout_from(service: &NiriService) -> Option<CurrentLayout> {
    let layouts = service.keyboard_layouts.get()?;
    let label = layouts.names.get(usize::from(layouts.current_idx))?.clone();
    Some(CurrentLayout { label })
}
