pub mod buffer;
pub mod dmabuf;
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
    /// Damage rects `(x, y, w, h)` accumulated from `zwlr_screencopy_frame_v1`
    /// `Damage` events for this frame, in capture order.
    pub damage: Vec<(u32, u32, u32, u32)>,
    /// dmabuf format learned from the `zwlr_screencopy_frame_v1` `LinuxDmabuf`
    /// event: `(drm_fourcc, width, height)`. Present only when the compositor
    /// advertises a dmabuf path for this frame.
    pub dmabuf_format: Option<(u32, u32, u32)>,
}
