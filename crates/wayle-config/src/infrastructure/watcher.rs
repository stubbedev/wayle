use std::{
    collections::HashSet,
    path::{Path, PathBuf},
    sync::Arc,
    time::Duration,
};

use notify::{
    Event, RecommendedWatcher, RecursiveMode, Watcher as NotifyWatcher, event::EventKind,
};
use tokio::sync::{mpsc, watch};
use tracing::{debug, error, info, instrument};

use super::{
    error::Error,
    paths::{ConfigPaths, discover_main_config},
    secrets,
    service::ConfigService,
};
use crate::{
    ApplyConfigLayer, ApplyRuntimeLayer, CommitConfigReload, Config, ResetConfigLayer,
    ResetRuntimeLayer, infrastructure::themes::utils::load_themes,
};

/// Hot-reloads configuration files on disk changes.
///
/// When config files are modified, reloads them and updates corresponding
/// `ConfigProperty` values. Uses `send_if_modified` to prevent circular writes.
#[derive(Clone)]
pub struct FileWatcher {
    config_service: Arc<ConfigService>,
    secrets_tx: watch::Sender<()>,
    _watcher: Arc<RecommendedWatcher>,
}

impl FileWatcher {
    /// Subscribes to secrets reload events.
    ///
    /// The receiver fires whenever `.env` files are reloaded.
    #[must_use]
    pub fn subscribe_secrets_reload(&self) -> watch::Receiver<()> {
        self.secrets_tx.subscribe()
    }
}

impl FileWatcher {
    /// Starts watching config directory for changes.
    ///
    /// # Errors
    ///
    /// Returns error if file watching cannot be initialized.
    #[instrument(skip(config_service))]
    pub fn start(config_service: Arc<ConfigService>) -> Result<Self, Error> {
        let (tx, rx) = mpsc::unbounded_channel();
        let (secrets_tx, _) = watch::channel(());

        let mut watcher = notify::recommended_watcher(move |result: Result<Event, _>| {
            if let Ok(event) = result {
                let _ = tx.send(event);
            }
        })
        .map_err(|source| Error::WatcherInit { source })?;

        let config_dir = ConfigPaths::config_dir()?;

        watcher
            .watch(&config_dir, RecursiveMode::Recursive)
            .map_err(|source| Error::Watch {
                path: config_dir.clone(),
                source,
            })?;

        info!(?config_dir, "Config directory watcher started");

        Self::watch_canonical_dir(&mut watcher, &config_dir);

        let file_watcher = Self {
            config_service,
            secrets_tx,
            _watcher: Arc::new(watcher),
        };

        tokio::spawn(run_debounced_event_loop(file_watcher.clone(), rx));

        Ok(file_watcher)
    }

    /// Also watch the real parent directory when the main config file is a
    /// symlink, so edits applied through the link target are still observed.
    fn watch_canonical_dir(watcher: &mut RecommendedWatcher, config_dir: &Path) {
        let config_path = discover_main_config();
        if let Ok(canonical_path) = config_path.canonicalize()
            && let Some(canonical_dir) = canonical_path.parent()
            && canonical_dir != config_dir
            && !Self::is_immutable_store(canonical_dir)
        {
            if let Err(e) = watcher.watch(canonical_dir, RecursiveMode::NonRecursive) {
                tracing::warn!(error = %e, ?canonical_dir, "failed to watch canonical config folder");
            } else {
                info!(?canonical_dir, "Canonical config folder watcher started");
            }
        }
    }

    /// Whether `dir` is the root of an immutable package store shared with the
    /// rest of the system.
    ///
    /// A config symlinked in by Nix/Home Manager resolves to a file sitting
    /// directly in `/nix/store`, so the canonical parent is the store root.
    /// Watching it means every build, GC, or substitution on the machine floods
    /// this watcher with thousands of unrelated events. There is nothing to
    /// gain either: store paths are immutable, and a rebuild swaps the symlink
    /// in the config dir, which the recursive `config_dir` watch already sees.
    fn is_immutable_store(dir: &Path) -> bool {
        // ponytail: literal store root; honours NIX_STORE_DIR if someone
        // relocated the store, otherwise the compiled-in default.
        let store = std::env::var_os("NIX_STORE_DIR")
            .map_or_else(|| PathBuf::from("/nix/store"), PathBuf::from);
        dir == store
    }

    const fn should_reload(event: &Event) -> bool {
        matches!(
            event.kind,
            EventKind::Create(_) | EventKind::Modify(_) | EventKind::Remove(_)
        )
    }

    #[instrument(skip(self))]
    async fn reload_and_sync(&self, paths: &[PathBuf]) -> Result<(), Error> {
        let themes_dir = ConfigPaths::themes_dir();
        let runtime_path = ConfigPaths::runtime_config();
        let runtime_tmp_path = runtime_path.with_extension("tmp");

        let is_env = |path: &PathBuf| secrets::is_env_file(path);
        let is_theme = |path: &PathBuf| path.starts_with(&themes_dir);
        let is_runtime = |path: &PathBuf| path == &runtime_path || path == &runtime_tmp_path;

        let has_env_changes = paths.iter().any(is_env);
        let has_theme_changes = paths.iter().any(is_theme);
        let has_runtime_changes = paths.iter().any(is_runtime);
        let has_main_config_changes = paths
            .iter()
            .any(|path| !is_env(path) && !is_theme(path) && !is_runtime(path));

        if has_env_changes && let Ok(config_dir) = ConfigPaths::config_dir() {
            secrets::reload_env_files(&config_dir);
            let _ = self.secrets_tx.send(());
        }

        if has_theme_changes {
            load_themes(self.config_service.config(), &themes_dir);
        }

        if has_main_config_changes {
            self.reload_main_config().await?;
        } else if has_runtime_changes {
            self.reload_runtime_only().await?;
        }

        Ok(())
    }

