//! Screenshot host.
//!
//! A headless GTK-thread component that performs captures on behalf of the
//! `com.wayle.Screenshot1` D-Bus service. Capture runs here (rather than in the
//! tokio service) because the region path awaits the in-shell region overlay
//! and the clipboard copy uses `gdk::Clipboard`, both of which need the GTK
//! main thread.
//!
//! The flow per request: resolve the capture target (opening the region overlay
//! for `region`), capture a full-resolution image, save a PNG, optionally copy
//! it to the clipboard and fire a `notify-send`, then reply with the path.

mod capture;

use std::{
    collections::HashMap,
    path::{Path, PathBuf},
    process::Stdio,
    sync::Arc,
};

use capture::{CaptureKind, WindowTarget, capture};
use hyprland::shared::{HyprData, HyprDataActiveOptional};
use image::RgbImage;
use relm4::{
    gtk,
    gtk::{gdk, glib, prelude::*},
    prelude::*,
};
use tokio::sync::oneshot;
use tracing::warn;
use wayle_config::{ConfigService, schemas::modules::ScreenshotConfig};
use wayle_hyprland::HyprlandService;
use wayle_mango::MangoService;
use wayle_niri::NiriService;

use crate::shell::helpers::monitors::current_monitors;

/// Messages driving the screenshot host.
pub(crate) enum ScreenshotInput {
    /// Capture a screenshot. `mode` is `region`/`output`/`window`; `target` is
    /// an optional output name. The saved path (empty on cancel) is returned.
    Capture {
        mode: String,
        target: String,
        reply: oneshot::Sender<Result<String, String>>,
    },
}

impl std::fmt::Debug for ScreenshotInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Capture { mode, target, .. } => f
                .debug_struct("Capture")
                .field("mode", mode)
                .field("target", target)
                .finish_non_exhaustive(),
        }
    }
}

/// Init for the screenshot host: config plus the optional compositor services
/// used to resolve the focused output / active window.
pub(crate) struct ScreenshotInit {
    pub(crate) config: Arc<ConfigService>,
    pub(crate) hyprland: Option<Arc<HyprlandService>>,
    pub(crate) niri: Option<Arc<NiriService>>,
    pub(crate) mango: Option<Arc<MangoService>>,
}

/// The screenshot host component.
pub(crate) struct Screenshot {
    config: Arc<ConfigService>,
    hyprland: Option<Arc<HyprlandService>>,
    niri: Option<Arc<NiriService>>,
    mango: Option<Arc<MangoService>>,
}

#[relm4::component(pub(crate))]
impl Component for Screenshot {
    type Init = ScreenshotInit;
    type Input = ScreenshotInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        // Headless: this component owns no visible surface.
        #[root]
        gtk::Window {
            set_decorated: false,
            set_visible: false,
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        _sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let model = Screenshot {
            config: init.config,
            hyprland: init.hyprland,
            niri: init.niri,
            mango: init.mango,
        };
        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: ScreenshotInput, _sender: ComponentSender<Self>, _root: &Self::Root) {
        let ScreenshotInput::Capture {
            mode,
            target,
            reply,
        } = msg;
        let config = self.config.clone();
        // Resolve compositor-specific focus up front (sync, GTK thread).
        let focused_output = self.focused_output_name();
        let window_target = if mode == "window" {
            self.active_window_target()
        } else {
            Default::default()
        };
        glib::spawn_future_local(async move {
            let result = run(config, mode, target, focused_output, window_target).await;
            let _ = reply.send(result);
        });
    }
}

impl Screenshot {
    /// Connector name of the focused output, resolved from whichever compositor
    /// service is present. `None` lets capture fall back to the first output.
    fn focused_output_name(&self) -> Option<String> {
        if self.hyprland.is_some()
            && let Ok(monitors) = hyprland::data::Monitors::get()
            && let Some(name) = monitors.into_iter().find(|m| m.focused).map(|m| m.name)
        {
            return Some(name);
        }
        if let Some(mango) = &self.mango
            && let Some(name) = mango
                .monitors
                .get()
                .iter()
                .find(|m| m.is_active)
                .map(|m| m.name.clone())
        {
            return Some(name);
        }
        // niri / unknown: caller falls back to the first output.
        None
    }

