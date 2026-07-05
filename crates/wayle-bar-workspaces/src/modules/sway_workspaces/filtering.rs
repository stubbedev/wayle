//! Filter and order the workspace list for display.
//!
//! All workspaces are shown by default, including empty ones, matching sway's
//! dynamic workspace model. `hide-trailing-empty` drops the trailing run of
//! empty workspaces per output; ignore patterns and monitor scoping filter the
//! rest. No placeholder workspaces are ever fabricated.

use std::collections::HashMap;

use super::helpers;

/// Plain-data view of one workspace, used as input to [`collect_displayed`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WorkspaceSnapshot {
    pub id: u64,
    pub num: i32,
    pub name: Option<String>,
    pub output: Option<String>,
    pub is_urgent: bool,
    pub is_active: bool,
    pub is_focused: bool,
    pub has_windows: bool,
}

/// Inputs that govern filtering, ordering, and padding.
#[derive(Debug, Clone)]
pub struct FilterContext<'a> {
    pub monitor_specific: bool,
    pub bar_monitor: Option<&'a str>,
    pub hide_trailing_empty: bool,
    pub ignore_patterns: &'a [String],
}

pub fn collect_displayed(
    snapshots: Vec<WorkspaceSnapshot>,
    ctx: &FilterContext<'_>,
) -> Vec<WorkspaceSnapshot> {
    let trailing_empty_ids = if ctx.hide_trailing_empty {
        compute_trailing_empties(&snapshots)
    } else {
        Vec::new()
    };

    let mut workspaces: Vec<WorkspaceSnapshot> = snapshots
        .into_iter()
        .filter(|snapshot| visible_on_monitor(snapshot, ctx))
        .filter(|snapshot| {
            !helpers::is_ignored(
                snapshot.name.as_deref(),
                snapshot.num,
                snapshot.id,
                ctx.ignore_patterns,
            )
        })
        .filter(|snapshot| !trailing_empty_ids.contains(&snapshot.id))
        .collect();

    workspaces.sort_by_key(sort_key);
    workspaces
}

fn visible_on_monitor(snapshot: &WorkspaceSnapshot, ctx: &FilterContext<'_>) -> bool {
    if !ctx.monitor_specific {
        return true;
    }
    let Some(bar_monitor) = ctx.bar_monitor else {
        return true;
    };
    snapshot
        .output
        .as_deref()
        .is_some_and(|output| output == bar_monitor)
}

fn compute_trailing_empties(snapshots: &[WorkspaceSnapshot]) -> Vec<u64> {
    let mut last_per_output: HashMap<String, OutputTail> = HashMap::new();

    for snapshot in snapshots {
        let Some(output) = snapshot.output.clone() else {
            continue;
        };
        let candidate = OutputTail {
            num: snapshot.num,
            id: snapshot.id,
            is_empty: !snapshot.has_windows,
        };
        last_per_output
            .entry(output)
            .and_modify(|tail| {
                if candidate.num > tail.num {
                    *tail = candidate.clone();
                }
            })
            .or_insert(candidate);
    }

    last_per_output
        .into_values()
        .filter(|tail| tail.is_empty)
        .map(|tail| tail.id)
        .collect()
}

#[derive(Clone)]
struct OutputTail {
    num: i32,
    id: u64,
    is_empty: bool,
}

/// Orders workspaces by output, then by sway number. Unnumbered workspaces
/// (`num == -1`) sort after numbered ones on the same output.
fn sort_key(snapshot: &WorkspaceSnapshot) -> (String, i32, u64) {
    let output = snapshot.output.clone().unwrap_or_default();
    let num = if snapshot.num < 0 {
        i32::MAX
    } else {
        snapshot.num
    };
    (output, num, snapshot.id)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn occupied(id: u64, num: i32, output: &str) -> WorkspaceSnapshot {
        WorkspaceSnapshot {
            id,
            num,
            name: Some(num.to_string()),
            output: Some(output.to_string()),
            is_urgent: false,
            is_active: false,
            is_focused: false,
            has_windows: true,
        }
    }

    fn named_empty(id: u64, num: i32, output: &str, name: &str) -> WorkspaceSnapshot {
        WorkspaceSnapshot {
            id,
            num,
            name: Some(name.to_string()),
            output: Some(output.to_string()),
            is_urgent: false,
            is_active: false,
            is_focused: false,
            has_windows: false,
        }
    }

    fn empty(id: u64, num: i32, output: &str) -> WorkspaceSnapshot {
        WorkspaceSnapshot {
            id,
            num,
            name: Some(num.to_string()),
            output: Some(output.to_string()),
            is_urgent: false,
            is_active: false,
            is_focused: false,
            has_windows: false,
        }
    }

    fn ctx_default<'a>() -> FilterContext<'a> {
        FilterContext {
            monitor_specific: false,
            bar_monitor: None,
            hide_trailing_empty: false,
            ignore_patterns: &[],
        }
    }

    fn ids(displayed: &[WorkspaceSnapshot]) -> Vec<u64> {
        displayed.iter().map(|snapshot| snapshot.id).collect()
    }

    #[test]
    fn shows_empty_workspaces_when_hide_trailing_disabled() {
        let snapshots = vec![
            occupied(1, 1, "DP-1"),
            empty(2, 2, "DP-1"),
            named_empty(3, 3, "DP-1", "web"),
        ];
        let displayed = collect_displayed(snapshots, &ctx_default());
        assert_eq!(ids(&displayed), vec![1, 2, 3]);
    }

    #[test]
    fn hide_trailing_empty_drops_last_empty_per_output() {
        let snapshots = vec![
            occupied(1, 1, "DP-1"),
            occupied(2, 2, "DP-1"),
            empty(3, 3, "DP-1"),
        ];
        let ctx = FilterContext {
            hide_trailing_empty: true,
            ..ctx_default()
        };
        let displayed = collect_displayed(snapshots, &ctx);
        assert_eq!(ids(&displayed), vec![1, 2]);
    }

    #[test]
    fn workspace_ignore_drops_by_name_glob() {
        let snapshots = vec![
            occupied(1, 1, "DP-1"),
            occupied(2, 2, "DP-1"),
            named_empty(3, 3, "DP-1", "scratch"),
        ];
        let patterns = vec![String::from("scratch")];
        let ctx = FilterContext {
            ignore_patterns: &patterns,
            ..ctx_default()
        };
        let displayed = collect_displayed(snapshots, &ctx);
        assert_eq!(ids(&displayed), vec![1, 2]);
    }

    #[test]
    fn ordering_default_is_by_output_then_num() {
        let snapshots = vec![
            occupied(3, 2, "DP-2"),
            occupied(1, 2, "DP-1"),
            occupied(2, 1, "DP-2"),
        ];
        let displayed = collect_displayed(snapshots, &ctx_default());
        assert_eq!(ids(&displayed), vec![1, 2, 3]);
    }
}
