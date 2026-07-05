use std::rc::Rc;

use relm4::prelude::*;
use tracing::warn;
use wayle_widgets::prelude::BarSettings;

use super::compositor::Compositor;
use crate::{bar::dropdown_registry::DropdownRegistry, shell_services::ShellServices};

pub struct ModuleInstance {
    pub controller: Box<dyn ModuleController>,
    pub class: Option<String>,
}

pub trait ModuleFactory {
    fn create(
        settings: &BarSettings,
        services: &ShellServices,
        dropdowns: &Rc<DropdownRegistry>,
        class: Option<String>,
    ) -> Option<ModuleInstance>;
}

pub trait ModuleController {
    fn widget(&self) -> &gtk::Box;
}

struct ControllerHandle<C>
where
    C: Component<Root = gtk::Box>,
{
    controller: Controller<C>,
}

impl<C> ModuleController for ControllerHandle<C>
where
    C: Component<Root = gtk::Box> + 'static,
{
    fn widget(&self) -> &gtk::Box {
        self.controller.widget()
    }
}

pub fn dynamic_controller<C>(controller: Controller<C>) -> Box<dyn ModuleController>
where
    C: Component<Root = gtk::Box> + 'static,
{
    Box::new(ControllerHandle { controller })
}

pub fn require_service<T>(
    module: &'static str,
    service: &'static str,
    value: Option<T>,
) -> Option<T> {
    match value {
        Some(v) => Some(v),
        None => {
            warn!(
                module,
                service,
                "module configured in bar layout but `{service}` service is unavailable on this system - \
                 module will not appear; remove `{module}` from your layout or enable the service"
            );
            None
        }
    }
}

pub fn require_hyprland(module: &'static str) -> bool {
    match Compositor::detect() {
        Compositor::Hyprland => true,
        other => {
            warn!(module, compositor = ?other, "module requires hyprland, skipping");
            false
        }
    }
}

pub fn require_niri(module: &'static str) -> bool {
    match Compositor::detect() {
        Compositor::Niri => true,
        other => {
            warn!(module, compositor = ?other, "module requires niri, skipping");
            false
        }
    }
}

pub fn require_mango(module: &'static str) -> bool {
    match Compositor::detect() {
        Compositor::Mango => true,
        other => {
            warn!(module, compositor = ?other, "module requires mango, skipping");
            false
        }
    }
}

pub fn require_sway(module: &'static str) -> bool {
    match Compositor::detect() {
        Compositor::Sway => true,
        other => {
            warn!(module, compositor = ?other, "module requires sway, skipping");
            false
        }
    }
}
