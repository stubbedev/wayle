mod types;

use schemars::schema_for;
pub use types::{AnimationType, SurfaceAnimation};
use wayle_derive::wayle_config;

use crate::{
    ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
};

/// Enter/exit and change animations for transient surfaces.
#[wayle_config(i18n_prefix = "settings-animations")]
pub struct AnimationsConfig {
    /// Enable enter/exit animations (OSD, toasts, notifications) and
    /// icon-change crossfades. When disabled, surfaces appear instantly.
    #[default(true)]
    pub enabled: ConfigProperty<bool>,

    /// Animation duration in milliseconds.
    #[default(200u32)]
    pub duration: ConfigProperty<u32>,

    /// Transition style used for enter/exit of the OSD, toasts, and
    /// notification cards. Base fallback for every surface and direction.
    #[default(AnimationType::default())]
    pub transition: ConfigProperty<AnimationType>,

    /// Global enter transition. Unset → `transition`.
    #[default(None)]
    pub enter: ConfigProperty<Option<AnimationType>>,

    /// Global exit transition. Unset → `transition`.
    #[default(None)]
    pub exit: ConfigProperty<Option<AnimationType>>,

    /// Global enter duration in ms. Unset → `duration`.
    #[serde(rename = "enter-duration")]
    #[default(None)]
    pub enter_duration: ConfigProperty<Option<u32>>,

    /// Global exit duration in ms. Unset → `duration`.
    #[serde(rename = "exit-duration")]
    #[default(None)]
    pub exit_duration: ConfigProperty<Option<u32>>,

    /// Per-surface override for notification popup cards.
    #[default(SurfaceAnimation::default())]
    pub notifications: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for the OSD (volume/brightness/toggle).
    #[default(SurfaceAnimation::default())]
    pub osd: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for toasts (`wayle toast`).
    #[default(SurfaceAnimation::default())]
    pub toast: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for bar widget dropdown foldouts.
    #[default(SurfaceAnimation::default())]
    pub dropdown: ConfigProperty<SurfaceAnimation>,
}

/// Transient surface an animation applies to.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AnimSurface {
    /// Notification popup cards.
    Notifications,
    /// The OSD overlay (volume/brightness/toggle).
    Osd,
    /// Toasts shown via `wayle toast`.
    Toast,
    /// Bar widget dropdown foldouts.
    Dropdown,
}

impl AnimationsConfig {
    fn surface(&self, surface: AnimSurface) -> SurfaceAnimation {
        match surface {
            AnimSurface::Notifications => self.notifications.get(),
            AnimSurface::Osd => self.osd.get(),
            AnimSurface::Toast => self.toast.get(),
            AnimSurface::Dropdown => self.dropdown.get(),
        }
    }

    /// Resolved transition for a surface and direction. Honors `enabled` and
    /// the surface → global → `transition` fallback cascade.
    #[must_use]
    pub fn transition_for(&self, surface: AnimSurface, exiting: bool) -> AnimationType {
        if !self.enabled.get() {
            return AnimationType::None;
        }
        let sa = self.surface(surface);
        let (per_surface, global) = if exiting {
            (sa.exit, self.exit.get())
        } else {
            (sa.enter, self.enter.get())
        };
        per_surface
            .or(global)
            .unwrap_or_else(|| self.transition.get())
    }

    /// Resolved duration in ms for a surface and direction. `0` when disabled.
    #[must_use]
    pub fn duration_for(&self, surface: AnimSurface, exiting: bool) -> u32 {
        if !self.enabled.get() {
            return 0;
        }
        let sa = self.surface(surface);
        let (per_surface, global) = if exiting {
            (sa.exit_duration, self.exit_duration.get())
        } else {
            (sa.enter_duration, self.enter_duration.get())
        };
        per_surface
            .or(global)
            .unwrap_or_else(|| self.duration.get())
    }
}

impl ModuleInfoProvider for AnimationsConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("animations"),
            schema: || schema_for!(AnimationsConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

crate::register_module!(AnimationsConfig);
