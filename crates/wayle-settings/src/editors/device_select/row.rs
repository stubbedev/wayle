use relm4::{gtk::prelude::*, prelude::*};
use wayle_config::ConfigProperty;

use crate::{
    editors::device_select::{
        DeviceChoice, DeviceSelectControl, DeviceSelectInit, DeviceSelectMsg, cameras::cameras,
        microphones::microphones,
    },
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

/// Row with a dropdown of detected microphone sources bound to a
/// microphone-device name property. Sources are fetched asynchronously from the
/// audio service; until they arrive (or if it is unavailable) only the
/// "Default" entry is shown.
pub(crate) fn microphone_device_select(property: &ConfigProperty<String>) -> SettingRowInit {
    let controller = DeviceSelectControl::builder()
        .launch(DeviceSelectInit {
            property: property.clone(),
            choices: vec![DeviceChoice {
                id: String::new(),
                label: String::from("Default"),
            }],
        })
        .detach();

    // Source names come from the running shell over D-Bus (zbus needs the tokio
    // runtime), so enumerate on tokio and let the relm4 sender marshal the list
    // back to the main thread once it resolves.
    let sender = controller.sender().clone();
    tokio::spawn(async move {
        let _ = sender.send(DeviceSelectMsg::SetChoices(microphones().await));
    });

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
