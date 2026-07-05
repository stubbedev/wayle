//! Data model mirroring `treeman status --format json`.
//!
//! treeman is the source of truth for the bucket derivation; this module only
//! deserializes the aggregated shape it emits. Keep these structs in lockstep
//! with `statusData`/`statusRepo`/`statusWt` in treeman's `cmd/treeman/cmd/status.go`.

use serde::{Deserialize, Serialize};

/// Deserializes a possibly-`null` JSON value into `T::default()`.
///
/// Go marshals an empty slice as `null` rather than `[]`, so `#[serde(default)]`
/// alone (which only fires on an absent key) is not enough — the key is present
/// with a `null` value. This turns that `null` into the default.
fn null_default<'de, D, T>(deserializer: D) -> Result<T, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Default + Deserialize<'de>,
{
    Ok(Option::deserialize(deserializer)?.unwrap_or_default())
}

/// Aggregated worktree health across every registered repo.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct TreemanStatus {
    /// Total active worktrees across all repos.
    pub total: u32,
    /// Worktrees in the resting-ready bucket.
    pub stable: u32,
    /// Worktrees preparing (finalize in flight).
    pub up: u32,
    /// Worktrees tearing down.
    pub down: u32,
    /// Worktrees whose last finalize errored.
    pub failed: u32,
    /// Worst non-resting condition present: `""`, `"active"`, or `"failed"`.
    #[serde(default, deserialize_with = "null_default")]
    pub class: String,
    /// Per-repo breakdown, in the order treeman lists them.
    #[serde(default, deserialize_with = "null_default")]
    pub repos: Vec<TreemanRepo>,
}

/// One registered repo and its active worktrees.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct TreemanRepo {
    /// Repo directory basename.
    pub repo: String,
    /// Worktree count in this repo.
    pub total: u32,
    /// The repo's worktrees.
    #[serde(default, deserialize_with = "null_default")]
    pub worktrees: Vec<TreemanWorktree>,
}

/// One worktree row within a repo.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct TreemanWorktree {
    /// Branch name (`-` when detached / unknown).
    pub branch: String,
    /// treeman-derived slug.
    pub slug: String,
    /// Fine-grained lifecycle state (`ready`, `preparing`, `error`, …).
    pub state: String,
    /// Coarse bucket the state maps to: `stable`, `up`, `down`, or `failed`.
    pub bucket: String,
    /// Whether this is the repo's main worktree.
    pub is_main: bool,
    /// Absolute worktree path.
    pub path: String,
}

/// The four buckets a worktree can occupy — mirrors treeman's bucket strings.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Bucket {
    /// Resting-ready.
    Stable,
    /// Preparing.
    Up,
    /// Tearing down.
    Down,
    /// Last finalize errored.
    Failed,
}

impl Bucket {
    /// Parses treeman's bucket string, defaulting unknown values to [`Bucket::Stable`].
    #[must_use]
    pub fn parse(s: &str) -> Self {
        match s {
            "up" => Self::Up,
            "down" => Self::Down,
            "failed" => Self::Failed,
            _ => Self::Stable,
        }
    }
}

impl TreemanStatus {
    /// The most severe bucket present, for a single at-a-glance glyph:
    /// failed > down > up > stable.
    #[must_use]
    pub fn worst_bucket(&self) -> Bucket {
        if self.failed > 0 {
            Bucket::Failed
        } else if self.down > 0 {
            Bucket::Down
        } else if self.up > 0 {
            Bucket::Up
        } else {
            Bucket::Stable
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_null_slices_as_empty() {
        // treeman emits `null` (not `[]`) for empty repo/worktree lists.
        let json = r#"{"total":0,"stable":0,"up":0,"down":0,"failed":0,"class":null,"repos":null}"#;
        let s: TreemanStatus = serde_json::from_str(json).unwrap();
        assert_eq!(s, TreemanStatus::default());
        assert_eq!(s.worst_bucket(), Bucket::Stable);
    }

    #[test]
    fn parses_populated_status_and_picks_worst() {
        let json = r#"{
            "total":3,"stable":1,"up":1,"down":0,"failed":1,"class":"failed",
            "repos":[{"repo":"wayle","total":3,"worktrees":[
                {"branch":"master","slug":"wayle-master","state":"ready","bucket":"stable","is_main":true,"path":"/w/wayle"},
                {"branch":"feat","slug":"wayle-feat","state":"preparing","bucket":"up","is_main":false,"path":"/w/feat"},
                {"branch":"bug","slug":"wayle-bug","state":"error","bucket":"failed","is_main":false,"path":"/w/bug"}
            ]}]
        }"#;
        let s: TreemanStatus = serde_json::from_str(json).unwrap();
        assert_eq!(s.total, 3);
        assert_eq!(s.repos.len(), 1);
        assert_eq!(s.repos[0].worktrees.len(), 3);
        assert_eq!(s.worst_bucket(), Bucket::Failed);
    }
}
