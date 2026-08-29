use std::collections::HashMap;

#[derive(Debug)]
pub enum VpnFormInput {
    /// Open the form empty, to create a VPN.
    ShowNew,
    /// Open the form on an existing profile.
    ShowEdit {
        uuid: String,
        name: String,
        kind: String,
        values: HashMap<String, String>,
    },
    /// A different VPN type was picked; the fields change with it.
    KindSelected(u32),
    SaveClicked,
    DeleteClicked,
    CancelClicked,
    /// NetworkManager refused the profile.
    Failed(String),
}

#[derive(Debug)]
pub enum VpnFormOutput {
    /// Create when `uuid` is absent, rewrite in place when it is present.
    Save {
        uuid: Option<String>,
        kind: String,
        name: String,
        values: HashMap<String, String>,
    },
    Delete(String),
    Cancel,
}
