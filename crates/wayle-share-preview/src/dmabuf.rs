//! Best-effort dmabuf (zero-copy) allocation for screencopy.
//!
//! This wraps `gbm` to allocate a DRM buffer object on a render node and turn
//! its planes into dmabuf fds that the consumer's PipeWire stream imports with
//! `SPA_DATA_DmaBuf` — avoiding the SHM mmap copy on the hot path.
//!
//! **Everything here is best-effort with a guaranteed SHM fallback** (see
//! [`crate::output::OutputManager::capture_output_dmabuf_or_shm`]): any failure
//! — no DRM render node, gbm device open fails, the format is unsupported, or
//! the bo allocation fails — returns an [`Error`] so the caller cleanly drops
//! back to the SHM path. Capture never regresses.
//!
//! **Unverified on hardware.** The DRM device chosen here is the system's
//! default render node, which need not be the same GPU the compositor renders
//! the captured output on. On a multi-GPU system the compositor may reject the
//! import (we fall back to SHM). The allocation, fd export, and
//! `zwp_linux_dmabuf` import wiring follow xdph's `Screencopy.cpp` but have not
//! been exercised against a live compositor in this tree.

use std::{fs::OpenOptions, os::fd::OwnedFd};

use drm_fourcc::{DrmFourcc, DrmModifier};
use gbm::{BufferObjectFlags, Device as GbmDeviceInner};
use wayland_client::protocol::wl_shm::Format;

use crate::error::Error;

/// An opened gbm device on a DRM render node, reused across captures.
pub struct GbmDevice {
    inner: GbmDeviceInner<OwnedFd>,
}

/// The result of allocating one dmabuf buffer object: the modifier the driver
/// chose plus the per-plane fds/offsets/strides, and the bo kept alive (boxed)
/// so the exported fds stay valid while the wl_buffer references it.
pub struct DmabufAlloc {
    /// dmabuf layout modifier the bo ended up with.
    pub modifier: u64,
    /// Owned plane fds (closed when this/the resulting buffer is dropped).
    pub owned_fds: Vec<OwnedFd>,
    /// Per-plane byte offsets.
    pub offsets: Vec<u32>,
    /// Per-plane strides in bytes.
    pub strides: Vec<u32>,
    /// The gbm buffer object, boxed to keep it (and the underlying GPU memory)
    /// alive for the lifetime of the wl_buffer.
    pub bo: Box<dyn std::any::Any>,
}

/// DRM render nodes to try, in order. Render nodes are unprivileged and the
/// right device for offscreen allocation.
const RENDER_NODES: &[&str] = &[
    "/dev/dri/renderD128",
    "/dev/dri/renderD129",
    "/dev/dri/renderD130",
];

/// Opens a gbm device on the first DRM render node that works.
///
/// # Errors
///
/// Returns [`Error::DmabufUnavailable`] if no render node can be opened as a
/// gbm device.
pub fn open_gbm_device() -> Result<GbmDevice, Error> {
    let mut last_err = String::from("no DRM render node found");
    for node in RENDER_NODES {
        let file = match OpenOptions::new().read(true).write(true).open(node) {
            Ok(f) => f,
            Err(err) => {
                last_err = format!("open {node}: {err}");
                continue;
            }
        };
        let fd: OwnedFd = file.into();
        match GbmDeviceInner::new(fd) {
            Ok(inner) => {
                log::debug!("opened gbm device on {node}");
                return Ok(GbmDevice { inner });
            }
            Err(err) => last_err = format!("gbm_create_device {node}: {err}"),
        }
    }
    Err(Error::DmabufUnavailable(last_err))
}

/// Allocates a dmabuf bo of `width`x`height` in `drm_fourcc`, trying modifiers
/// in the order [`crate::source`-style] preference dictates.
///
/// `advertised` is the consumer's modifier list (may be empty). We try a
/// modifier-aware allocation first; if that fails we fall back to a plain
/// allocation with `RENDERING` usage (driver picks an implicit modifier).
///
/// # Errors
///
/// Returns [`Error::DmabufUnavailable`] for an unknown fourcc or if every
/// allocation attempt fails.
pub fn allocate(
    device: &GbmDevice,
    drm_fourcc: u32,
    width: u32,
    height: u32,
    advertised: &[u64],
) -> Result<DmabufAlloc, Error> {
    let format = DrmFourcc::try_from(drm_fourcc)
        .map_err(|_| Error::DmabufUnavailable(format!("unknown DRM fourcc {drm_fourcc:#x}")))?;

    // Candidate modifiers, most-preferred first, always ending with the safe
    // INVALID/LINEAR fallbacks. Mirrors the pure helper in the portal's
    // `screencast::source::dmabuf_modifier_candidates`, kept inline here so the
    // share-preview crate has no dependency on the portal crate.
    let candidates = modifier_candidates(advertised);

    // First, try an explicit modifier-list allocation (one modifier at a time;
    // the driver returns whichever it actually used via `bo.modifier()`).
    for &m in &candidates {
        let modifier = DrmModifier::from(m);
        if let Ok(alloc) = try_alloc_with_modifier(device, format, width, height, modifier) {
            return Ok(alloc);
        }
    }

    // Fall back to the legacy non-modifier API: let the driver choose.
    match device.inner.create_buffer_object::<()>(
        width,
        height,
        format,
        BufferObjectFlags::RENDERING,
    ) {
        Ok(bo) => finish_bo(bo),
        Err(err) => Err(Error::DmabufUnavailable(format!(
            "gbm bo alloc failed for {format:?} {width}x{height}: {err}"
        ))),
    }
}

