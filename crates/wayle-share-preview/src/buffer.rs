use std::{
    os::{
        fd::{AsFd, OwnedFd},
        unix::fs::FileExt,
    },
    sync::Arc,
};

use wayland_client::{
    Dispatch, QueueHandle,
    protocol::{
        wl_buffer::WlBuffer,
        wl_shm::{Format, WlShm},
        wl_shm_pool::WlShmPool,
    },
};

use crate::error::Error;

/// One dmabuf plane's import parameters, as handed to the consumer's PipeWire
/// stream (`SPA_DATA_DmaBuf`).
#[derive(Debug, Clone, Copy)]
pub struct DmabufPlane {
    /// dmabuf file descriptor for this plane.
    pub fd: i32,
    /// Byte offset of the plane within the dmabuf.
    pub offset: u32,
    /// Plane stride in bytes.
    pub stride: u32,
}

/// The dmabuf backing of a [`Buffer`], present only when the zero-copy dmabuf
/// path succeeded. When this is `None` the buffer is plain SHM and the existing
/// mmap-copy path is used.
///
/// Best-effort: anything that prevents allocating/importing a dmabuf leaves
/// this `None` and capture continues over SHM exactly as before. The plane fds
/// are owned here so they stay valid for the lifetime of the buffer and are
/// closed on drop.
#[derive(Debug)]
pub struct DmabufBacking {
    /// DRM fourcc the buffer was allocated with.
    pub format: u32,
    /// dmabuf layout modifier (`DRM_FORMAT_MOD_*`).
    pub modifier: u64,
    /// Per-plane import parameters (fd/offset/stride).
    pub planes: Vec<DmabufPlane>,
    /// Keeps the plane fds alive; the `i32` fds in `planes` borrow from these.
    /// `OwnedFd` is `Send + Sync`, so a [`Buffer`] (and the capture
    /// [`crate::Frame`] used as Wayland dispatch userdata) stays `Send + Sync`.
    _owned_fds: Vec<OwnedFd>,
}

impl DmabufBacking {
    /// Builds a backing from owned plane fds.
    ///
    /// The gbm buffer object is intentionally NOT stored here: `fd_for_plane`
    /// dups an independent dmabuf fd and the imported `wl_buffer` holds the
    /// kernel reference, so the bo only needs to outlive `wl_buffer` creation
    /// (it stays a local in the capture function). Keeping it out makes this
    /// type `Send` — the gbm bo (`*mut` ffi handle) is not.
    #[must_use]
    pub fn new(
        format: u32,
        modifier: u64,
        owned_fds: Vec<OwnedFd>,
        offsets: &[u32],
        strides: &[u32],
    ) -> Self {
        let planes = owned_fds
            .iter()
            .enumerate()
            .map(|(i, fd)| DmabufPlane {
                fd: std::os::fd::AsRawFd::as_raw_fd(fd),
                offset: offsets.get(i).copied().unwrap_or(0),
                stride: strides.get(i).copied().unwrap_or(0),
            })
            .collect();
        Self {
            format,
            modifier,
            planes,
            _owned_fds: owned_fds,
        }
    }
}

#[derive(Debug)]
pub struct Buffer {
    pub buffer: WlBuffer,
    pub width: u32,
    pub height: u32,
    pub stride: u32,
    pub format: Format,
    /// Damaged regions reported for this frame, as `(x, y, w, h)` in buffer
    /// pixels. Empty when the compositor reported no damage (or the capture
    /// path — e.g. ext-image-copy window capture — does not report damage); the
    /// consumer should then treat the whole frame as damaged.
    pub damage: Vec<(u32, u32, u32, u32)>,
    /// dmabuf import parameters when the zero-copy path succeeded; `None` for
    /// the SHM path. See [`DmabufBacking`].
    pub dmabuf: Option<DmabufBacking>,
    /// SHM memfd backing the [`WlBuffer`]; `None` for dmabuf buffers (whose
    /// pixels live in GPU memory and are not read back here). `Arc` so a pooled
    /// [`ShmSlot`] and the per-frame [`Buffer`] leased from it share one memfd.
    fd: Option<Arc<memfd::Memfd>>,
    /// True when this `Buffer` is a lightweight lease over a pooled [`ShmSlot`]
    /// (shares the slot's `wl_buffer`). [`destroy`](Self::destroy) is then a
    /// no-op — the slot owns the compositor object and outlives the lease.
    leased: bool,
}

