use wayle_config::schemas::wallpaper::{CyclingMode as CfgCyclingMode, FitMode as CfgFitMode};
use wayle_wallpaper::{CyclingMode, FitMode};

pub(crate) fn fit_mode(cfg: CfgFitMode) -> FitMode {
    match cfg {
        CfgFitMode::Fill => FitMode::Fill,
        CfgFitMode::Fit => FitMode::Fit,
        CfgFitMode::Center => FitMode::Center,
        CfgFitMode::Stretch => FitMode::Stretch,
    }
}

pub(crate) fn cycling_mode(cfg: CfgCyclingMode) -> CyclingMode {
    match cfg {
        CfgCyclingMode::Sequential => CyclingMode::Sequential,
        CfgCyclingMode::Shuffle => CyclingMode::Shuffle,
    }
}
