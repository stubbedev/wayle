use std::sync::Arc;

use relm4::ComponentSender;
use wayle_config::ConfigService;
use wayle_treeman::TreemanService;
use wayle_widgets::watch;

use super::{TreemanDropdown, messages::TreemanDropdownCmd};

pub fn spawn(
    sender: &ComponentSender<TreemanDropdown>,
    treeman: &Arc<TreemanService>,
    config: &Arc<ConfigService>,
) {
    spawn_scale_watcher(sender, config);
    spawn_status_watcher(sender, treeman);
}

fn spawn_scale_watcher(sender: &ComponentSender<TreemanDropdown>, config: &Arc<ConfigService>) {
    let scale = config.config().styling.scale.clone();

    watch!(sender, [scale.watch()], |out| {
        let _ = out.send(TreemanDropdownCmd::ScaleChanged(scale.get().value()));
    });
}

fn spawn_status_watcher(sender: &ComponentSender<TreemanDropdown>, treeman: &Arc<TreemanService>) {
    let status = treeman.status.clone();

    watch!(sender, [status.watch()], |out| {
        let _ = out.send(TreemanDropdownCmd::StatusChanged(status.get()));
    });
}
