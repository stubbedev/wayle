use relm4::{gtk::prelude::*, prelude::*};
use wayle_config::ConfigProperty;

use super::{ActionChoice, ActionControl, ActionInit, ActionValue};
use crate::{pages::spec::SettingRowInit, property_handle::PropertyHandle, row::RowBehavior};

/// Row with a searchable action dropdown bound to an action property. Offers
/// `choices` plus "None" and "Custom command…" (a raw command string).
pub(crate) fn action<T: ActionValue>(
    property: &ConfigProperty<T>,
    choices: Vec<ActionChoice<T>>,
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
        handle: PropertyHandle::new(property, |value: &T| value.to_command()),
        control: widget.upcast(),
        keepalive: Box::new(controller),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
