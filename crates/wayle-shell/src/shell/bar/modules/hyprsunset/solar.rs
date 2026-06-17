//! Self-contained sunrise/sunset computation (NOAA sunrise equation) used by
//! the hyprsunset auto-schedule. No network, no external service — given a UTC
//! instant and a latitude/longitude it decides whether it is currently night.

use chrono::{DateTime, Datelike, Utc};

/// Whether the sun is currently below the horizon at the given location.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum Phase {
    Day,
    Night,
}

/// Decide the solar phase at `now` for `(latitude, longitude)` in decimal
/// degrees (north/east positive).
///
/// Handles polar day/night (regions where the sun never rises or never sets on
/// the given date) by returning [`Phase::Night`]/[`Phase::Day`] respectively.
pub(super) fn phase_at(now: DateTime<Utc>, latitude: f64, longitude: f64) -> Phase {
    match sun_times(now, latitude, longitude) {
        SunTimes::Times { sunrise, sunset } => {
            if now >= sunrise && now < sunset {
                Phase::Day
            } else {
                Phase::Night
            }
        }
        SunTimes::PolarDay => Phase::Day,
        SunTimes::PolarNight => Phase::Night,
    }
}

enum SunTimes {
    Times {
        sunrise: DateTime<Utc>,
        sunset: DateTime<Utc>,
    },
    PolarDay,
    PolarNight,
}

/// UTC sunrise/sunset for the calendar date of `now`.
fn sun_times(now: DateTime<Utc>, latitude: f64, longitude: f64) -> SunTimes {
    // Julian day number (integer, at noon) for the date of `now`.
    let jdn = now.date_naive().num_days_from_ce() as f64 + 1_721_425.0;

    // Days since J2000, with leap-second fudge. l_w is longitude WEST.
    let l_w = -longitude;
    let n = (jdn - 2_451_545.0 + 0.0008).round();

    // Mean solar time.
    let j_star = n - l_w / 360.0;

    // Solar mean anomaly (degrees).
    let m = (357.5291 + 0.985_600_28 * j_star).rem_euclid(360.0);
    let m_rad = m.to_radians();

    // Equation of the center (degrees).
    let c = 1.9148 * m_rad.sin() + 0.0200 * (2.0 * m_rad).sin() + 0.0003 * (3.0 * m_rad).sin();

    // Ecliptic longitude (degrees).
    let lambda = (m + c + 180.0 + 102.9372).rem_euclid(360.0);
    let lambda_rad = lambda.to_radians();

    // Solar transit (Julian date of solar noon).
    let j_transit = 2_451_545.0 + j_star + 0.0053 * m_rad.sin() - 0.0069 * (2.0 * lambda_rad).sin();

    // Sun declination.
    let sin_decl = lambda_rad.sin() * 23.4397_f64.to_radians().sin();
    let decl = sin_decl.asin();

    // Hour angle (with -0.833° for refraction + solar disc radius).
    let lat_rad = latitude.to_radians();
    let cos_omega = ((-0.833_f64).to_radians().sin() - lat_rad.sin() * decl.sin())
        / (lat_rad.cos() * decl.cos());

    if cos_omega < -1.0 {
        return SunTimes::PolarDay; // sun never sets
    }
    if cos_omega > 1.0 {
        return SunTimes::PolarNight; // sun never rises
    }

    let omega = cos_omega.acos().to_degrees();

    let j_rise = j_transit - omega / 360.0;
    let j_set = j_transit + omega / 360.0;

    match (julian_to_utc(j_rise), julian_to_utc(j_set)) {
        (Some(sunrise), Some(sunset)) => SunTimes::Times { sunrise, sunset },
        // Numerically degenerate — treat as the safer "day" so the filter stays off.
        _ => SunTimes::PolarDay,
    }
}

/// Convert a Julian Date to a UTC instant.
fn julian_to_utc(jd: f64) -> Option<DateTime<Utc>> {
    let unix_secs = (jd - 2_440_587.5) * 86_400.0;
    DateTime::from_timestamp(unix_secs.round() as i64, 0)
}

#[cfg(test)]
mod tests {
    use chrono::TimeZone;

    use super::*;

    // Copenhagen, summer: long daylight. Noon UTC is day, 02:00 UTC is night.
    #[test]
    fn copenhagen_summer_noon_is_day() {
        let now = Utc.with_ymd_and_hms(2024, 6, 21, 12, 0, 0).unwrap();
        assert_eq!(phase_at(now, 55.6, 12.5), Phase::Day);
    }

    #[test]
    fn copenhagen_summer_predawn_is_night() {
        let now = Utc.with_ymd_and_hms(2024, 6, 21, 1, 0, 0).unwrap();
        assert_eq!(phase_at(now, 55.6, 12.5), Phase::Night);
    }

    // Copenhagen, winter: short daylight. 17:00 UTC is already past sunset.
    #[test]
    fn copenhagen_winter_evening_is_night() {
        let now = Utc.with_ymd_and_hms(2024, 12, 21, 17, 0, 0).unwrap();
        assert_eq!(phase_at(now, 55.6, 12.5), Phase::Night);
    }

    #[test]
    fn copenhagen_winter_midday_is_day() {
        let now = Utc.with_ymd_and_hms(2024, 12, 21, 11, 0, 0).unwrap();
        assert_eq!(phase_at(now, 55.6, 12.5), Phase::Day);
    }

    // Polar night: northern Norway in deep winter — sun never rises.
    #[test]
    fn arctic_midwinter_is_night_all_day() {
        let noon = Utc.with_ymd_and_hms(2024, 12, 21, 12, 0, 0).unwrap();
        assert_eq!(phase_at(noon, 78.2, 15.6), Phase::Night);
    }

    // Polar day: same location in midsummer — sun never sets.
    #[test]
    fn arctic_midsummer_is_day_all_night() {
        let midnight = Utc.with_ymd_and_hms(2024, 6, 21, 0, 0, 0).unwrap();
        assert_eq!(phase_at(midnight, 78.2, 15.6), Phase::Day);
    }
}
