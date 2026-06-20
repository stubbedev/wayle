//! Builds the `gst-launch`-style pipeline description for a recording.
//!
//! The H.264 path prefers a hardware encoder (VA-API, then NVENC) when one is
//! actually present on the machine, and falls back to software x264 otherwise.
//! Software encoders ship with the application's bundled GStreamer plugins, so
//! a recording always works on any machine even with no GPU encoder — the
//! caller ([`crate::Recorder::start`]) retries on the software path if a
//! detected hardware encoder fails to start. WebM/VP9 always uses software:
//! VP9 hardware encoders are not broadly available (NVENC cannot encode VP9).

use std::{os::fd::AsRawFd, thread::available_parallelism};

use gstreamer as gst;

use crate::{
    options::{OutputFormat, RecordOptions},
    portal::ScreenCast,
};

/// Fallback screen size when the portal does not report one.
const FALLBACK_SIZE: (i32, i32) = (1920, 1080);
/// Opus audio bitrate per track, in bits per second. 128 kbps is transparent
/// for both speech and desktop audio, so it is no longer worth exposing.
const AUDIO_BITRATE: u32 = 128_000;
/// Constant-quality target for the H.264 (x264) encoder. CRF-style: lower is
/// higher quality / larger files. 18 is visually near-lossless and keeps small
/// text and UI edges crisp and legible — deliberately favouring quality over
/// file size, since screen recordings are made to be read back.
const X264_QUALITY: u32 = 18;
/// Constant-quality target for the VP9 (WebM) encoder, on its 0–63 scale. Kept
/// low (high quality) for the same legibility reasons as [`X264_QUALITY`].
const VP9_CQ_LEVEL: u32 = 20;
/// Constant quantizer for hardware H.264 encoders (VA-API / NVENC). Comparable
/// in perceived quality to [`X264_QUALITY`]; low enough to keep text crisp.
const HW_H264_QP: u32 = 20;

/// A built pipeline description plus whether its video encoder is hardware.
///
/// The flag lets [`crate::Recorder::start`] retry on the software path if a
/// detected hardware encoder turns out to fail at launch on this machine.
pub(crate) struct Built {
    /// The `gst-launch`-style description for [`gstreamer::parse::launch`].
    pub description: String,
    /// Whether the chosen video encoder is hardware-accelerated.
    pub hardware: bool,
}

/// Builds a pipeline, preferring a hardware video encoder when one is present.
///
/// The screen comes from `pipewiresrc` (portal node), optionally composited
/// with a letterboxed `v4l2src` webcam picture-in-picture, encoded, and muxed.
/// Each audio source becomes its own track unless mixing is requested.
pub(crate) fn build(opts: &RecordOptions, cast: &ScreenCast) -> Built {
    build_with(opts, cast, true)
}

/// Builds the same pipeline but forces the software video encoder. Used as the
/// fallback when a hardware encoder fails to start.
pub(crate) fn build_software(opts: &RecordOptions, cast: &ScreenCast) -> Built {
    build_with(opts, cast, false)
}

