use wayle_treeman::TreemanStatus;

/// Substitutes the `{{ key }}` placeholders in a label format string with the
/// current bucket counts.
pub(crate) fn format_label(format: &str, status: &TreemanStatus) -> String {
    format
        .replace("{{ total }}", &status.total.to_string())
        .replace("{{ stable }}", &status.stable.to_string())
        .replace("{{ up }}", &status.up.to_string())
        .replace("{{ down }}", &status.down.to_string())
        .replace("{{ failed }}", &status.failed.to_string())
}

/// The severity CSS class for the button root, mirroring treeman's waybar
/// `class`: `"failed"` when any worktree errored, `"active"` when preparing or
/// tearing down, otherwise `None`.
pub(crate) fn severity_class(status: &TreemanStatus) -> Option<&'static str> {
    match status.class.as_str() {
        "failed" => Some("failed"),
        "active" => Some("active"),
        _ => None,
    }
}
