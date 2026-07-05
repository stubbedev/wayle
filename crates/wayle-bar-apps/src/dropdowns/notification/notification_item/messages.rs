use std::sync::Arc;

use wayle_notification::core::notification::Notification;

use crate::shell::notification_popup::helpers::ResolvedIcon;

pub struct NotificationItemInit {
    pub notification: Arc<Notification>,
    pub resolved_icon: ResolvedIcon,
}

#[derive(Debug)]
pub enum NotificationItemInput {
    RefreshTime,
}

#[derive(Debug)]
pub enum NotificationItemOutput {
    Dismissed(u32),
}
