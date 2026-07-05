use relm4::prelude::*;

use super::{TreemanDropdown, messages::TreemanDropdownInit};
use crate::shell::{
    bar::dropdowns::{DropdownFactory, DropdownInstance},
    services::ShellServices,
};

pub struct Factory;

impl DropdownFactory for Factory {
    fn create(services: &ShellServices) -> Option<DropdownInstance> {
        let init = TreemanDropdownInit {
            treeman: services.treeman.clone(),
            config: services.config.clone(),
            toast_bus: services.toast_bus.clone(),
        };
        let controller = TreemanDropdown::builder().launch(init).detach();

        let popover = controller.widget().clone();
        Some(DropdownInstance::new(popover, Box::new(controller)))
    }
}