    /// Identifies the active window from whichever compositor service is present.
    fn active_window_target(&self) -> WindowTarget {
        if self.hyprland.is_some()
            && let Ok(Some(client)) = hyprland::data::Client::get_active()
        {
            let address = format!("{}", client.address);
            let handle = u64::from_str_radix(address.trim_start_matches("0x"), 16).ok();
            return WindowTarget {
                hyprland_handle: handle,
                app_id: Some(client.class),
                title: Some(client.title),
            };
        }
        if let Some(niri) = &self.niri
            && let Some(id) = niri.focused_window_id.get()
            && let Some(window) = niri.window(id)
        {
            return WindowTarget {
                hyprland_handle: None,
                app_id: window.app_id.get(),
                title: window.title.get(),
            };
        }
        if let Some(mango) = &self.mango
            && let Some(client) = mango.focused_client.get()
        {
            return WindowTarget {
                hyprland_handle: None,
                app_id: client.app_id,
                title: client.title,
            };
        }
        WindowTarget::default()
    }
}

/// Resolves the target, captures, saves, and applies clipboard/notify options.
async fn run(
    config: Arc<ConfigService>,
    mode: String,
    target: String,
    focused_output: Option<String>,
    window_target: WindowTarget,
) -> Result<String, String> {
    let image = match mode.as_str() {
        "region" => match capture_region().await? {
            Some(image) => image,
            // Cancelled — not an error; report an empty path.
            None => return Ok(String::new()),
        },
        "output" => {
            let name = if target.is_empty() {
                focused_output
            } else {
                Some(target)
            };
            capture(CaptureKind::Output(name))?
        }
        "window" => capture(CaptureKind::Window(window_target))?,
        other => return Err(format!("unknown screenshot mode: {other}")),
    };

    let settings = config.config().modules.screenshot.snapshot();
    let dir = resolve_dir(&settings.output_directory);
    if let Err(err) = std::fs::create_dir_all(&dir) {
        return Err(format!("cannot create {}: {err}", dir.display()));
    }
    let path = dir.join(filename(&settings.filename_format));
    save_png(&image, &path)?;
    let path_str = path.to_string_lossy().into_owned();

    if settings.copy_to_clipboard {
        copy_to_clipboard(&path);
    }
    if settings.notify {
        notify_saved(&path_str);
    }

    Ok(path_str)
}

/// Freeze-frame region capture.
///
/// Captures every output up front (before the region overlay maps, so any
/// transient popups on screen survive), shows the overlay painting those frozen
/// frames, then crops the selection out of the in-memory frame for its output.
/// Returns `None` when the user cancels.
async fn capture_region() -> Result<Option<RgbImage>, String> {
    let frozen = capture::capture_all_outputs()?;

    // Logical size per connector, to scale the logical selection back to the
    // frame's physical resolution when cropping.
    let logical: HashMap<String, (i32, i32)> = current_monitors()
        .into_iter()
        .map(|(connector, monitor)| {
            let geometry = monitor.geometry();
            (connector, (geometry.width(), geometry.height()))
        })
        .collect();

    // Textures for the overlay to paint (display only); the host keeps the
    // frames for cropping.
    let texture = std::time::Instant::now();
    let frames: HashMap<String, gdk::Texture> = frozen
        .iter()
        .map(|frame| (frame.connector.clone(), rgb_to_texture(&frame.image)))
        .collect();
    if cfg!(debug_assertions) {
        tracing::info!(
            texture_ms = texture.elapsed().as_millis(),
            "screenshot freeze: textures built, showing overlay"
        );
    }

    let Some(selection) = crate::services::region_overlay::request_region(frames).await else {
        return Ok(None);
    };

    let frame = frozen
        .iter()
        .find(|frame| frame.connector == selection.output)
        .ok_or("captured output for selection no longer present")?;
    let (logical_width, logical_height) = logical
        .get(&selection.output)
        .copied()
        .ok_or("no logical geometry for the selected output")?;

    Ok(Some(capture::crop_frozen(
        &frame.image,
        logical_width,
        logical_height,
        &selection,
    )))
}

