use relm4::prelude::*;

use super::{RecorderDropdown, messages::RecorderDropdownInit};
use crate::shell::{
    bar::dropdowns::{DropdownFactory, DropdownInstance, require_service},
    services::ShellServices,
};

pub struct Factory;

impl DropdownFactory for Factory {
    fn create(services: &ShellServices) -> Option<DropdownInstance> {
        let recorder = require_service("recorder", "recorder", services.recorder.clone())?;
        let init = RecorderDropdownInit {
            config: services.config.clone(),
            state: recorder.state(),
            audio: services.audio.clone(),
        };
        let controller = RecorderDropdown::builder().launch(init).detach();

        let popover = controller.widget().clone();
        Some(DropdownInstance::new(popover, Box::new(controller)))
    }
}
