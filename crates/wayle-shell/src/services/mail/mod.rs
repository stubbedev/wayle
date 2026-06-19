//! Mail unread service: runs notmuch queries (per-account or a single query),
//! re-querying on maildir changes, and exposes reactive counts that both the
//! bar module and the dropdown watch.

use std::{
    collections::{BTreeSet, HashMap},
    path::PathBuf,
    process::Stdio,
    sync::Arc,
    time::Duration,
};

use futures::StreamExt;
use notify::{Event, RecursiveMode, Watcher, event::EventKind};
use tokio::{process::Command, sync::mpsc};
use tracing::{info, warn};
use wayle_config::{
    ConfigService,
    schemas::modules::{MailAccount, MailConfig},
};
use wayle_core::Property;
use wayle_icons::{IconManager, IconRegistry, IconSource, sources::SimpleIcons};

/// Debounce window to coalesce a maildir-sync burst into one re-query.
const DEBOUNCE: Duration = Duration::from_millis(500);

/// Cap on how many per-message notifications a single arrival burst fires, so a
/// large sync (or a freshly added account) can't flood the notification daemon.
const NOTIFY_MAX: usize = 5;

/// One newly-arrived message, rendered into a desktop notification.
#[derive(Clone, Debug, PartialEq, Eq)]
struct NewMail {
    /// Icon name (provider/account icon) for the notification.
    icon: String,
    /// Sender display name (or address).
    sender: String,
    /// Message subject.
    subject: String,
    /// Total unread for the originating query, after the arrival.
    count: u32,
    /// How many messages arrived in this burst for that query.
    new: u32,
}

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

        // First pass seeds the baseline counts without notifying, so existing
        // unread mail at startup doesn't fire spurious "new mail" notifications.
        self.recompute(&config, false).await;

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
            self.recompute(&config, true).await;
        }
    }

    /// Re-run the queries and publish counts. When `notify` (and the `notify`
    /// config) is set, fire a desktop notification per newly-arrived message for
    /// every query whose unread count rose.
    async fn recompute(&self, config: &Arc<ConfigService>, notify: bool) {
        let mail = &config.config().modules.mail;
        let accounts = mail.accounts.get();
        let notify = notify && mail.notify.get();

        if accounts.is_empty() {
            let query = mail.query.get();
            let previous = self.total.get();
            let total = query_count(&query).await;
            self.accounts.set(Vec::new());
            self.total.set(total);

            if notify && total > previous {
                let icon = mail.icon_name.get();
                let batch = build_batch(&query, &icon, "New mail", previous, total).await;
                fire_notifications(mail, &batch);
            }
            return;
        }

        let prev_counts: HashMap<String, u32> = self
            .accounts
            .get()
            .into_iter()
            .map(|a| (a.name, a.count))
            .collect();

        let mut resolved = Vec::with_capacity(accounts.len());
        let mut total = 0u32;
        let mut batch = Vec::new();
        for account in &accounts {
            let count = query_count(&account.query).await;
            total = total.saturating_add(count);
            let icon = account.resolved_icon();

            if notify {
                let previous = prev_counts.get(&account.name).copied().unwrap_or(0);
                if count > previous {
                    batch.extend(
                        build_batch(&account.query, &icon, &account.name, previous, count).await,
                    );
                }
            }

            resolved.push(AccountUnread {
                name: account.name.clone(),
                icon,
                count,
            });
        }

        self.accounts.set(resolved);
        self.total.set(total);

        if !batch.is_empty() {
            fire_notifications(mail, &batch);
        }
    }
}

