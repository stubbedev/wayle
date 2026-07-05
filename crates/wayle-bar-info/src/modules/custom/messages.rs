use std::{rc::Rc, sync::Arc};

use wayle_config::{ConfigService, schemas::modules::CustomModuleDefinition};
use wayle_widgets::prelude::BarSettings;

use crate::{services::WidgetBus, shell::bar::dropdowns::DropdownRegistry};

pub struct CustomInit {
    pub settings: BarSettings,
    pub config: Arc<ConfigService>,
    pub definition: CustomModuleDefinition,
    pub dropdowns: Rc<DropdownRegistry>,
    pub widget_bus: WidgetBus,
}

#[derive(Debug)]
pub enum CustomMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub enum CustomCmd {
    PollTrigger,
    ScrollDebounceExpired,
    CommandCancelled,
    CommandOutput(String),
    WatchOutput(String),
    ExternalOutput(String),
    DefinitionChanged(Box<CustomModuleDefinition>),
    DefinitionRemoved,
}
