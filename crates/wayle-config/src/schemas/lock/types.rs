use wayle_derive::wayle_enum;

/// How the lock screen background is rendered.
#[wayle_enum(default)]
pub enum LockBackground {
    /// Solid color fill (`background-color`).
    #[default]
    Color,
    /// A specific image file (`background-image`).
    Image,
    /// Reuse the current desktop wallpaper.
    Wallpaper,
}
