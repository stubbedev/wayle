use wayle_treeman::{Bucket, TreemanStatus};

/// Substitutes the `{{ key }}` placeholders in a label format string with the
/// current bucket counts.
pub fn format_label(format: &str, status: &TreemanStatus) -> String {
    format
        .replace("{{ total }}", &status.total.to_string())
        .replace("{{ stable }}", &status.stable.to_string())
        .replace("{{ up }}", &status.up.to_string())
        .replace("{{ down }}", &status.down.to_string())
        .replace("{{ failed }}", &status.failed.to_string())
}

/// The severity CSS class for the button root, one per non-resting bucket so
/// setup and teardown are each visible on the bar rather than collapsing into
/// treeman's single `active` class. `None` when everything is resting-ready.
pub const fn severity_class(status: &TreemanStatus) -> Option<&'static str> {
    match status.worst_bucket() {
        Bucket::Failed => Some("failed"),
        Bucket::Down => Some("tearing-down"),
        Bucket::Up => Some("preparing"),
        Bucket::Stable => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn status(up: u32, down: u32, failed: u32) -> TreemanStatus {
        TreemanStatus {
            total: 1 + up + down + failed,
            stable: 1,
            up,
            down,
            failed,
            ..TreemanStatus::default()
        }
    }

    #[test]
    fn severity_tracks_worst_bucket() {
        assert_eq!(severity_class(&status(0, 0, 0)), None);
        assert_eq!(severity_class(&status(1, 0, 0)), Some("preparing"));
        assert_eq!(severity_class(&status(1, 1, 0)), Some("tearing-down"));
        assert_eq!(severity_class(&status(1, 1, 1)), Some("failed"));
    }
}