/// Build the notification batch for one query that gained unread messages:
/// fetch up to [`NOTIFY_MAX`] of the newest matches (sender + subject). Falls
/// back to a single count-only entry when message details can't be read.
async fn build_batch(
    query: &str,
    icon: &str,
    fallback_sender: &str,
    previous: u32,
    total: u32,
) -> Vec<NewMail> {
    let new = total.saturating_sub(previous);
    let limit = (new as usize).min(NOTIFY_MAX);
    let messages = query_new_messages(query, limit).await;

    if messages.is_empty() {
        return vec![NewMail {
            icon: icon.to_owned(),
            sender: fallback_sender.to_owned(),
            subject: format!("{new} new ({total} unread)"),
            count: total,
            new,
        }];
    }

    messages
        .into_iter()
        .map(|(sender, subject)| NewMail {
            icon: icon.to_owned(),
            sender,
            subject,
            count: total,
            new,
        })
        .collect()
}

/// Run `notmuch search` for the newest `limit` matches, returning
/// `(sender, subject)` per thread. Returns empty on any failure.
async fn query_new_messages(query: &str, limit: usize) -> Vec<(String, String)> {
    if limit == 0 {
        return Vec::new();
    }

    let output = Command::new("notmuch")
        .args([
            "search",
            "--format=json",
            "--sort=newest-first",
            &format!("--limit={limit}"),
        ])
        .arg(query)
        .output()
        .await;

    let Ok(output) = output else {
        return Vec::new();
    };
    if !output.status.success() {
        return Vec::new();
    }
    let Ok(value) = serde_json::from_slice::<serde_json::Value>(&output.stdout) else {
        return Vec::new();
    };
    let Some(threads) = value.as_array() else {
        return Vec::new();
    };

    threads
        .iter()
        .map(|thread| {
            // `authors` is "matched | non-matched"; keep the first matched author.
            let authors = thread.get("authors").and_then(|v| v.as_str()).unwrap_or("");
            let sender = authors
                .split('|')
                .next()
                .unwrap_or(authors)
                .split(',')
                .next()
                .unwrap_or(authors)
                .trim()
                .to_owned();
            let subject = thread
                .get("subject")
                .and_then(|v| v.as_str())
                .unwrap_or("")
                .trim()
                .to_owned();
            (sender, subject)
        })
        .filter(|(sender, subject)| !(sender.is_empty() && subject.is_empty()))
        .collect()
}

/// Render a notification template, substituting `{{ sender }}`, `{{ subject }}`,
/// `{{ count }}` (total unread) and `{{ new }}` (newly arrived).
fn render_notification(format: &str, mail: &NewMail) -> String {
    format
        .replace("{{ sender }}", &mail.sender)
        .replace("{{ subject }}", &mail.subject)
        .replace("{{ count }}", &mail.count.to_string())
        .replace("{{ new }}", &mail.new.to_string())
}

/// Resolve an icon name to an absolute SVG path for `notify-send` (notification
/// daemons don't search wayle's private icon dir by name), falling back to the
/// bare name for system-themed icons.
fn notify_icon_arg(icon: &str) -> String {
    IconRegistry::default_path()
        .ok()
        .map(|base| {
            base.join("hicolor")
                .join("scalable")
                .join("actions")
                .join(format!("{icon}.svg"))
        })
        .filter(|path| path.exists())
        .map(|path| path.to_string_lossy().into_owned())
        .unwrap_or_else(|| icon.to_owned())
}

/// Fire one fire-and-forget `notify-send` per message in the batch.
fn fire_notifications(mail: &MailConfig, batch: &[NewMail]) {
    let summary_fmt = mail.notify_summary.get();
    let body_fmt = mail.notify_body.get();

    for item in batch {
        let summary = render_notification(&summary_fmt, item);
        let body = render_notification(&body_fmt, item);
        let icon = notify_icon_arg(&item.icon);

        let mut command = Command::new("notify-send");
        command
            .arg("--app-name=Wayle")
            .arg(format!("--icon={icon}"))
            .arg(summary)
            .arg(body)
            .stdout(Stdio::null())
            .stderr(Stdio::null());

        match command.spawn() {
            Ok(child) => {
                tokio::spawn(async move {
                    let _ = child.wait_with_output().await;
                });
            }
            Err(err) => warn!(error = %err, "cannot spawn notify-send for mail"),
        }
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
