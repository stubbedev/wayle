use std::{
    io::Read,
    os::fd::{AsFd, OwnedFd},
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
    _owned_fds: Vec<OwnedFd>,
    /// Keeps the gbm buffer object alive while the wl_buffer references it.
    /// Boxed as `dyn` so [`Buffer`] does not have to name the gbm device type.
    /// Not `Send`: a [`Buffer`] lives entirely on the capture thread that built
    /// it, so the gbm bo never crosses threads.
    _bo: Box<dyn std::any::Any>,
}

impl DmabufBacking {
    /// Builds a backing from owned plane fds + the gbm bo to keep alive.
    #[must_use]
    pub fn new(
        format: u32,
        modifier: u64,
        owned_fds: Vec<OwnedFd>,
        offsets: &[u32],
        strides: &[u32],
        bo: Box<dyn std::any::Any>,
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
            _bo: bo,
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
    /// pixels live in GPU memory and are not read back here).
    fd: Option<memfd::Memfd>,
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
        let mfd = memfd::MemfdOptions::default()
            .create("buffer")
            .map_err(|err| Error::BufferCreate(err.into()))?;
        mfd.as_file()
            .set_len((width * height * 4) as u64)
            .map_err(|err| Error::BufferCreate(err.into()))?;
        let pool = shm.create_pool(
            mfd.as_file().as_fd(),
            (width * height * 4) as i32,
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
            fd: Some(mfd),
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
        }
    }

    /// read the bytes from the temporary buffer file
    ///
    /// # Errors
    ///
    /// Returns [`Error::NoShmBacking`] for a dmabuf buffer (which has no
    /// host-readable memfd), or [`Error::BufferRead`] if the read fails.
    pub fn get_bytes(&self) -> Result<Vec<u8>, Error> {
        let fd = self.fd.as_ref().ok_or(Error::NoShmBacking)?;
        let mut bytes = Vec::new();
        fd.as_file()
            .read_to_end(&mut bytes)
            .map_err(Error::BufferRead)?;
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
        let mut file = fd.as_file();
        let mut written = 0;
        while written < dst.len() {
            match file.read(&mut dst[written..]) {
                Ok(0) => break,
                Ok(n) => written += n,
                Err(err) => return Err(Error::BufferRead(err)),
            }
        }
        Ok(written)
    }

    /// clear the wayland buffer and remove the temporary file
    ///
    /// should only be called after [`get_bytes`] since all data gets deleted by this function
    pub fn destroy(&self) {
        self.buffer.destroy();
    }
}