/// Wraps an RGB frame in a `gdk::Texture` for the overlay to paint. The image is
/// packed `R8G8B8`, so the stride is `width * 3`.
fn rgb_to_texture(image: &RgbImage) -> gdk::Texture {
    let stride = image.width() as usize * 3;
    let bytes = glib::Bytes::from(image.as_raw().as_slice());
    gdk::MemoryTexture::new(
        image.width() as i32,
        image.height() as i32,
        gdk::MemoryFormat::R8g8b8,
        &bytes,
        stride,
    )
    .upcast()
}

/// Encodes the frame as PNG with fast compression — screenshots favour a snappy
/// save over the smallest possible file.
fn save_png(image: &RgbImage, path: &Path) -> Result<(), String> {
    use image::{
        ExtendedColorType, ImageEncoder,
        codecs::png::{CompressionType, FilterType, PngEncoder},
    };

    let file = std::fs::File::create(path)
        .map_err(|e| format!("cannot create {}: {e}", path.display()))?;
    let writer = std::io::BufWriter::new(file);
    PngEncoder::new_with_quality(writer, CompressionType::Fast, FilterType::Adaptive)
        .write_image(
            image.as_raw(),
            image.width(),
            image.height(),
            ExtendedColorType::Rgb8,
        )
        .map_err(|e| format!("cannot save {}: {e}", path.display()))
}

/// Plain snapshot of the `[screenshot]` options used per capture.
struct Settings {
    output_directory: String,
    filename_format: String,
    copy_to_clipboard: bool,
    notify: bool,
}

trait ScreenshotConfigExt {
    fn snapshot(&self) -> Settings;
}

impl ScreenshotConfigExt for ScreenshotConfig {
    fn snapshot(&self) -> Settings {
        Settings {
            output_directory: self.output_directory.get(),
            filename_format: self.filename_format.get(),
            copy_to_clipboard: self.copy_to_clipboard.get(),
            notify: self.notify.get(),
        }
    }
}

/// Resolves the save directory: the configured dir, else `$XDG_PICTURES_DIR`,
/// else `$HOME/Pictures`.
fn resolve_dir(configured: &str) -> PathBuf {
    if !configured.is_empty() {
        return PathBuf::from(configured);
    }
    if let Some(dir) = std::env::var_os("XDG_PICTURES_DIR") {
        return PathBuf::from(dir);
    }
    std::env::var_os("HOME")
        .map(|home| PathBuf::from(home).join("Pictures"))
        .unwrap_or_else(|| PathBuf::from("."))
}

/// Builds a timestamped file name from a `chrono` format string.
fn filename(format: &str) -> String {
    chrono::Local::now().format(format).to_string()
}

/// Copies the saved PNG to the Wayland clipboard via `gdk::Clipboard`.
fn copy_to_clipboard(path: &Path) {
    let Some(display) = gdk::Display::default() else {
        warn!("no gdk display for clipboard copy");
        return;
    };
    match gdk::Texture::from_filename(path) {
        Ok(texture) => display.clipboard().set_texture(&texture),
        Err(err) => warn!(error = %err, "cannot load screenshot texture for clipboard"),
    }
}

/// Fires a fire-and-forget `notify-send` reporting where the shot was saved.
fn notify_saved(path: &str) {
    let mut command = std::process::Command::new("notify-send");
    command
        .arg("--app-name=Wayle")
        .arg(format!("--icon={path}"))
        .arg("Screenshot saved")
        .arg(path)
        .stdout(Stdio::null())
        .stderr(Stdio::null());

    match command.spawn() {
        // Reap on a detached thread so we leave no zombie and need no runtime.
        Ok(mut child) => {
            std::thread::spawn(move || {
                let _ = child.wait();
            });
        }
        Err(err) => warn!(error = %err, "cannot spawn notify-send"),
    }
}