impl Buffer {
    /// create a new buffer to store a single frame
    pub fn new<
        K: Send + Sync + Clone + 'static,
        T: Dispatch<WlBuffer, K> + Dispatch<WlShmPool, K> + Dispatch<WlShm, K> + 'static,
    >(
        shm: &WlShm,
        width: u32,
        height: u32,
        stride: u32,
        format: Format,
        handle: &QueueHandle<T>,
        udata: K,
    ) -> Result<Self, Error> {
        // Size the pool by `stride * height`, not `width * 4 * height`: the
        // compositor may pad the stride past `width * 4`, and the `wl_buffer`
        // is created with that stride, so a pool sized to the unpadded width is
        // too small and the compositor raises a protocol error.
        let pool_size = (stride * height) as u64;
        let mfd = memfd::MemfdOptions::default()
            .create("buffer")
            .map_err(|err| Error::BufferCreate(err.into()))?;
        mfd.as_file()
            .set_len(pool_size)
            .map_err(|err| Error::BufferCreate(err.into()))?;
        let pool = shm.create_pool(
            mfd.as_file().as_fd(),
            pool_size as i32,
            handle,
            udata.clone(),
        );
        let buffer = pool.create_buffer(
            0,
            width as i32,
            height as i32,
            stride as i32,
            format,
            handle,
            udata,
        );

        pool.destroy();
        Ok(Self {
            buffer,
            width,
            height,
            stride,
            format,
            damage: Vec::new(),
            dmabuf: None,
            fd: Some(Arc::new(mfd)),
            leased: false,
        })
    }

    /// Wrap an already-created dmabuf-backed [`WlBuffer`] (built via
    /// `zwp_linux_buffer_params_v1::create_immed`) into a [`Buffer`]. The SHM
    /// `fd` is absent; pixel readback ([`get_bytes`](Self::get_bytes) /
    /// [`read_into`](Self::read_into)) is unavailable for dmabuf buffers (the
    /// consumer maps the dmabuf directly).
    #[must_use]
    pub fn from_dmabuf(
        buffer: WlBuffer,
        width: u32,
        height: u32,
        stride: u32,
        format: Format,
        backing: DmabufBacking,
    ) -> Self {
        Self {
            buffer,
            width,
            height,
            stride,
            format,
            damage: Vec::new(),
            dmabuf: Some(backing),
            fd: None,
            leased: false,
        }
    }

    /// read the bytes from the temporary buffer file
    ///
    /// # Errors
    ///
    /// Returns [`Error::NoShmBacking`] for a dmabuf buffer (which has no
    /// host-readable memfd), or [`Error::BufferRead`] if the read fails.
    pub fn get_bytes(&self) -> Result<Vec<u8>, Error> {
        let len = (self.stride as usize).saturating_mul(self.height as usize);
        let mut bytes = vec![0u8; len];
        let read = self.read_into(&mut bytes)?;
        bytes.truncate(read);
        Ok(bytes)
    }

    /// Read the frame straight into a caller-provided buffer, returning the
    /// number of bytes read. Avoids the per-call [`Vec`] allocation of
    /// [`get_bytes`](Self::get_bytes) — used by the continuous capture path
    /// (e.g. the portal's PipeWire producer) where a destination buffer already
    /// exists and is reused every frame.
    ///
    /// # Errors
    ///
    /// Returns [`Error::NoShmBacking`] for a dmabuf buffer, or
    /// [`Error::BufferRead`] if reading the backing memfd fails.
    pub fn read_into(&self, dst: &mut [u8]) -> Result<usize, Error> {
        let fd = self.fd.as_ref().ok_or(Error::NoShmBacking)?;
        let file = fd.as_file();
        let mut written = 0;
        // Positioned reads (pread): the memfd is shared across frames via a
        // pooled `ShmSlot` lease, so we must NOT advance the file's offset.
        // `read`/`read_to_end` would leave the offset at EOF after the first
        // frame, making every later frame read 0 bytes (a frozen stream).
        while written < dst.len() {
            match file.read_at(&mut dst[written..], written as u64) {
                Ok(0) => break,
                Ok(n) => written += n,
                Err(err) => return Err(Error::BufferRead(err)),
            }
        }
        Ok(written)
    }

