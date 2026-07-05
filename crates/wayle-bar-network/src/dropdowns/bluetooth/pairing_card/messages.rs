use wayle_bluetooth::types::agent::PairingRequest;

pub struct PairingCardInit;

#[derive(Debug)]
pub enum PairingCardMsg {
    SetRequest {
        request: PairingRequest,
        device_name: String,
        device_icon: &'static str,
        device_type_key: &'static str,
    },
    Clear,
    Confirm,
    Reject,
    Cancel,
}
