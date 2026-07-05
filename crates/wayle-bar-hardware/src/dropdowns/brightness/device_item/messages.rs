pub struct BrightnessDeviceInit {
    /// Raw device identity used to address the backend (e.g. `intel_backlight`).
    pub name: String,
    /// Human-friendly display name shown as the item title.
    pub title: String,
    pub subtitle: Option<String>,
    pub icon: &'static str,
    pub percentage: f64,
}

#[derive(Debug)]
pub enum BrightnessDeviceItemMsg {
    SetBackendBrightness(f64),
    BrightnessCommitted(f64),
}

#[derive(Debug)]
pub enum BrightnessDeviceItemOutput {
    BrightnessChanged(String, f64),
}
