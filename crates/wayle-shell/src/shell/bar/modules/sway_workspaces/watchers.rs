//! Background watchers: sway workspace/window state + config-property changes.

use std::sync::Arc;

use futures::{StreamExt, stream::select};
use relm4::ComponentSender;
use tokio::sync::mpsc;
use wayle_config::{
    ConfigProperty, SubscribeChanges,
    schemas::{
        modules::SwayWorkspacesConfig,
        styling::{ScaleFactor, ThemeProvider},
    },
};
use wayle_sway::SwayService;
use wayle_widgets::prelude::BarSettings;

use super::{SwayWorkspaces, messages::SwayWorkspacesCmd};

pub(super) fn spawn_watchers(
    sender: &ComponentSender<SwayWorkspaces>,
    config: &SwayWorkspacesConfig,
    sway: Arc<SwayService>,
    theme_provider: ConfigProperty<ThemeProvider>,
    bar_scale: ConfigProperty<ScaleFactor>,
    settings: &BarSettings,
) {
    spawn_sway_events(sender, sway);
    spawn_config_watcher(sender, config, theme_provider, bar_scale, settings);
}

fn spawn_sway_events(sender: &ComponentSender<SwayWorkspaces>, sway: Arc<SwayService>) {
    sender.command(move |out, shutdown| watch_state_changes(sway.clone(), out, shutdown));
}

async fn watch_state_changes(
    sway: Arc<SwayService>,
    out: relm4::Sender<SwayWorkspacesCmd>,
    shutdown: relm4::ShutdownReceiver,
) {
    // sway exposes no incremental event stream; instead the service rebuilds
    // the workspaces/windows snapshots on each IPC event. Watching both
    // Property streams turns any change into a single rebuild signal.
    let workspaces = sway.workspaces.watch().map(|_| ());
    let windows = sway.windows.watch().map(|_| ());
    let mut changes = select(workspaces, windows);

    let shutdown_fut = shutdown.wait();
    tokio::pin!(shutdown_fut);

    loop {
        tokio::select! {
            () = &mut shutdown_fut => return,
            next = changes.next() => {
                if next.is_none() {
                    return;
                }
                let _ = out.send(SwayWorkspacesCmd::WorkspacesChanged);
            }
        }
    }
}

fn spawn_config_watcher(
    sender: &ComponentSender<SwayWorkspaces>,
    config: &SwayWorkspacesConfig,
    theme_provider: ConfigProperty<ThemeProvider>,
    bar_scale: ConfigProperty<ScaleFactor>,
    settings: &BarSettings,
) {
    let (tx, rx) = mpsc::unbounded_channel();

    config.subscribe_changes(tx.clone());
    theme_provider.subscribe_changes(tx.clone());
    bar_scale.subscribe_changes(tx.clone());
    settings.border_width.subscribe_changes(tx.clone());
    settings.border_location.subscribe_changes(tx.clone());
    settings.is_vertical.subscribe_changes(tx);

    sender.command(move |out, shutdown| watch_config_changes(rx, out, shutdown));
}

async fn watch_config_changes(
    mut rx: mpsc::UnboundedReceiver<()>,
    out: relm4::Sender<SwayWorkspacesCmd>,
    shutdown: relm4::ShutdownReceiver,
) {
    let shutdown_fut = shutdown.wait();
    tokio::pin!(shutdown_fut);

    loop {
        tokio::select! {
            () = &mut shutdown_fut => return,
            received = rx.recv() => {
                if received.is_none() {
                    return;
                }

                while rx.try_recv().is_ok() {}

                let _ = out.send(SwayWorkspacesCmd::ConfigChanged);
            }
        }
    }
}
