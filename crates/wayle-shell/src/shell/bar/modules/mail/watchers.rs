use std::{sync::Arc, time::Duration};

use notify::{Event, RecursiveMode, Watcher, event::EventKind};
use relm4::ComponentSender;
use tokio::sync::mpsc;
use tracing::error;
use wayle_config::{ConfigProperty, schemas::modules::MailConfig};
use wayle_widgets::watch;

use super::{MailModule, helpers, messages::MailCmd};

/// Debounce window to coalesce a maildir-sync burst into one re-query.
const DEBOUNCE: Duration = Duration::from_millis(500);

pub(super) fn spawn_config_watchers(sender: &ComponentSender<MailModule>, config: &MailConfig) {
    let format = config.format.clone();
    let icon_name = config.icon_name.clone();
    let hide_when_zero = config.hide_when_zero.clone();

    watch!(
        sender,
        [format.watch(), icon_name.watch(), hide_when_zero.watch()],
        |out| {
            let _ = out.send(MailCmd::ConfigChanged);
        }
    );

    let query = config.query.clone();
    watch!(sender, [query.watch()], |out| {
        let _ = out.send(MailCmd::QueryChanged);
    });
}

/// Event-driven unread watcher: emit the initial count, then re-query on every
/// maildir change (inotify on the notmuch database path, debounced).
pub(super) fn spawn_mail_watcher(
    sender: &ComponentSender<MailModule>,
    query: ConfigProperty<String>,
) {
    sender.command(move |out, shutdown| async move {
        let _ = out.send(MailCmd::CountChanged(
            helpers::query_count(&query.get()).await,
        ));

        let Some(maildir) = helpers::maildir_path().await else {
            // No notmuch DB to watch — leave the initial count in place.
            return;
        };

        let (tx, mut rx) = mpsc::unbounded_channel();
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
                error!(error = %err, "cannot create maildir watcher");
                return;
            }
        };

        if let Err(err) = watcher.watch(&maildir, RecursiveMode::Recursive) {
            error!(error = %err, path = %maildir.display(), "cannot watch maildir");
            return;
        }

        let _watcher = Arc::new(watcher);
        let shutdown_fut = shutdown.wait();
        tokio::pin!(shutdown_fut);

        loop {
            tokio::select! {
                () = &mut shutdown_fut => break,
                received = rx.recv() => {
                    if received.is_none() {
                        break;
                    }
                    // Coalesce an mbsync burst: settle, then drain queued events.
                    tokio::time::sleep(DEBOUNCE).await;
                    while rx.try_recv().is_ok() {}
                    let count = helpers::query_count(&query.get()).await;
                    let _ = out.send(MailCmd::CountChanged(count));
                }
            }
        }
    });
}
