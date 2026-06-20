pub mod buffer;
pub mod error;
pub mod ext_capture;
pub mod frame;
pub mod image;
pub mod output;
mod protocols;
pub mod toplevel;

#[derive(Default)]
struct Frame {
    pub ready: bool,
    pub requested: bool,
    pub buffer: Option<buffer::Buffer>,
    pub error: Option<error::Error>,
}
