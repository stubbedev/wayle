use std::sync::Arc;

use relm4::ComponentSender;
use wayle_config::schemas::modules::TreemanConfig;
use wayle_treeman::{Bucket, TreemanService, TreemanStatus};
use wayle_widgets::watch;

use super::{TreemanModule, helpers, messages::TreemanCmd};

pub fn spawn_watchers(
    sender: &ComponentSender<TreemanModule>,
    config: &TreemanConfig,
    treeman: &Arc<TreemanService>,
) {
    let status_prop = treeman.status.clone();
    let format_config = config.format.clone();
    let hide_if_empty = config.hide_if_empty.clone();
    let icon_name = config.icon_name.clone();
    let icon_preparing = config.icon_preparing.clone();
    let icon_tearing_down = config.icon_tearing_down.clone();
    let icon_failed = config.icon_failed.clone();

    watch!(
        sender,
        [
            status_prop.watch(),
            format_config.watch(),
            hide_if_empty.watch(),
            icon_name.watch(),
            icon_preparing.watch(),
            icon_tearing_down.watch(),
            icon_failed.watch()
        ],
        |out| {
            let status = status_prop.get();
            let total = status.as_ref().map_or(0, |s| s.total);
            let (label, severity) = match &status {
                Some(status) => (
                    helpers::format_label(&format_config.get(), status),
                    helpers::severity_class(status),
                ),
                None => (String::from("--"), None),
            };
            // Icon follows the worst bucket, so a prepare or a teardown in
            // flight is legible on the bar without opening the dropdown.
            let icon = match status.as_ref().map(TreemanStatus::worst_bucket) {
                Some(Bucket::Failed) => icon_failed.get(),
                Some(Bucket::Down) => icon_tearing_down.get(),
                Some(Bucket::Up) => icon_preparing.get(),
                Some(Bucket::Stable) | None => icon_name.get(),
            };
            let visible = !(hide_if_empty.get() && total == 0);
            let _ = out.send(TreemanCmd::Update {
                label,
                icon,
                severity,
                visible,
            });
        }
    );
}
