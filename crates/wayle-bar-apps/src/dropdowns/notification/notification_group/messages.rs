use std::sync::Arc;

use wayle_config::schemas::modules::notification::IconSource;
use wayle_notification::core::notification::Notification;

pub struct NotificationGroupInit {
    pub app_name: Option<String>,
    pub notifications: Vec<Arc<Notification>>,
    pub icon_source: IconSource,
}

#[derive(Debug)]
pub enum NotificationGroupInput {
    ToggleExpanded,
    ShowAll,
    ClearGroup,
    UpdateNotifications(Vec<Arc<Notification>>),
    RefreshTime,
    ItemDismissed(u32),
}

#[derive(Debug)]
pub enum NotificationGroupOutput {
    Dismissed,
}
