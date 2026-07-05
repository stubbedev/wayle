use std::sync::Arc;

use wayle_sysinfo::SysinfoService;

pub struct SystemStatsInit {
    pub sysinfo: Arc<SysinfoService>,
    /// Usage percent at which the rings turn warning.
    pub usage_warning: f32,
    /// Usage percent at which the rings turn error.
    pub usage_error: f32,
    /// Temperature (°C) at which the temp ring turns warning.
    pub temp_warning: f32,
    /// Temperature (°C) at which the temp ring turns error.
    pub temp_error: f32,
}

#[derive(Debug)]
pub enum SystemStatsInput {
    SetActive(bool),
}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum SystemStatsCmd {
    CpuChanged { usage: f32, temp: Option<f32> },
    MemoryChanged { usage: f32 },
    DiskChanged { usage: f32 },
}
