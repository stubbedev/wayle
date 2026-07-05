use std::sync::Arc;

use wayle_config::ConfigService;

pub struct UserSessionInit {
    pub username: String,
    pub config: Arc<ConfigService>,
}

#[derive(Debug, Copy, Clone)]
pub enum UserSessionInput {
    Lock,
    Logout,
    Reboot,
    PowerOff,
}

#[derive(Debug)]
pub enum UserSessionCmd {
    FaceChanged(bool),
}
