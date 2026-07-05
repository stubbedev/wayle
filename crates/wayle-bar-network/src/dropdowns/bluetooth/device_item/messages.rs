use zbus::zvariant::OwnedObjectPath;

use crate::shell::bar::dropdowns::bluetooth::helpers::DeviceSnapshot;

pub struct DeviceItemInit {
    pub snapshot: DeviceSnapshot,
}

#[derive(Debug)]
pub enum DeviceItemInput {
    Clicked,
    Hovered(bool),
    ForgetClicked,
}

#[derive(Debug)]
pub enum DeviceItemOutput {
    Connect(OwnedObjectPath),
    Disconnect(OwnedObjectPath),
    Forget(OwnedObjectPath),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PendingAction {
    Connecting,
    Disconnecting,
    Forgetting,
}
