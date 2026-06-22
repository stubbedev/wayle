//! Wayland frame capture for ScreenCast streams.
//!
//! Reuses [`wayle_share_preview`]'s wlr-screencopy ([`OutputManager`]) and
//! ext-image-copy ([`ExtToplevelManager`]) managers. A [`Capturer`] is opened
//! once for a [`CaptureTarget`] and re-captures a fresh frame on demand; the
//! PipeWire producer drives the timing.

use wayland_client::{
    Connection,
    protocol::wl_output::{Transform, WlOutput},
};
use wayle_share_preview::{buffer::Buffer, ext_capture::ExtToplevelManager, output::OutputManager};

use super::source::CaptureTarget;

/// A live capture source bound to one target, reused frame to frame.
pub enum Capturer {
    /// Whole-output capture via wlr-screencopy.
    Output {
        /// The screencopy manager (holds the Wayland connection).
        manager: OutputManager,
        /// The bound output.
        output: WlOutput,
        /// Whether to composite the cursor into captured frames.
        show_cursor: bool,
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
        /// Whether to composite the cursor into captured frames.
        show_cursor: bool,
    },
    /// Toplevel capture via ext-image-copy.
    Window {
        /// The ext capture manager (holds the toplevel handles + connection).
        manager: ExtToplevelManager,
        /// The bound toplevel handle.
        handle: wayle_share_preview::ext_capture::ExtToplevel,
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
    pub fn open(target: &CaptureTarget, show_cursor: bool) -> Result<Self, String> {
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
                    show_cursor,
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
                    show_cursor,
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
                Ok(Self::Window { manager, handle })
            }
        }
    }

    /// Captures one frame into a [`Buffer`].
    ///
    /// When `prefer_dmabuf` is set and this is a whole-output capture, the
    /// dmabuf zero-copy path is tried, transparently falling back to SHM if
    /// anything in that path is unavailable or fails (see
    /// [`OutputManager::capture_output_dmabuf_or_shm`]). The caller sets
    /// `prefer_dmabuf` only when the PipeWire stream actually negotiated a
    /// dmabuf buffer, so a stream that negotiated SHM always gets a readable SHM
    /// buffer and never regresses. Region and window capture always use SHM. The
    /// returned buffer carries its damage rects and, when zero-copy succeeded,
    /// its dmabuf import parameters.
    ///
    /// # Errors
    ///
    /// Returns an error if the capture fails (output disappeared, protocol
    /// error, …).
    pub fn capture(&mut self, prefer_dmabuf: bool) -> Result<Buffer, String> {
        match self {
            Self::Output {
                manager,
                output,
                show_cursor,
            } => {
                if prefer_dmabuf {
                    manager
                        .capture_output_dmabuf_or_shm(output, *show_cursor)
                        .map_err(|e| format!("output capture failed: {e}"))
                } else {
                    manager
                        .capture_output_with_cursor(output, *show_cursor)
                        .map_err(|e| format!("output capture failed: {e}"))
                }
            }
            Self::Region {
                manager,
                output,
                x,
                y,
                width,
                height,
                show_cursor,
            } => manager
                .capture_output_region_with_cursor(output, *x, *y, *width, *height, *show_cursor)
                .map_err(|e| format!("region capture failed: {e}")),
            Self::Window { manager, handle } => manager
                .capture_toplevel(&handle.handle)
                .map_err(|e| format!("window capture failed: {e}")),
        }
    }

    /// The bound output's refresh rate in mHz (millihertz), if known. `None`
    /// for window capture, where there is no single output refresh.
    #[must_use]
    pub fn refresh_mhz(&self) -> Option<i32> {
        let (manager, output) = match self {
            Self::Output {
                manager, output, ..
            }
            | Self::Region {
                manager, output, ..
            } => (manager, output),
            Self::Window { .. } => return None,
        };
        manager
            .outputs
            .iter()
            .find(|(o, _)| o == output)
            .and_then(|(_, info)| info.mode.as_ref())
            .map(|mode| mode.refresh)
    }

    /// The bound output's rotation/flip, from its `wl_output` geometry. Used to
    /// emit the `SPA_META_VideoTransform` so a rotated monitor streams the right
    /// way up. Window capture has no single output transform, so it reports
    /// [`Transform::Normal`] (identity).
    #[must_use]
    pub fn transform(&self) -> Transform {
        let (manager, output) = match self {
            Self::Output {
                manager, output, ..
            }
            | Self::Region {
                manager, output, ..
            } => (manager, output),
            Self::Window { .. } => return Transform::Normal,
        };
        manager
            .outputs
            .iter()
            .find(|(o, _)| o == output)
            .and_then(|(_, info)| info.geometry.as_ref())
            .map_or(Transform::Normal, |geo| geo.transform)
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
