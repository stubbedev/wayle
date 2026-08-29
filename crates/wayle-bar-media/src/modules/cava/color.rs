use wayle_config::{
    ConfigService,
    schemas::styling::{ColorValue, CssToken},
};
use wayle_styling::resolve_palette;

/// RGBA color with components normalized to `[0.0, 1.0]`.
pub struct Rgba {
    pub red: f64,
    pub green: f64,
    pub blue: f64,
    pub alpha: f64,
}

pub fn resolve_rgba(color: &ColorValue, config: &ConfigService) -> Rgba {
    let hex = match color {
        ColorValue::Token(token) => {
            let raw_palette = config.config().styling.palette();
            let palette = resolve_palette(&raw_palette, &config.config().styling);
            match token {
                CssToken::BgBase => palette.bg,
                CssToken::BgSurface | CssToken::BgSurfaceElevated => palette.surface,
                CssToken::BgElevated
                | CssToken::BgOverlay
                | CssToken::BgHover
                | CssToken::BgActive
                | CssToken::BgSelected => palette.elevated,

                CssToken::FgDefault | CssToken::FgOnAccent => palette.fg,
                CssToken::FgMuted | CssToken::FgSubtle => palette.fg_muted,

                CssToken::Accent | CssToken::AccentSubtle | CssToken::AccentHover => {
                    palette.primary
                }

                CssToken::Red
                | CssToken::StatusError
                | CssToken::StatusErrorSubtle
                | CssToken::StatusErrorHover
                | CssToken::BorderError => palette.red,

                CssToken::Yellow | CssToken::StatusWarning | CssToken::StatusWarningSubtle => {
                    palette.yellow
                }

                CssToken::Green | CssToken::StatusSuccess | CssToken::StatusSuccessSubtle => {
                    palette.green
                }

                CssToken::Blue | CssToken::StatusInfo | CssToken::StatusInfoSubtle => {
                    palette.blue
                }

                CssToken::BorderSubtle
                | CssToken::BorderDefault
                | CssToken::BorderStrong
                | CssToken::BorderAccent => palette.primary,
            }
        }
        ColorValue::Custom(hex) => hex.to_string(),
        ColorValue::Transparent => {
            return Rgba {
                red: 0.0,
                green: 0.0,
                blue: 0.0,
                alpha: 0.0,
            };
        }
        ColorValue::Auto => {
            let raw_palette = config.config().styling.palette();
            let palette = resolve_palette(&raw_palette, &config.config().styling);
            palette.primary
        }
    };

    parse_hex_rgba(&hex)
}

fn hex_byte(hex: &str, range: std::ops::Range<usize>) -> u8 {
    hex.get(range)
        .and_then(|part| u8::from_str_radix(part, 16).ok())
        .unwrap_or(255)
}

fn parse_hex_rgba(hex: &str) -> Rgba {
    let hex = hex.trim_start_matches('#');
    let (r, g, b, a) = match hex.len() {
        6 => (
            hex_byte(hex, 0..2),
            hex_byte(hex, 2..4),
            hex_byte(hex, 4..6),
            255u8,
        ),
        8 => (
            hex_byte(hex, 0..2),
            hex_byte(hex, 2..4),
            hex_byte(hex, 4..6),
            hex_byte(hex, 6..8),
        ),
        _ => (255, 255, 255, 255),
    };

    Rgba {
        red: f64::from(r) / 255.0,
        green: f64::from(g) / 255.0,
        blue: f64::from(b) / 255.0,
        alpha: f64::from(a) / 255.0,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_hex_6_digit() {
        let color = parse_hex_rgba("#ff0000");
        assert!((color.red - 1.0).abs() < f64::EPSILON);
        assert!(color.green.abs() < f64::EPSILON);
        assert!(color.blue.abs() < f64::EPSILON);
        assert!((color.alpha - 1.0).abs() < f64::EPSILON);
    }

    #[test]
    fn parse_hex_8_digit_with_alpha() {
        let color = parse_hex_rgba("#00ff0080");
        assert!(color.red.abs() < f64::EPSILON);
        assert!((color.green - 1.0).abs() < f64::EPSILON);
        assert!(color.blue.abs() < f64::EPSILON);
        let expected_alpha = 128.0 / 255.0;
        assert!((color.alpha - expected_alpha).abs() < 0.01);
    }
}
