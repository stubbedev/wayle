const BYTES_PER_KB: f64 = 1024.0;

pub struct FormattedSpeed {
    pub value: String,
    pub is_megabytes: bool,
}

/// Formats bytes per second into a human-readable speed value and unit flag.
pub fn format_speed(bytes_per_sec: u64) -> FormattedSpeed {
    #[expect(
        clippy::as_conversions,
        clippy::cast_precision_loss,
        reason = "speed display; precision loss at >4 PiB/s is irrelevant"
    )]
    let kbps = bytes_per_sec as f64 / BYTES_PER_KB;
    if kbps < BYTES_PER_KB {
        FormattedSpeed {
            value: format!("{kbps:.1}"),
            is_megabytes: false,
        }
    } else {
        let mbps = kbps / BYTES_PER_KB;
        FormattedSpeed {
            value: format!("{mbps:.1}"),
            is_megabytes: true,
        }
    }
}