    /// Destroy the compositor-side `wl_buffer`.
    ///
    /// No-op for a leased buffer (one obtained from [`ShmSlot::lease`]): the
    /// pooled [`ShmSlot`] owns the shared `wl_buffer` and is reused across
    /// frames, so destroying it here would invalidate the next frame's reuse.
    /// Owned buffers (SHM created via [`new`](Self::new), or per-frame dmabuf
    /// buffers) destroy normally.
    pub fn destroy(&self) {
        if !self.leased {
            self.buffer.destroy();
        }
    }
}

/// A reusable SHM capture target kept across frames.
///
/// The continuous-capture path (the portal's PipeWire producer) captures at the
/// stream's fixed geometry every frame. Allocating a fresh memfd, `wl_shm_pool`,
/// and `wl_buffer` per frame is wasteful; an [`ShmSlot`] allocates that backing
/// once and hands out a cheap [`Buffer`] lease ([`lease`](Self::lease)) each
/// frame that shares the same `wl_buffer` and memfd. Reuse is sound because
/// capture is synchronous — the previous frame's copy has completed (the
/// screencopy `Ready` event fired) before the next capture reuses the slot.
///
/// `Clone` shares the underlying `wl_buffer` proxy + memfd (same shallow-share
/// semantics as the other proxy fields on the owning manager); the slot is not
/// cloned in the capture path.
#[derive(Clone)]
pub struct ShmSlot {
    buffer: WlBuffer,
    fd: Arc<memfd::Memfd>,
    /// Geometry/format the slot was allocated for; reused only while these match.
    pub width: u32,
    pub height: u32,
    pub stride: u32,
    pub format: Format,
}

impl ShmSlot {
    /// Allocates a reusable memfd-backed `wl_buffer` for the given geometry.
    ///
    /// # Errors
    ///
    /// Returns [`Error::BufferCreate`] if the memfd cannot be created or sized.
    pub fn new<
        K: Send + Sync + Clone + 'static,
        T: Dispatch<WlBuffer, K> + Dispatch<WlShmPool, K> + Dispatch<WlShm, K> + 'static,
    >(
        shm: &WlShm,
        width: u32,
        height: u32,
        stride: u32,
        format: Format,
        handle: &QueueHandle<T>,
        udata: K,
    ) -> Result<Self, Error> {
        // See `Buffer::new`: size by `stride * height` to tolerate padded strides.
        let pool_size = (stride * height) as u64;
        let mfd = memfd::MemfdOptions::default()
            .create("buffer")
            .map_err(|err| Error::BufferCreate(err.into()))?;
        mfd.as_file()
            .set_len(pool_size)
            .map_err(|err| Error::BufferCreate(err.into()))?;
        let pool = shm.create_pool(
            mfd.as_file().as_fd(),
            pool_size as i32,
            handle,
            udata.clone(),
        );
        let buffer = pool.create_buffer(
            0,
            width as i32,
            height as i32,
            stride as i32,
            format,
            handle,
            udata,
        );
        pool.destroy();
        Ok(Self {
            buffer,
            fd: Arc::new(mfd),
            width,
            height,
            stride,
            format,
        })
    }

    /// Whether this slot matches the requested capture geometry/format.
    #[must_use]
    pub fn matches(&self, width: u32, height: u32, stride: u32, format: Format) -> bool {
        self.width == width
            && self.height == height
            && self.stride == stride
            && self.format == format
    }

    /// A cheap per-frame [`Buffer`] sharing this slot's `wl_buffer` + memfd.
    #[must_use]
    pub fn lease(&self) -> Buffer {
        Buffer {
            buffer: self.buffer.clone(),
            width: self.width,
            height: self.height,
            stride: self.stride,
            format: self.format,
            damage: Vec::new(),
            dmabuf: None,
            fd: Some(self.fd.clone()),
            leased: true,
        }
    }

    /// Destroy the pooled `wl_buffer` (call when discarding/replacing the slot).
    pub fn destroy(&self) {
        self.buffer.destroy();
    }
}