    async fn reload_main_config(&self) -> Result<(), Error> {
        let config = self.config_service.config();

        let config_path = discover_main_config();
        let toml_value =
            tokio::task::spawn_blocking(move || Config::load_toml_with_imports(&config_path))
                .await
                .map_err(|source| Error::TaskJoin { source })??;

        config.reset_config_layer();
        config.apply_config_layer(&toml_value, "");

        config.reset_runtime_layer();
        let runtime_path = ConfigPaths::runtime_config();
        let runtime_result =
            tokio::task::spawn_blocking(move || ConfigService::load_toml_file(&runtime_path))
                .await
                .map_err(|source| Error::TaskJoin { source })?;

        if let Ok(runtime_toml) = runtime_result {
            let _ = config.apply_runtime_layer(&runtime_toml, "");
        }

        config.commit_config_reload();

        Ok(())
    }

    async fn reload_runtime_only(&self) -> Result<(), Error> {
        let config = self.config_service.config();
        let runtime_path = ConfigPaths::runtime_config();

        let runtime_result =
            tokio::task::spawn_blocking(move || ConfigService::load_toml_file(&runtime_path))
                .await
                .map_err(|source| Error::TaskJoin { source })?;

        let Ok(runtime_toml) = runtime_result else {
            return Ok(());
        };

        config.reset_runtime_layer();
        let _ = config.apply_runtime_layer(&runtime_toml, "");
        config.commit_config_reload();

        Ok(())
    }
}

const DEBOUNCE_DURATION: Duration = Duration::from_millis(100);

/// Ceiling on how long a sustained event stream may defer a flush.
///
/// Every event pushes the debounce deadline out, so a directory under
/// continuous churn would otherwise never reload and the pending set would grow
/// without bound.
const MAX_DEBOUNCE_DELAY: Duration = Duration::from_secs(2);

async fn run_debounced_event_loop(watcher: FileWatcher, mut rx: mpsc::UnboundedReceiver<Event>) {
    use tokio::time::{Instant, sleep_until};

    let mut pending_paths: HashSet<PathBuf> = HashSet::new();
    let mut deadline: Option<Instant> = None;
    let mut flush_by: Option<Instant> = None;

    loop {
        // No `biased` here on purpose: an always-ready `rx` would starve the
        // timer branch entirely and the debounce would never fire.
        let maybe_event = match deadline {
            Some(d) => tokio::select! {
                event = rx.recv() => event,
                () = sleep_until(d) => None,
            },
            None => rx.recv().await,
        };

        match maybe_event {
            Some(event) if FileWatcher::should_reload(&event) => {
                pending_paths.extend(event.paths);
                deadline = Some(next_deadline(Instant::now(), &mut flush_by));
            }
            Some(_) => {}
            None if deadline.is_some() => {
                flush_pending(&watcher, &mut pending_paths).await;
                deadline = None;
                flush_by = None;
            }
            None => break,
        }
    }
}

/// Debounce deadline for an event seen at `now`, capped so a continuous event
/// stream still flushes within [`MAX_DEBOUNCE_DELAY`] of the first pending
/// event. `flush_by` holds that ceiling and is reset by the flush.
fn next_deadline(
    now: tokio::time::Instant,
    flush_by: &mut Option<tokio::time::Instant>,
) -> tokio::time::Instant {
    let ceiling = *flush_by.get_or_insert(now + MAX_DEBOUNCE_DELAY);
    (now + DEBOUNCE_DURATION).min(ceiling)
}

async fn flush_pending(watcher: &FileWatcher, pending_paths: &mut HashSet<PathBuf>) {
    let paths: Vec<PathBuf> = pending_paths.drain().collect();
    debug!(?paths, "Debounce complete, reloading config");

    if let Err(e) = watcher.reload_and_sync(&paths).await {
        error!("config reload failed:\n{e}");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn store_root_is_never_watched() {
        assert!(FileWatcher::is_immutable_store(Path::new("/nix/store")));
        assert!(!FileWatcher::is_immutable_store(Path::new(
            "/nix/store/blyx7c87qbilmj512y14jgmydx640dbv-source"
        )));
        assert!(!FileWatcher::is_immutable_store(Path::new(
            "/home/user/.config/wayle"
        )));
    }

    #[test]
    fn continuous_events_still_flush_within_the_ceiling() {
        let start = tokio::time::Instant::now();
        let mut flush_by = None;

        // An event every 10ms forever: each one pushes the 100ms debounce out,
        // so only the ceiling can end the batch.
        let reached_ceiling = (0..1_000_u64).any(|tick| {
            let now = start + Duration::from_millis(tick * 10);
            let deadline = next_deadline(now, &mut flush_by);
            assert!(deadline <= start + MAX_DEBOUNCE_DELAY);
            deadline <= now
        });

        assert!(
            reached_ceiling,
            "debounce never reached its ceiling under a continuous event stream"
        );
    }

    #[test]
    fn isolated_events_use_the_short_debounce() {
        let now = tokio::time::Instant::now();
        let mut flush_by = None;

        assert_eq!(next_deadline(now, &mut flush_by), now + DEBOUNCE_DURATION);
    }
}
