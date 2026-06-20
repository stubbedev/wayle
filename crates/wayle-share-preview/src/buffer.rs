use std::{io::Read, os::fd::AsFd};

use wayland_client::{
    Dispatch, QueueHandle,
    protocol::{
        wl_buffer::WlBuffer,
        wl_shm::{Format, WlShm},
        wl_shm_pool::WlShmPool,
    },
};

use crate::error::Error;

#[derive(Debug)]
pub struct Buffer {
    pub buffer: WlBuffer,
    pub width: u32,
    pub height: u32,
    pub stride: u32,
    pub format: Format,
    fd: memfd::Memfd,
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
            fd: mfd,
        })
    }

    /// read the bytes from the temporary buffer file
    pub fn get_bytes(&self) -> Result<Vec<u8>, Error> {
        // let mut file = unsafe { File::from_raw_fd(self.fd) };
        let mut bytes = Vec::new();
        self.fd
            .as_file()
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
    /// Returns [`Error::BufferRead`] if reading the backing memfd fails.
    pub fn read_into(&self, dst: &mut [u8]) -> Result<usize, Error> {
        let mut file = self.fd.as_file();
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
