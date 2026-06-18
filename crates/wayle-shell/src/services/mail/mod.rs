//! Mail unread service: runs notmuch queries (per-account or a single query),
//! re-querying on maildir changes, and exposes reactive counts that both the
//! bar module and the dropdown watch.

use std::{collections::BTreeSet, path::PathBuf, sync::Arc, time::Duration};

use futures::StreamExt;
use notify::{Event, RecursiveMode, Watcher, event::EventKind};
use tokio::{process::Command, sync::mpsc};
use tracing::{info, warn};
use wayle_config::{ConfigService, schemas::modules::MailAccount};
use wayle_core::Property;
use wayle_icons::{IconManager, IconSource, sources::SimpleIcons};

/// Debounce window to coalesce a maildir-sync burst into one re-query.
const DEBOUNCE: Duration = Duration::from_millis(500);

/// One account's resolved icon and unread count.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AccountUnread {
    pub name: String,
    pub icon: String,
    pub count: u32,
}

/// Reactive mail unread state.
pub struct MailService {
    /// Per-account unread, in config order. Empty when no accounts configured.
    pub accounts: Property<Vec<AccountUnread>>,
    /// Total unread: sum across accounts, or the single `query` when none.
    pub total: Property<u32>,
}

impl MailService {
    /// Builds the service and spawns its background query/watch task.
    #[must_use]
    pub fn new(config: Arc<ConfigService>) -> Arc<Self> {
        let service = Arc::new(Self {
            accounts: Property::new(Vec::new()),
            total: Property::new(0),
        });

        let task_service = Arc::clone(&service);
        tokio::spawn(async move { task_service.run(config).await });

        service
    }

    async fn run(self: Arc<Self>, config: Arc<ConfigService>) {
        let mail = config.config().modules.mail.clone();

        // Best-effort: install brand icons for the configured providers.
        install_provider_icons(&mail.accounts.get());

        self.recompute(&config).await;

        let mut accounts_w = mail.accounts.watch();
        let mut query_w = mail.query.watch();

        // Maildir watcher → channel. Kept alive for the task's lifetime; when no
        // notmuch DB exists, `rx` simply never yields and we rely on config
        // watches alone.
        let (tx, mut rx) = mpsc::unbounded_channel();
        let _maildir_watcher = spawn_maildir_watcher(tx).await;

        loop {
            tokio::select! {
                _ = accounts_w.next() => {
                    install_provider_icons(&mail.accounts.get());
                }
                _ = query_w.next() => {}
                received = rx.recv() => {
                    if received.is_none() {
                        // Watcher gone; stop selecting on a dead channel.
                        std::future::pending::<()>().await;
                    }
                }
            }

            // Settle the burst, then drain queued events before re-querying.
            tokio::time::sleep(DEBOUNCE).await;
            while rx.try_recv().is_ok() {}
            self.recompute(&config).await;
        }
    }

    async fn recompute(&self, config: &Arc<ConfigService>) {
        let mail = &config.config().modules.mail;
        let accounts = mail.accounts.get();

        if accounts.is_empty() {
            let total = query_count(&mail.query.get()).await;
            self.accounts.set(Vec::new());
            self.total.set(total);
            return;
        }

        let mut resolved = Vec::with_capacity(accounts.len());
        let mut total = 0u32;
        for account in &accounts {
            let count = query_count(&account.query).await;
            total = total.saturating_add(count);
            resolved.push(AccountUnread {
                name: account.name.clone(),
                icon: account.resolved_icon(),
                count,
            });
        }

        self.accounts.set(resolved);
        self.total.set(total);
    }
}

/// Run `notmuch count <query>`, returning 0 on any failure.
async fn query_count(query: &str) -> u32 {
    let output = Command::new("notmuch")
        .arg("count")
        .arg(query)
        .output()
        .await;

    let Ok(output) = output else {
        return 0;
    };
    if !output.status.success() {
        return 0;
    }

    String::from_utf8_lossy(&output.stdout)
        .trim()
        .parse()
        .unwrap_or(0)
}

/// Resolve the notmuch maildir (`notmuch config get database.path`).
async fn maildir_path() -> Option<PathBuf> {
    let output = Command::new("notmuch")
        .args(["config", "get", "database.path"])
        .output()
        .await
        .ok()?;

    if !output.status.success() {
        return None;
    }

    let path = String::from_utf8_lossy(&output.stdout).trim().to_string();
    (!path.is_empty()).then(|| PathBuf::from(path))
}

/// Watch the notmuch maildir, forwarding create/modify/remove events. Returns
/// the watcher handle (must be kept alive) or `None` when there's no DB.
async fn spawn_maildir_watcher(tx: mpsc::UnboundedSender<()>) -> Option<impl Watcher> {
    let maildir = maildir_path().await?;

    let mut watcher = match notify::recommended_watcher(move |result: Result<Event, _>| {
        if let Ok(event) = result
            && matches!(
                event.kind,
                EventKind::Create(_) | EventKind::Modify(_) | EventKind::Remove(_)
            )
        {
            let _ = tx.send(());
        }
    }) {
        Ok(watcher) => watcher,
        Err(err) => {
            warn!(error = %err, "cannot create maildir watcher");
            return None;
        }
    };

    if let Err(err) = watcher.watch(&maildir, RecursiveMode::Recursive) {
        warn!(error = %err, path = %maildir.display(), "cannot watch maildir");
        return None;
    }

    Some(watcher)
}

/// Best-effort install of the Simple Icons brand icons for the configured
/// providers, skipping any already present. Fire-and-forget; failures (offline,
/// unknown slug) just leave the account on the generic fallback icon.
fn install_provider_icons(accounts: &[MailAccount]) {
    let Ok(manager) = IconManager::new() else {
        return;
    };

    let mut slugs: BTreeSet<&'static str> = BTreeSet::new();
    for account in accounts {
        // Skip when the user overrode the icon — we only auto-install defaults.
        if account.icon.as_deref().is_some_and(|s| !s.is_empty()) {
            continue;
        }
        if let Some(slug) = account.provider.simple_icons_slug()
            && !manager.is_installed(&SimpleIcons.icon_name(slug))
        {
            slugs.insert(slug);
        }
    }

    if slugs.is_empty() {
        return;
    }

    let slugs: Vec<&'static str> = slugs.into_iter().collect();
    tokio::spawn(async move {
        match manager.install(&SimpleIcons, &slugs).await {
            Ok(result) => {
                if !result.installed.is_empty() {
                    info!(icons = ?result.installed, "installed mail provider icons");
                }
                for failure in result.failed {
                    warn!(slug = %failure.slug, error = %failure.error, "mail provider icon install failed");
                }
            }
            Err(err) => warn!(error = %err, "mail provider icon install failed"),
        }
    });
}
