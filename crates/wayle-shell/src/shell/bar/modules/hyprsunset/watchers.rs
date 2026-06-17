use std::time::Duration;

use futures::StreamExt;
use relm4::ComponentSender;
use tokio::time::interval;
use tokio_stream::wrappers::IntervalStream;
use wayle_config::schemas::modules::HyprsunsetConfig;
use wayle_widgets::{watch, watch_async};

use super::{HyprsunsetModule, geoclue, helpers, messages::HyprsunsetCmd};

/// How often to refresh the GeoClue location once auto-schedule is running.
const LOCATION_REFRESH: Duration = Duration::from_secs(6 * 60 * 60);

pub(super) fn spawn_config_watchers(
    sender: &ComponentSender<HyprsunsetModule>,
    config: &HyprsunsetConfig,
) {
    let icon_off = config.icon_off.clone();
    let icon_on = config.icon_on.clone();
    let format = config.format.clone();

    watch!(
        sender,
        [icon_off.watch(), icon_on.watch(), format.watch()],
        |out| {
            let _ = out.send(HyprsunsetCmd::ConfigChanged);
        }
    );
}

pub(super) fn spawn_state_watcher(sender: &ComponentSender<HyprsunsetModule>) {
    let interval_stream = IntervalStream::new(interval(Duration::from_secs(1)));

    watch_async!(sender, [interval_stream], |out| async {
        let state = helpers::query_state().await;
        let _ = out.send(HyprsunsetCmd::StateChanged(state));
    });
}

/// Re-evaluate the solar auto-schedule periodically. A coarse interval is
/// enough — sunrise/sunset boundaries shift by minutes per day, and the eval
/// is cheap (pure math).
pub(super) fn spawn_schedule_watcher(sender: &ComponentSender<HyprsunsetModule>) {
    let interval_stream = IntervalStream::new(interval(Duration::from_secs(60)));

    watch!(sender, [interval_stream], |out| {
        let _ = out.send(HyprsunsetCmd::TickSchedule);
    });
}

/// Re-evaluate the schedule when any of its config inputs change.
pub(super) fn spawn_schedule_config_watcher(
    sender: &ComponentSender<HyprsunsetModule>,
    config: &HyprsunsetConfig,
) {
    let auto_schedule = config.auto_schedule.clone();
    let latitude = config.latitude.clone();
    let longitude = config.longitude.clone();

    watch!(
        sender,
        [auto_schedule.watch(), latitude.watch(), longitude.watch()],
        |out| {
            let _ = out.send(HyprsunsetCmd::TickSchedule);
        }
    );
}

/// Resolve the schedule location via GeoClue: once at startup (if auto-schedule
/// is on), again whenever it is toggled, and refreshed on a slow timer. Failures
/// emit nothing, so the schedule falls back to the configured lat/long.
pub(super) fn spawn_location_watcher(
    sender: &ComponentSender<HyprsunsetModule>,
    config: &HyprsunsetConfig,
) {
    let auto_schedule = config.auto_schedule.clone();

    sender.command(move |out, shutdown| async move {
        let shutdown_fut = shutdown.wait();
        tokio::pin!(shutdown_fut);

        let mut auto_changes = auto_schedule.watch();
        let mut refresh = IntervalStream::new(interval(LOCATION_REFRESH));

        loop {
            if auto_schedule.get()
                && let Some((lat, lng)) = geoclue::query_location().await
            {
                let _ = out.send(HyprsunsetCmd::LocationResolved(lat, lng));
            }

            tokio::select! {
                () = &mut shutdown_fut => break,
                _ = auto_changes.next() => {}
                _ = refresh.next() => {}
            }
        }
    });
}
