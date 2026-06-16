//! D-Bus client proxy for the recorder service.
#![allow(missing_docs)]

use zbus::{Result, proxy};

pub const SERVICE_NAME: &str = "com.wayle.Recorder1";
pub const SERVICE_PATH: &str = "/com/wayle/Recorder";

#[proxy(
    interface = "com.wayle.Recorder1",
    default_service = "com.wayle.Recorder1",
    default_path = "/com/wayle/Recorder",
    gen_blocking = false
)]
pub trait Recorder {
    async fn start(&self) -> Result<()>;

    async fn stop(&self) -> Result<()>;

    async fn toggle(&self) -> Result<()>;

    async fn pause(&self) -> Result<()>;

    async fn resume(&self) -> Result<()>;

    #[zbus(property)]
    fn active(&self) -> Result<bool>;

    #[zbus(property)]
    fn paused(&self) -> Result<bool>;

    #[zbus(property)]
    fn elapsed(&self) -> Result<u32>;

    #[zbus(property)]
    fn file(&self) -> Result<String>;
}
