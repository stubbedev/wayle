use std::sync::Arc;

use wayle_config::ConfigService;
use wayle_notification::NotificationService;

pub struct NotificationDropdownInit {
    pub notification: Arc<NotificationService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum NotificationDropdownMsg {
    DndToggled(bool),
    ClearAll,
    NotificationDismissed,
}

#[derive(Debug)]
pub enum NotificationDropdownCmd {
    NotificationsChanged,
    DndChanged(bool),
    ScaleChanged(f32),
    IconSourceChanged,
    TimeTick,
}
