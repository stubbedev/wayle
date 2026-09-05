//! Remembers a manual filter toggle across restarts.
//!
//! The filter's on/off state lives in the running hyprsunset process, which
//! dies with the shell — so a shell restart (a config switch, an upgrade) used
//! to silently drop a manually enabled filter and let the auto-schedule turn it
//! back off. The toggle is recorded in the XDG state dir instead and replayed
//! at startup.
//!
//! The record is only honoured while it still describes *this* solar phase: the
//! override expires at the next sunrise/sunset exactly as an in-memory one
//! does, and a record older than [`MAX_AGE`] is discarded outright so a machine
//! booted days later starts from the schedule.

use std::fs;

use chrono::{DateTime, Duration, Utc};
use tracing::debug;
use wayle_core::paths::ConfigPaths;

use super::solar::Phase;

const FILE: &str = "hyprsunset-override";

/// Beyond this, a same-phase record may span a whole day/night cycle and no
/// longer describes an override the user would expect to still be in force.
const MAX_AGE: Duration = Duration::hours(12);

#[derive(Debug, Clone, Copy)]
pub struct Override {
    /// Solar phase the toggle was made in, when the auto-schedule was running.
    pub phase: Option<Phase>,
    pub enabled: bool,
}

pub fn save(phase: Option<Phase>, enabled: bool) {
    let Ok(dir) = ConfigPaths::state_dir() else {
        return;
    };

    let phase = match phase {
        Some(Phase::Day) => "day",
        Some(Phase::Night) => "night",
        None => "none",
    };
    let enabled = if enabled { "on" } else { "off" };
    let body = format!("{phase} {enabled} {}\n", Utc::now().to_rfc3339());

    if let Err(err) = fs::write(dir.join(FILE), body) {
        debug!(%err, "could not record hyprsunset override");
    }
}

pub fn clear() {
    let Ok(dir) = ConfigPaths::state_dir() else {
        return;
    };
    let _ = fs::remove_file(dir.join(FILE));
}

pub fn load() -> Option<Override> {
    let dir = ConfigPaths::state_dir().ok()?;
    let body = fs::read_to_string(dir.join(FILE)).ok()?;
    parse(&body, Utc::now())
}

fn parse(body: &str, now: DateTime<Utc>) -> Option<Override> {
    let mut fields = body.split_whitespace();
    let phase = fields.next()?;
    let enabled = fields.next()?;
    let saved_at = DateTime::parse_from_rfc3339(fields.next()?)
        .ok()?
        .with_timezone(&Utc);

    if now.signed_duration_since(saved_at) > MAX_AGE {
        return None;
    }

    let phase = match phase {
        "day" => Some(Phase::Day),
        "night" => Some(Phase::Night),
        "none" => None,
        _ => return None,
    };

    Some(Override {
        phase,
        enabled: enabled == "on",
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn now() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2026-09-05T12:00:00Z")
            .unwrap()
            .with_timezone(&Utc)
    }

    #[test]
    fn parses_a_fresh_record() {
        let rec = parse("day on 2026-09-05T11:59:00Z", now()).unwrap();
        assert_eq!(rec.phase, Some(Phase::Day));
        assert!(rec.enabled);
    }

    #[test]
    fn parses_an_off_record_without_a_phase() {
        let rec = parse("none off 2026-09-05T11:00:00Z", now()).unwrap();
        assert_eq!(rec.phase, None);
        assert!(!rec.enabled);
    }

    #[test]
    fn discards_a_stale_record() {
        assert!(parse("night on 2026-09-04T12:00:00Z", now()).is_none());
    }

    #[test]
    fn discards_a_malformed_record() {
        assert!(parse("night on", now()).is_none());
        assert!(parse("dusk on 2026-09-05T11:59:00Z", now()).is_none());
        assert!(parse("", now()).is_none());
    }
}