fn try_alloc_with_modifier(
    device: &GbmDevice,
    format: DrmFourcc,
    width: u32,
    height: u32,
    modifier: DrmModifier,
) -> Result<DmabufAlloc, Error> {
    let bo = device
        .inner
        .create_buffer_object_with_modifiers2::<()>(
            width,
            height,
            format,
            std::iter::once(modifier),
            BufferObjectFlags::RENDERING,
        )
        .map_err(|err| {
            Error::DmabufUnavailable(format!("gbm bo alloc with modifier failed: {err}"))
        })?;
    finish_bo(bo)
}

/// Exports the plane fds/offsets/strides from an allocated bo.
fn finish_bo(bo: gbm::BufferObject<()>) -> Result<DmabufAlloc, Error> {
    let modifier: u64 = bo.modifier().into();
    let plane_count = bo.plane_count();

    let mut owned_fds = Vec::with_capacity(plane_count as usize);
    let mut offsets = Vec::with_capacity(plane_count as usize);
    let mut strides = Vec::with_capacity(plane_count as usize);

    for plane in 0..plane_count as i32 {
        let fd = bo.fd_for_plane(plane).map_err(|err| {
            Error::DmabufUnavailable(format!("gbm fd_for_plane({plane}): {err:?}"))
        })?;
        owned_fds.push(fd);
        offsets.push(bo.offset(plane));
        strides.push(bo.stride_for_plane(plane));
    }

    if owned_fds.is_empty() {
        return Err(Error::DmabufUnavailable("bo reported zero planes".into()));
    }

    Ok(DmabufAlloc {
        modifier,
        owned_fds,
        offsets,
        strides,
        bo: Box::new(bo),
    })
}

/// Candidate modifiers in preference order (explicit modifiers first, then the
/// universally-safe `INVALID`/`LINEAR` fallbacks). Never empty.
fn modifier_candidates(advertised: &[u64]) -> Vec<u64> {
    /// `DRM_FORMAT_MOD_INVALID`.
    const MOD_INVALID: u64 = 0x00ff_ffff_ffff_ffff;
    /// `DRM_FORMAT_MOD_LINEAR`.
    const MOD_LINEAR: u64 = 0;

    let mut out: Vec<u64> = Vec::new();
    for &m in advertised {
        if !out.contains(&m) {
            out.push(m);
        }
    }
    if !out.contains(&MOD_INVALID) {
        out.push(MOD_INVALID);
    }
    if !out.contains(&MOD_LINEAR) {
        out.push(MOD_LINEAR);
    }
    out
}

/// Maps a DRM fourcc to the matching `wl_shm::Format` so the dmabuf-backed
/// [`crate::buffer::Buffer`] can carry the same `Format` an SHM buffer would.
/// Only the 32-bit packed formats screencopy hands out are mapped; anything
/// else returns `None` and the caller falls back to SHM.
#[must_use]
pub fn shm_format_from_fourcc(fourcc: u32) -> Option<Format> {
    let f = DrmFourcc::try_from(fourcc).ok()?;
    Some(match f {
        DrmFourcc::Argb8888 => Format::Argb8888,
        DrmFourcc::Xrgb8888 => Format::Xrgb8888,
        DrmFourcc::Abgr8888 => Format::Abgr8888,
        DrmFourcc::Xbgr8888 => Format::Xbgr8888,
        _ => return None,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn modifier_candidates_never_empty_and_appends_fallbacks() {
        const INVALID: u64 = 0x00ff_ffff_ffff_ffff;
        const LINEAR: u64 = 0;
        assert_eq!(modifier_candidates(&[]), vec![INVALID, LINEAR]);
        assert_eq!(modifier_candidates(&[7]), vec![7, INVALID, LINEAR]);
        assert_eq!(modifier_candidates(&[LINEAR]), vec![LINEAR, INVALID]);
    }

    #[test]
    fn shm_format_maps_packed_formats() {
        assert_eq!(
            shm_format_from_fourcc(DrmFourcc::Xrgb8888 as u32),
            Some(Format::Xrgb8888)
        );
        assert_eq!(
            shm_format_from_fourcc(DrmFourcc::Argb8888 as u32),
            Some(Format::Argb8888)
        );
        // NV12 (planar) is not a packed format we stream -> None.
        assert_eq!(shm_format_from_fourcc(DrmFourcc::Nv12 as u32), None);
        // Garbage fourcc -> None.
        assert_eq!(shm_format_from_fourcc(0xdead_beef), None);
    }
}
