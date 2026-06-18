use relm4::{gtk::prelude::*, prelude::*};
use wayle_config::ConfigProperty;

use crate::{
    editors::device_select::{DeviceSelectControl, DeviceSelectInit, cameras::cameras},
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

/// Row with a dropdown of detected V4L2 cameras bound to a webcam-device path
/// property. Falls back to just the "Automatic" entry when no camera exists.
pub(crate) fn webcam_device_select(property: &ConfigProperty<String>) -> SettingRowInit {
    let controller = DeviceSelectControl::builder()
        .launch(DeviceSelectInit {
            property: property.clone(),
            choices: cameras(),
        })
        .detach();

    let widget = controller.widget().clone();

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value: &String| value.clone()),
        control: widget.upcast(),
        keepalive: Box::new(controller),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