fn build_with(opts: &RecordOptions, cast: &ScreenCast, allow_hardware: bool) -> Built {
    let fd = cast.fd.as_raw_fd();
    let node = cast.node_id;
    let fps = opts.framerate.max(1);
    let (screen_w, screen_h) = cast.size.unwrap_or(FALLBACK_SIZE);
    // Quote so paths/devices with spaces or special chars don't break the
    // gst-launch parser (which otherwise splits on whitespace and `!`).
    let path = quote(&opts.output_path);

    // Encode with as many threads as the machine actually has, rather than a
    // baked-in count, so capture keeps up on big and small CPUs alike.
    let threads = available_parallelism().map_or(1, |n| n.get());
    let (video_encoder, hardware) = video_encoder(opts.format, fps, threads, allow_hardware);
    // Hardware H.264 encoders emit a byte-stream that mp4/matroska muxers won't
    // accept directly; h264parse converts it (and is a harmless no-op for the
    // software encoder, which already negotiates the muxer's caps).
    let parser = video_parser(opts.format);
    let audio_encoder = format!("opusenc bitrate={AUDIO_BITRATE}");
    let muxer = muxer(opts.format);

    let mut desc = String::new();

    if let Some(cam) = &opts.webcam {
        let cam_w =
            (f64::from(screen_w) * f64::from(cam.size_percent.clamp(1, 100)) / 100.0) as i32;
        let cam_w = cam_w.max(80);
        let cam_h = (cam_w * 9 / 16).max(60);
        let (xpos, ypos) =
            webcam_xy(cam.x_percent, cam.y_percent, screen_w, screen_h, cam_w, cam_h);
        let device = if cam.device.is_empty() {
            String::new()
        } else {
            format!(" device={}", quote(&cam.device))
        };

        // `add-borders=true` letterboxes the webcam into the box, so a camera
        // that isn't 16:9 keeps its aspect instead of being stretched.
        desc.push_str(&format!(
            "compositor name=comp background=black \
             sink_1::xpos={xpos} sink_1::ypos={ypos} sink_1::width={cam_w} sink_1::height={cam_h} \
             ! videoconvert n-threads=0 ! queue leaky=downstream ! {video_encoder} ! queue ! {parser}{muxer} name=mux ! filesink location={path} \
             pipewiresrc fd={fd} path={node} do-timestamp=true ! videorate ! video/x-raw,framerate={fps}/1 ! videoconvert n-threads=0 ! queue ! comp.sink_0 \
             v4l2src{device} ! videorate ! videoconvert n-threads=0 ! videoscale add-borders=true ! video/x-raw,width={cam_w},height={cam_h},framerate={fps}/1 ! queue ! comp.sink_1 "
        ));
    } else {
        desc.push_str(&format!(
            "pipewiresrc fd={fd} path={node} do-timestamp=true ! videorate ! video/x-raw,framerate={fps}/1 \
             ! videoconvert n-threads=0 ! queue leaky=downstream ! {video_encoder} ! queue ! {parser}{muxer} name=mux ! filesink location={path} "
        ));
    }

    let mut audio_sources: Vec<String> = Vec::new();
    if opts.audio.system_audio {
        audio_sources.push(String::from("pulsesrc device=@DEFAULT_MONITOR@"));
    }
    if opts.audio.microphone {
        if opts.audio.microphone_device.is_empty() {
            audio_sources.push(String::from("pulsesrc"));
        } else {
            audio_sources.push(format!(
                "pulsesrc device={}",
                quote(&opts.audio.microphone_device)
            ));
        }
    }

    // Mix every audio source into one track so any player reproduces mic and
    // system audio together (separate tracks confused players that only play
    // the first track).
    if audio_sources.len() > 1 {
        desc.push_str(&format!(
            "audiomixer name=amix ! audioconvert ! audioresample ! {audio_encoder} ! queue ! mux. "
        ));
        for source in &audio_sources {
            desc.push_str(&format!(
                "{source} ! queue ! audioconvert ! audioresample ! amix. "
            ));
        }
    } else {
        for source in &audio_sources {
            desc.push_str(&format!(
                "{source} ! queue ! audioconvert ! audioresample ! {audio_encoder} ! queue ! mux. "
            ));
        }
    }

    Built {
        description: desc.trim_end().to_owned(),
        hardware,
    }
}

/// Wraps a value in double quotes for the `gst-launch` parser, escaping any
/// embedded backslashes and quotes. Without this, a path or device name
/// containing a space, `!`, or quote breaks pipeline parsing.
fn quote(value: &str) -> String {
    let escaped = value.replace('\\', "\\\\").replace('"', "\\\"");
    format!("\"{escaped}\"")
}

/// Builds the video encoder element for a format, returning the element string
/// and whether it is hardware-accelerated.
///
/// Encoding is constant-quality (CRF / constant-QP) rather than constant-
/// bitrate: the encoder spends bits where the picture needs them and almost
/// none on a static screen, giving better quality at smaller files than a fixed
/// bitrate — with no knob to tune. The software `threads` count comes from the
/// host's actual core count so the encode keeps up in real time on whatever
/// machine it runs on.
fn video_encoder(
    format: OutputFormat,
    fps: u32,
    threads: usize,
    allow_hardware: bool,
) -> (String, bool) {
    // Keyframe every ~2s keeps files seekable without inflating size.
    let keyint = fps.saturating_mul(2).max(1);
    match format {
        OutputFormat::Mp4 | OutputFormat::Mkv => h264_encoder(keyint, threads, allow_hardware),
        // VP9 in constant-quality mode (`end-usage=cq` + `cq-level`). The high
        // `target-bitrate` is only a ceiling, so it never clamps quality, and
        // `lag-in-frames=0` is the alt-ref-lookahead equivalent of zerolatency:
        // frames are emitted immediately so brief captures still write output.
        // `tile-columns` (log2) is what lets VP9 spread work across cores;
        // derive it from the core count and let libvpx cap it to the frame
        // width, so we never over- or under-subscribe a given machine.
        OutputFormat::Webm => {
            let tile_columns = log2_floor(threads).min(6);
            let enc = format!(
                "vp9enc threads={threads} tile-columns={tile_columns} deadline=1 cpu-used=4 lag-in-frames=0 end-usage=cq cq-level={VP9_CQ_LEVEL} target-bitrate=25000 keyframe-max-dist={keyint}"
            );
            (enc, false)
        }
    }
}

