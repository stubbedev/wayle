use relm4::{gtk::prelude::*, prelude::*};
use wayle_config::{ClickAction, ConfigProperty};

use super::{ActionChoice, ActionControl, ActionInit};
use crate::{pages::spec::SettingRowInit, property_handle::PropertyHandle, row::RowBehavior};

/// Row with a searchable action dropdown bound to a `ClickAction` property.
/// Offers `choices` plus "None" and "Custom command…" (a raw shell command).
pub(crate) fn action(
    property: &ConfigProperty<ClickAction>,
    choices: Vec<ActionChoice>,
) -> SettingRowInit {
    let controller = ActionControl::builder()
        .launch(ActionInit {
            property: property.clone(),
            choices,
        })
        .detach();

    let widget = controller.widget().clone();

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value: &ClickAction| match value {
            ClickAction::Shell(cmd) => cmd.clone(),
            ClickAction::Dropdown(name) => format!("dropdown:{name}"),
            ClickAction::None => String::new(),
        }),
        control: widget.upcast(),
        keepalive: Box::new(controller),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
