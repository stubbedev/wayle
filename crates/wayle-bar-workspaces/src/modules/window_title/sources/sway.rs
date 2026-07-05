//! sway implementation of [`FocusedWindowSource`].
//!
//! sway has no incremental event stream, so the source derives the focused
//! window from the service's `windows` snapshot and watches it for changes.

use std::{collections::HashMap, sync::Arc};

use futures::{StreamExt, stream::BoxStream};
use wayle_sway::{SwayService, core::Window};

use super::{FocusedWindow, FocusedWindowSource};

pub struct SwayFocusedWindowSource {
    service: Arc<SwayService>,
}

impl SwayFocusedWindowSource {
    pub fn new(service: Arc<SwayService>) -> Self {
        Self { service }
    }
}

impl FocusedWindowSource for SwayFocusedWindowSource {
    fn snapshot(&self) -> Option<FocusedWindow> {
        self.service.focused_window().map(focused_window_from)
    }

    fn changes(&self) -> BoxStream<'static, Option<FocusedWindow>> {
        let updates = self
            .service
            .windows
            .watch()
            .skip(1)
            .map(|windows| focused_in(&windows).map(focused_window_from));
        Box::pin(updates)
    }
}

fn focused_in(windows: &HashMap<u64, Arc<Window>>) -> Option<Arc<Window>> {
    windows
        .values()
        .find(|window| window.is_focused.get())
        .cloned()
}

fn focused_window_from(window: Arc<Window>) -> FocusedWindow {
    FocusedWindow {
        title: window.title.get().unwrap_or_default(),
        app_id: window.app_id.get().unwrap_or_default(),
    }
}
