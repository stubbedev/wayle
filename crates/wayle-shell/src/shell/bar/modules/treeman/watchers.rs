use std::sync::Arc;

use relm4::ComponentSender;
use wayle_config::schemas::modules::TreemanConfig;
use wayle_treeman::TreemanService;
use wayle_widgets::watch;

use super::{TreemanModule, helpers, messages::TreemanCmd};

pub(super) fn spawn_watchers(
    sender: &ComponentSender<TreemanModule>,
    config: &TreemanConfig,
    treeman: &Arc<TreemanService>,
) {
    let status_prop = treeman.status.clone();
    let format_config = config.format.clone();
    let hide_if_empty = config.hide_if_empty.clone();
    let icon_name = config.icon_name.clone();
    let icon_failed = config.icon_failed.clone();

    watch!(
        sender,
        [
            status_prop.watch(),
            format_config.watch(),
            hide_if_empty.watch(),
            icon_name.watch(),
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
            let icon = if status.as_ref().is_some_and(|s| s.failed > 0) {
                icon_failed.get()
            } else {
                icon_name.get()
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
