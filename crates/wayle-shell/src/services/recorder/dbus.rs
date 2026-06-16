//! D-Bus interface for screen recorder control.

use tracing::instrument;
use zbus::interface;

use super::state::RecorderState;

pub const SERVICE_NAME: &str = "com.wayle.Recorder1";
pub const SERVICE_PATH: &str = "/com/wayle/Recorder";

pub struct RecorderDaemon {
    state: RecorderState,
}

impl RecorderDaemon {
    pub fn new(state: RecorderState) -> Self {
        Self { state }
    }
}

#[interface(name = "com.wayle.Recorder1")]
impl RecorderDaemon {
    #[instrument(skip(self))]
    pub async fn start(&self) {
        self.state.start().await;
    }

    #[instrument(skip(self))]
    pub async fn stop(&self) {
        self.state.stop();
    }

    #[instrument(skip(self))]
    pub async fn toggle(&self) {
        self.state.toggle().await;
    }

    #[instrument(skip(self))]
    pub async fn pause(&self) {
        self.state.set_paused(true);
    }

    #[instrument(skip(self))]
    pub async fn resume(&self) {
        self.state.set_paused(false);
    }

    #[zbus(property)]
    pub async fn active(&self) -> bool {
        self.state.active.get()
    }

    #[zbus(property)]
    pub async fn paused(&self) -> bool {
        self.state.paused.get()
    }

    /// Elapsed recording time in seconds.
    #[zbus(property)]
    pub async fn elapsed(&self) -> u32 {
        self.state.elapsed_secs.get()
    }

    /// Path of the current/last output file.
    #[zbus(property)]
    pub async fn file(&self) -> String {
        self.state.file_path.get()
    }
}
