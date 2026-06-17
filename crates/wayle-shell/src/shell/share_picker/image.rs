//! Conversion from a captured [`Image`] into a GDK pixbuf for display.

use gdk_pixbuf::Pixbuf;
use wayle_share_preview::image::{Image, ImageKind};

/// Turns a captured frame into a [`Pixbuf`] that a `gtk::Picture` can show.
pub(super) trait ImageExt {
    /// Converts the image to RGB and wraps its bytes in a [`Pixbuf`].
    ///
    /// # Errors
    ///
    /// Returns an error if the underlying RGB conversion fails.
    fn into_pixbuf(self) -> Result<Pixbuf, Box<dyn std::error::Error>>;
}

impl ImageExt for Image {
    fn into_pixbuf(self) -> Result<Pixbuf, Box<dyn std::error::Error>> {
        let rgb_image = match self.into_rgb()?.buffer {
            ImageKind::Xrgb(_) => return Err("image was not converted to rgb".into()),
            ImageKind::Rgb(image_buffer) => image_buffer,
        };

        let height = rgb_image.height() as i32;
        let width = rgb_image.width() as i32;

        let bytes = gtk4::glib::Bytes::from(&rgb_image.into_vec());
        let pixbuf = Pixbuf::from_bytes(
            &bytes,
            gtk4::gdk_pixbuf::Colorspace::Rgb,
            false,
            8,
            width,
            height,
            width * 3,
        );
        Ok(pixbuf)
    }
}
