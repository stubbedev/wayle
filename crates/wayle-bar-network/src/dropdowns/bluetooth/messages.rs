use std::sync::Arc;

use wayle_bluetooth::{BluetoothService, types::agent::PairingRequest};
use wayle_config::ConfigService;
use wayle_core::DeferredService;
use zbus::zvariant::OwnedObjectPath;

pub struct BluetoothDropdownInit {
    pub bluetooth: DeferredService<BluetoothService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum BluetoothDropdownMsg {
    BluetoothToggled(bool),
    ScanRequested,
    DeviceAction(DeviceActionMsg),
    PairingCard(PairingCardOutput),
}

#[derive(Debug)]
pub enum DeviceActionMsg {
    Connect(OwnedObjectPath),
    Disconnect(OwnedObjectPath),
    Forget(OwnedObjectPath),
}

#[derive(Debug)]
pub enum PairingCardOutput {
    Cancelled,
    PinSubmitted(String),
    PasskeyConfirmed,
    PasskeyRejected,
    AuthorizationAccepted,
    AuthorizationRejected,
    ServiceAuthorizationAccepted,
    ServiceAuthorizationRejected,
    LegacyPinSubmitted(String),
}

#[derive(Debug)]
pub enum BluetoothDropdownCmd {
    ServiceReady(Arc<BluetoothService>),
    ScaleChanged(f32),
    EnabledChanged(bool),
    AvailableChanged(bool),
    ScanComplete,
    DevicesChanged,
    DevicePropertyChanged,
    DeviceActionFailed(OwnedObjectPath),
    PairingRequested(Option<PairingRequest>),
}