/// Picks the best available H.264 encoder: VA-API, then NVENC, then software
/// x264. Hardware encoders are used only when their element is registered,
/// which the GStreamer `va` / `nvcodec` plugins do only when a capable device
/// is actually present — so this is a real "is the GPU encoder here?" check.
fn h264_encoder(keyint: u32, threads: usize, allow_hardware: bool) -> (String, bool) {
    if allow_hardware {
        // VA-API (Intel / AMD): constant-QP for consistent, crisp quality.
        if has_factory("vah264enc") {
            return (
                format!(
                    "vah264enc rate-control=cqp qpi={HW_H264_QP} qpp={HW_H264_QP} qpb={HW_H264_QP} key-int-max={keyint}"
                ),
                true,
            );
        }
        // NVENC (NVIDIA): constant-QP equivalent.
        if has_factory("nvh264enc") {
            return (
                format!(
                    "nvh264enc preset=hq rc-mode=constqp qp-const={HW_H264_QP} gop-size={keyint}"
                ),
                true,
            );
        }
    }
    // Software fallback, always available (bundled). `tune=zerolatency` disables
    // the lookahead / B-frame buffering that otherwise makes x264enc hold ~55
    // input frames before its first output. The screen source is damage-driven
    // (variable framerate), so a short capture of a near-static screen can
    // deliver fewer frames than that buffer, leaving the muxer with no data and
    // a 0-byte file. Zerolatency flushes each frame straight through, so even
    // brief or motionless captures always write output. `pass=qual quantizer=`
    // is x264's constant-quality (CRF) mode.
    let enc = format!(
        "x264enc threads={threads} speed-preset=veryfast tune=zerolatency pass=qual quantizer={X264_QUALITY} key-int-max={keyint}"
    );
    (enc, false)
}

/// Whether a GStreamer element factory with this name is registered.
fn has_factory(name: &str) -> bool {
    gst::ElementFactory::find(name).is_some()
}

/// The parser element (with trailing `! `) to insert between the video encoder
/// and the muxer, or empty when none is needed. H.264 muxers require parsed
/// `avc` access units; `h264parse` provides them (and is harmless for x264).
fn video_parser(format: OutputFormat) -> &'static str {
    match format {
        OutputFormat::Mp4 | OutputFormat::Mkv => "h264parse ! ",
        OutputFormat::Webm => "",
    }
}

/// Floor of log2 for a positive count (0 for inputs ≤ 1). Used to turn a core
/// count into VP9's log2-scaled `tile-columns`.
fn log2_floor(n: usize) -> u32 {
    usize::BITS - 1 - n.max(1).leading_zeros()
}

/// Returns the muxer element for a format.
fn muxer(format: OutputFormat) -> &'static str {
    match format {
        OutputFormat::Mp4 => "mp4mux",
        OutputFormat::Mkv => "matroskamux",
        OutputFormat::Webm => "webmmux",
    }
}

/// Top-left pixel offset of the webcam frame from relative position percentages.
///
/// `x_percent`/`y_percent` (0-100) place the frame within the free space left
/// over after the frame's own size, so 0 is flush to the left/top edge and 100
/// is flush to the right/bottom edge. Being relative keeps placement consistent
/// across monitors of different resolutions.
fn webcam_xy(
    x_percent: u32,
    y_percent: u32,
    screen_w: i32,
    screen_h: i32,
    cam_w: i32,
    cam_h: i32,
) -> (i32, i32) {
    let free_w = (screen_w - cam_w).max(0);
    let free_h = (screen_h - cam_h).max(0);
    let xpos = free_w * i32::try_from(x_percent.min(100)).unwrap_or(100) / 100;
    let ypos = free_h * i32::try_from(y_percent.min(100)).unwrap_or(100) / 100;
    (xpos, ypos)
}
