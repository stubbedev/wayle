//! Factory entry: gate on the sway compositor + service availability,
//! then launch the [`SwayWorkspaces`] component.

use std::rc::Rc;

use relm4::prelude::*;
use wayle_widgets::prelude::BarSettings;

use super::{SwayWorkspaces, SwayWorkspacesInit};
use crate::shell::{
    bar::{
        dropdowns::DropdownRegistry,
        modules::registry::{ModuleFactory, ModuleInstance, dynamic_controller, require_sway},
    },
    services::ShellServices,
};

/// Module factory that launches [`SwayWorkspaces`] when sway is the active
/// compositor and the [`SwayService`] is available.
///
/// [`SwayWorkspaces`]: super::SwayWorkspaces
/// [`SwayService`]: wayle_sway::SwayService
pub(crate) struct Factory;

impl ModuleFactory for Factory {
    fn create(
        settings: &BarSettings,
        services: &ShellServices,
        dropdowns: &Rc<DropdownRegistry>,
        class: Option<String>,
    ) -> Option<ModuleInstance> {
        if !require_sway("sway-workspaces") {
            return None;
        }
        let sway = services.sway.clone()?;

        let init = SwayWorkspacesInit {
            settings: settings.clone(),
            sway,
            config: services.config.clone(),
            dropdowns: dropdowns.clone(),
        };
        let controller = dynamic_controller(SwayWorkspaces::builder().launch(init).detach());
        Some(ModuleInstance { controller, class })
    }
}
