#[derive(Debug)]
pub enum PasswordFormInput {
    Show {
        ssid: String,
        security_label: String,
        signal_icon: &'static str,
        error_message: Option<String>,
    },
    ConnectClicked,
    CancelClicked,
}

#[derive(Debug)]
pub enum PasswordFormOutput {
    Connect { password: String },
    Cancel,
}
