//! Wayland frame capture for ScreenCast streams.
//!
//! Reuses [`wayle_share_preview`]'s wlr-screencopy ([`OutputManager`]) and
//! ext-image-copy ([`ExtToplevelManager`]) managers. A [`Capturer`] is opened
//! once for a [`CaptureTarget`] and re-captures a fresh frame on demand; the
//! PipeWire producer drives the timing.

use wayland_client::{
    Connection,
    protocol::{wl_buffer::WlBuffer, wl_output::WlOutput},
};
use wayle_share_preview::{
    buffer::Buffer, dmabuf::DmaBuffer, ext_capture::ExtToplevelManager, output::OutputManager,
};

use super::source::CaptureTarget;

/// A live capture source bound to one target, reused frame to frame.
pub enum Capturer {
    /// Whole-output capture via wlr-screencopy.
    Output {
        /// The screencopy manager (holds the Wayland connection).
        manager: OutputManager,
        /// The bound output.
        output: WlOutput,
        /// Composite the hardware cursor into each frame.
        cursor: bool,
    },
    /// Output-region capture via wlr-screencopy.
    Region {
        /// The screencopy manager.
        manager: OutputManager,
        /// The bound output.
        output: WlOutput,
        /// Region left in output-local pixels.
        x: i32,
        /// Region top in output-local pixels.
        y: i32,
        /// Region width.
        width: i32,
        /// Region height.
        height: i32,
        /// Composite the hardware cursor into each frame.
        cursor: bool,
    },
    /// Toplevel capture via ext-image-copy.
    Window {
        /// The ext capture manager (holds the toplevel handles + connection).
        manager: ExtToplevelManager,
        /// The bound toplevel handle.
        handle: wayle_share_preview::ext_capture::ExtToplevel,
        /// Composite the hardware cursor into each frame.
        cursor: bool,
    },
}

impl Capturer {
    /// Opens a capturer for `target`, resolving the output name / toplevel
    /// identifier against the live Wayland globals.
    ///
    /// # Errors
    ///
    /// Returns an error if the Wayland connection fails, the required capture
    /// protocol is unavailable, or the target output/window no longer exists.
    ///
    /// `cursor` requests the hardware cursor be composited into each frame
    /// (output/region targets only; the ext-image-copy window path has no
    /// equivalent overlay flag).
    pub fn open(target: &CaptureTarget, cursor: bool) -> Result<Self, String> {
        let connection =
            Connection::connect_to_env().map_err(|e| format!("cannot connect to wayland: {e}"))?;

        match target {
            CaptureTarget::Output(name) => {
                let manager = OutputManager::new(&connection)
                    .map_err(|e| format!("screencopy unavailable: {e}"))?;
                let output = find_output(&manager, name)?;
                Ok(Self::Output {
                    manager,
                    output,
                    cursor,
                })
            }
            CaptureTarget::Region {
                output,
                x,
                y,
                width,
                height,
            } => {
                let manager = OutputManager::new(&connection)
                    .map_err(|e| format!("screencopy unavailable: {e}"))?;
                let bound = find_output(&manager, output)?;
                Ok(Self::Region {
                    manager,
                    output: bound,
                    x: *x,
                    y: *y,
                    width: *width,
                    height: *height,
                    cursor,
                })
            }
            CaptureTarget::Window(identifier) => {
                let manager = ExtToplevelManager::new(&connection)
                    .map_err(|e| format!("toplevel capture unavailable: {e}"))?;
                let handle = manager
                    .toplevels()
                    .iter()
                    .find(|t| t.identifier.as_deref() == Some(identifier.as_str()))
                    .cloned()
                    .ok_or_else(|| format!("window '{identifier}' is gone"))?;
                Ok(Self::Window {
                    manager,
                    handle,
                    cursor,
                })
            }
        }
    }

    /// Captures one frame into an SHM [`Buffer`].
    ///
    /// # Errors
    ///
    /// Returns an error if the capture fails (output disappeared, protocol
    /// error, …).
    pub fn capture(&mut self) -> Result<Buffer, String> {
        match self {
            Self::Output {
                manager,
                output,
                cursor,
            } => manager
                .capture_output(output, *cursor)
                .map_err(|e| format!("output capture failed: {e}")),
            Self::Region {
                manager,
                output,
                x,
                y,
                width,
                height,
                cursor,
            } => manager
                .capture_output_region(output, *x, *y, *width, *height, *cursor)
                .map_err(|e| format!("region capture failed: {e}")),
            Self::Window {
                manager,
                handle,
                cursor,
            } => manager
                .capture_toplevel(&handle.handle, *cursor)
                .map_err(|e| format!("window capture failed: {e}")),
        }
    }

    /// Whether this target can capture into a dmabuf (zero-copy). Only the
    /// wlr-screencopy output/region paths support it; the ext-image-copy window
    /// path stays on SHM.
    #[must_use]
    pub fn supports_dmabuf(&self) -> bool {
        match self {
            Self::Output { manager, .. } | Self::Region { manager, .. } => {
                manager.supports_dmabuf()
            }
            Self::Window { .. } => false,
        }
    }

    /// Imports an allocated GPU buffer as a `wl_buffer` for reuse across
    /// captures.
    ///
    /// # Errors
    ///
    /// Returns an error if the target does not support dmabuf or the import
    /// fails.
    pub fn import_dmabuf(&mut self, dma: &DmaBuffer) -> Result<WlBuffer, String> {
        match self {
            Self::Output { manager, .. } | Self::Region { manager, .. } => manager
                .import_dmabuf(dma)
                .map_err(|e| format!("dmabuf import failed: {e}")),
            Self::Window { .. } => Err("dmabuf capture not supported for windows".to_owned()),
        }
    }

    /// Captures one frame straight into a dmabuf `wl_buffer` (zero-copy).
    ///
    /// # Errors
    ///
    /// Returns an error if the target does not support dmabuf or the capture
    /// fails.
    pub fn capture_into(&mut self, buffer: &WlBuffer) -> Result<(), String> {
        match self {
            Self::Output {
                manager,
                output,
                cursor,
            } => manager
                .capture_output_into(output, buffer, *cursor)
                .map_err(|e| format!("output dmabuf capture failed: {e}")),
            Self::Region {
                manager,
                output,
                x,
                y,
                width,
                height,
                cursor,
            } => manager
                .capture_output_region_into(output, *x, *y, *width, *height, buffer, *cursor)
                .map_err(|e| format!("region dmabuf capture failed: {e}")),
            Self::Window { .. } => Err("dmabuf capture not supported for windows".to_owned()),
        }
    }
}

/// Finds an output by `wl_output` name.
fn find_output(manager: &OutputManager, name: &str) -> Result<WlOutput, String> {
    manager
        .outputs
        .iter()
        .find(|(_, info)| info.name.as_deref() == Some(name))
        .map(|(output, _)| output.clone())
        .ok_or_else(|| format!("output '{name}' not found"))
}
