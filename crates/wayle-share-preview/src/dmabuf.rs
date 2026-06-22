//! GPU buffer allocation for zero-copy screencast.
//!
//! Wraps a GBM device opened on a DRM render node and allocates linear,
//! single-plane buffer objects whose exported dmabuf fd is handed straight to
//! PipeWire (and through it to the consumer) — no CPU copy of pixel data.
//!
//! Everything here is best-effort: if a render node cannot be opened or a
//! buffer cannot be allocated the caller falls back to the SHM capture path,
//! which works everywhere.

use std::{
    fs::{File, OpenOptions},
    os::fd::OwnedFd,
};

use gbm::{BufferObject, BufferObjectFlags, Device, Format, Modifier};

/// A GBM device bound to a DRM render node.
pub struct GbmDevice {
    device: Device<File>,
}

/// One allocated buffer object plus the dmabuf parameters a consumer needs to
/// import it. Single plane only (the screencast format is packed BGRx).
pub struct DmaBuffer {
    /// Keeps the GPU allocation alive; dropped frees it.
    pub bo: BufferObject<()>,
    /// Exported dmabuf file descriptor for plane 0.
    pub fd: OwnedFd,
    /// Row stride in bytes.
    pub stride: u32,
    /// Plane 0 offset in bytes.
    pub offset: u32,
    /// DRM format modifier the allocation actually uses.
    pub modifier: Modifier,
    /// Pixel format (DRM fourcc).
    pub format: Format,
    pub width: u32,
    pub height: u32,
}

impl GbmDevice {
    /// Opens the first usable DRM render node (`/dev/dri/renderD128`…) and
    /// creates a GBM device on it.
    ///
    /// Returns `None` when no render node can be opened (headless, permissions,
    /// llvmpipe without a node) — the caller then uses SHM.
    #[must_use]
    pub fn open() -> Option<Self> {
        // Render nodes are numbered from 128. A handful covers every realistic
        // multi-GPU setup; the first that opens and accepts a GBM device wins.
        for n in 128..136 {
            let path = format!("/dev/dri/renderD{n}");
            let Ok(file) = OpenOptions::new().read(true).write(true).open(&path) else {
                continue;
            };
            match Device::new(file) {
                Ok(device) => {
                    log::debug!("screencast: opened GBM device {path} for dmabuf capture");
                    return Some(Self { device });
                }
                Err(err) => log::debug!("screencast: GBM device open failed for {path}: {err}"),
            }
        }
        None
    }

    /// Allocates a buffer matching the screencast pixel format (PipeWire `BGRx`
    /// ↔ DRM `XRGB8888`).
    ///
    /// # Errors
    ///
    /// Returns an error if allocation or fd export fails.
    pub fn allocate_bgrx(&self, width: u32, height: u32) -> Result<DmaBuffer, String> {
        self.allocate(width, height, Format::Xrgb8888)
    }

    /// Allocates a single-plane buffer object of `width`x`height` in `format`.
    ///
    /// Tries explicit-modifier allocation first (so the modifier is known and
    /// can be advertised to the consumer), falling back to implicit-modifier
    /// allocation. Returns the bo together with its exported dmabuf fd and
    /// plane geometry.
    ///
    /// # Errors
    ///
    /// Returns an error string if allocation or fd export fails.
    pub fn allocate(&self, width: u32, height: u32, format: Format) -> Result<DmaBuffer, String> {
        // RENDERING so the compositor can blit into it; LINEAR keeps the layout
        // simple and broadly importable across the GPU/driver boundary.
        let usage = BufferObjectFlags::RENDERING | BufferObjectFlags::LINEAR;
        let bo: BufferObject<()> = self
            .device
            .create_buffer_object_with_modifiers2(
                width,
                height,
                format,
                [Modifier::Linear].into_iter(),
                usage,
            )
            .or_else(|_| self.device.create_buffer_object(width, height, format, usage))
            .map_err(|e| format!("gbm allocation failed: {e}"))?;

        if bo.plane_count() != 1 {
            return Err(format!("unexpected multi-plane bo ({} planes)", bo.plane_count()));
        }
        let fd = bo
            .fd_for_plane(0)
            .map_err(|e| format!("dmabuf fd export failed: {e}"))?;
        Ok(DmaBuffer {
            stride: bo.stride_for_plane(0),
            offset: bo.offset(0),
            modifier: bo.modifier(),
            format,
            width,
            height,
            fd,
            bo,
        })
    }
}
