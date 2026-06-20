use wayle_derive::wayle_enum;

/// Page shown when the screen-share picker opens.
#[wayle_enum(default)]
pub enum SharePickerPage {
    /// Per-window previews.
    #[default]
    Windows,
    /// Per-output (monitor) previews.
    Outputs,
    /// Region selection via an external tool (e.g. `slurp`).
    Region,
}
