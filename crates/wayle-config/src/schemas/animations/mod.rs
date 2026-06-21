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

    /// Duration in ms for in-place dropdown transitions: hover highlights and
    /// the page/stack crossfades inside dropdowns. `enabled = false` removes
    /// them entirely.
    #[serde(rename = "interaction-duration")]
    #[default(150u32)]
    pub interaction_duration: ConfigProperty<u32>,

    /// Base duration in ms for general UI micro-transitions (hover, focus, and
    /// color fades) driven by the CSS `--duration-*` token family. Fast,
    /// normal, and slow speeds are derived from this. `enabled = false` zeroes
    /// them for an instant UI.
    #[serde(rename = "ui-duration")]
    #[default(250u32)]
    pub ui_duration: ConfigProperty<u32>,

    /// Run looping status indicators: spinners, network/bluetooth scan
    /// animations, the recording pulse, and the clock blink. Disable (or set
    /// `enabled = false`) for a fully static UI.
    #[default(true)]
    pub indicators: ConfigProperty<bool>,

    /// Per-surface override for notification popup cards.
    #[default(SurfaceAnimation::default())]
    pub notifications: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for the OSD (volume/brightness/toggle/toast).
    #[default(SurfaceAnimation::default())]
    pub osd: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for bar widget dropdown foldouts.
    #[default(SurfaceAnimation::default())]
    pub dropdown: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for the screen-share picker overlay.
    #[serde(rename = "share-picker")]
    #[default(SurfaceAnimation::default())]
    pub share_picker: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for the wallpaper crossfade between images.
    #[default(SurfaceAnimation::default())]
    pub wallpaper: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for the power menu overlay.
    #[default(SurfaceAnimation::default())]
    pub power: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for portal dialogs (access/account/app-chooser/
    /// launcher-install prompts).
    #[default(SurfaceAnimation::default())]
    pub dialog: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for the portal file-chooser surface.
    #[serde(rename = "file-chooser")]
    #[default(SurfaceAnimation::default())]
    pub file_chooser: ConfigProperty<SurfaceAnimation>,

    /// Per-surface override for the portal print surface.
    #[default(SurfaceAnimation::default())]
    pub print: ConfigProperty<SurfaceAnimation>,
}

/// Transient surface an animation applies to.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AnimSurface {
    /// Notification popup cards.
    Notifications,
    /// The OSD overlay (volume/brightness/toggle/toast).
    Osd,
    /// Bar widget dropdown foldouts.
    Dropdown,
    /// The screen-share picker overlay.
    SharePicker,
    /// The wallpaper crossfade between images.
    Wallpaper,
    /// The power menu overlay.
    Power,
    /// Portal dialogs (access / account / app-chooser / launcher install).
    Dialog,
    /// The portal file-chooser surface.
    FileChooser,
    /// The portal print surface.
    Print,
}

impl AnimationsConfig {
    fn surface(&self, surface: AnimSurface) -> SurfaceAnimation {
        match surface {
            AnimSurface::Notifications => self.notifications.get(),
            AnimSurface::Osd => self.osd.get(),
            AnimSurface::Dropdown => self.dropdown.get(),
            AnimSurface::SharePicker => self.share_picker.get(),
            AnimSurface::Wallpaper => self.wallpaper.get(),
            AnimSurface::Power => self.power.get(),
            AnimSurface::Dialog => self.dialog.get(),
            AnimSurface::FileChooser => self.file_chooser.get(),
            AnimSurface::Print => self.print.get(),
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

    /// Resolved duration in ms for in-place dropdown transitions (hover
    /// highlights, page/stack crossfades). `0` when animations are disabled.
    #[must_use]
    pub fn interaction_duration_ms(&self) -> u32 {
        if self.enabled.get() {
            self.interaction_duration.get()
        } else {
            0
        }
    }

    /// CSS custom-property overrides that bridge the animation config into the
    /// stylesheet. Returns a `:root { … }` block setting the `--duration-*`
    /// token family from `ui-duration` (scaled to fast/normal/slow) and, when
    /// indicators are off, freezing every looping keyframe via `--cfg-anim-*`.
    ///
    /// Disabling animations zeroes the durations so CSS transitions are
    /// instant. Emitted after the static stylesheet so it overrides the
    /// compile-time token defaults.
    #[must_use]
    pub fn css_overrides(&self) -> String {
        let on = self.enabled.get();
        let ui = if on { self.ui_duration.get() } else { 0 };
        let scaled = |factor: f32| (ui as f32 * factor).round() as u32;

        let indicators_on = on && self.indicators.get();
        let frozen = if indicators_on {
            String::new()
        } else {
            String::from(
                "    --cfg-anim-spin: 0s;\n    --cfg-anim-media-spin: 0s;\n    \
                 --cfg-anim-scan: 0s;\n    --cfg-anim-blink: 0s;\n    --cfg-anim-pulse: 0s;\n",
            )
        };

        format!(
            ":root {{\n    --duration-super-fast: {sf}ms;\n    --duration-fast: {f}ms;\n    \
             --duration-normal: {n}ms;\n    --duration-slow: {s}ms;\n{frozen}}}",
            sf = scaled(0.3),
            f = scaled(0.6),
            n = ui,
            s = scaled(1.4),
        )
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
