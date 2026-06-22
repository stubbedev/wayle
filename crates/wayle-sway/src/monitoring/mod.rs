//! Wires the event-stream socket into the reactive [`Property`] fields.
//!
//! [`ipc::subscribe_events`](crate::ipc::subscribe_events) owns the event
//! socket and pushes a [`SwayEvent`] into the inbound broadcast channel on
//! each `workspace`/`window` event. The spawned event loop drains a
//! pre-subscribed receiver and re-queries sway via [`refresh`], rebuilding the
//! relevant [`Property`] fields. Unlike niri, sway does not stream incremental
//! state, so each event triggers a fresh `GET_WORKSPACES` / `GET_TREE`.

mod refresh;

use std::{collections::HashMap, sync::Arc};

use derive_more::Debug;
use tokio::sync::broadcast::{self, error::RecvError};
use tokio_util::sync::CancellationToken;
use tracing::{error, instrument};
use wayle_core::Property;
use wayle_traits::ServiceMonitoring;

use crate::{
    core::{Window, Workspace},
    error::Error,
    ipc::{SwayCommandClient, SwayEvent, subscribe_events},
    service::SwayService,
};

/// Clones of the service's [`Property`] fields, refreshed by the event loop.
#[derive(Debug, Clone)]
pub(crate) struct MonitoringHandles {
    pub(crate) workspaces: Property<HashMap<u64, Arc<Workspace>>>,
    pub(crate) windows: Property<HashMap<u64, Arc<Window>>>,
    pub(crate) keyboard_layout: Property<Option<String>>,
}

impl ServiceMonitoring for SwayService {
    type Error = Error;

    #[instrument(skip(self), err)]
    async fn start_monitoring(&self) -> Result<(), Error> {
        let inbound_event_rx = self.inbound_event_tx.subscribe();

        subscribe_events(
            self.inbound_event_tx.clone(),
            self.cancellation_token.clone(),
        )
        .await?;

        let handles = MonitoringHandles {
            workspaces: self.workspaces.clone(),
            windows: self.windows.clone(),
            keyboard_layout: self.keyboard_layout.clone(),
        };

        // Populate the initial snapshot before the event loop starts; events
        // that arrive meanwhile are buffered in the broadcast receiver.
        refresh::refresh_windows(&self.command_client, &handles).await;
        refresh::refresh_workspaces(&self.command_client, &handles).await;
        refresh::refresh_keyboard_layout(&self.command_client, &handles).await;

        tokio::spawn(event_loop(
            inbound_event_rx,
            self.command_client.clone(),
            handles,
            self.cancellation_token.clone(),
        ));

        Ok(())
    }
}

async fn event_loop(
    mut inbound_event_rx: broadcast::Receiver<SwayEvent>,
    command_client: Arc<SwayCommandClient>,
    handles: MonitoringHandles,
    cancellation_token: CancellationToken,
) {
    loop {
        tokio::select! {
            _ = cancellation_token.cancelled() => return,
            received = inbound_event_rx.recv() => match received {
                Ok(SwayEvent::WorkspaceChanged) => {
                    refresh::refresh_workspaces(&command_client, &handles).await;
                }
                Ok(SwayEvent::WindowChanged) => {
                    // A window opening/closing/moving changes which workspaces
                    // are occupied, so refresh both.
                    refresh::refresh_windows(&command_client, &handles).await;
                    refresh::refresh_workspaces(&command_client, &handles).await;
                }
                Ok(SwayEvent::InputChanged) => {
                    refresh::refresh_keyboard_layout(&command_client, &handles).await;
                }
                Err(RecvError::Lagged(dropped)) => {
                    error!(
                        dropped,
                        "event loop lagged behind sway event stream; Properties may be briefly stale",
                    );
                }
                Err(RecvError::Closed) => return,
            }
        }
    }
}
