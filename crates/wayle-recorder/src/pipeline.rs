//! Builds the `gst-launch`-style pipeline description for a recording.

use std::os::fd::AsRawFd;

use crate::{
    options::{OutputFormat, RecordOptions, WebcamPosition},
    portal::ScreenCast,
};

/// Pixel gap between the webcam frame and the screen edge.
const WEBCAM_MARGIN: i32 = 24;
/// Fallback screen size when the portal does not report one.
const FALLBACK_SIZE: (i32, i32) = (1920, 1080);

/// Builds a pipeline description string for [`gstreamer::parse::launch`].
///
/// The screen comes from `pipewiresrc` (portal node), optionally composited
/// with a `v4l2src` webcam picture-in-picture, encoded, and muxed to the output
/// file. Audio sources are mixed, encoded, and muxed into the same file.
///
pub(crate) fn build(opts: &RecordOptions, cast: &ScreenCast) -> String {
    let fd = cast.fd.as_raw_fd();
    let node = cast.node_id;
    let fps = opts.framerate.max(1);
    let (screen_w, screen_h) = cast.size.unwrap_or(FALLBACK_SIZE);
    let path = &opts.output_path;

    let (video_encoder, audio_encoder, muxer) = codecs(opts.format, opts.bitrate_kbps);

    let mut desc = String::new();

    if let Some(cam) = &opts.webcam {
        let cam_w = (f64::from(screen_w) * f64::from(cam.size_percent.clamp(1, 100)) / 100.0) as i32;
        let cam_w = cam_w.max(80);
        let cam_h = (cam_w * 9 / 16).max(60);
        let (xpos, ypos) = webcam_xy(cam.position, screen_w, screen_h, cam_w, cam_h);
        let device = if cam.device.is_empty() {
            String::new()
        } else {
            format!(" device={}", cam.device)
        };

        desc.push_str(&format!(
            "compositor name=comp background=black \
             sink_1::xpos={xpos} sink_1::ypos={ypos} sink_1::width={cam_w} sink_1::height={cam_h} \
             ! videoconvert ! {video_encoder} ! queue ! {muxer} name=mux ! filesink location=\"{path}\" \
             pipewiresrc fd={fd} path={node} do-timestamp=true ! videorate ! video/x-raw,framerate={fps}/1 ! videoconvert ! comp.sink_0 \
             v4l2src{device} ! videoconvert ! videoscale ! video/x-raw,width={cam_w},height={cam_h} ! comp.sink_1 "
        ));
    } else {
        desc.push_str(&format!(
            "pipewiresrc fd={fd} path={node} do-timestamp=true ! videorate ! video/x-raw,framerate={fps}/1 \
             ! videoconvert ! {video_encoder} ! queue ! {muxer} name=mux ! filesink location=\"{path}\" "
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
            audio_sources.push(format!("pulsesrc device={}", opts.audio.microphone_device));
        }
    }

    if !audio_sources.is_empty() {
        desc.push_str(&format!(
            "audiomixer name=amix ! audioconvert ! audioresample ! {audio_encoder} ! queue ! mux. "
        ));
        for source in &audio_sources {
            desc.push_str(&format!("{source} ! queue ! audioconvert ! amix. "));
        }
    }

    desc.trim_end().to_owned()
}

/// Returns `(video_encoder, audio_encoder, muxer)` for a format.
fn codecs(format: OutputFormat, bitrate_kbps: u32) -> (String, &'static str, &'static str) {
    let bitrate = bitrate_kbps.max(500);
    match format {
        OutputFormat::Mp4 => (
            format!("x264enc bitrate={bitrate} speed-preset=veryfast tune=zerolatency"),
            "opusenc",
            "mp4mux",
        ),
        OutputFormat::Mkv => (
            format!("x264enc bitrate={bitrate} speed-preset=veryfast tune=zerolatency"),
            "opusenc",
            "matroskamux",
        ),
        OutputFormat::Webm => (
            format!("vp9enc target-bitrate={}", bitrate.saturating_mul(1000)),
            "opusenc",
            "webmmux",
        ),
    }
}

/// Top-left pixel offset of the webcam frame for the chosen corner.
fn webcam_xy(
    position: WebcamPosition,
    screen_w: i32,
    screen_h: i32,
    cam_w: i32,
    cam_h: i32,
) -> (i32, i32) {
    let m = WEBCAM_MARGIN;
    match position {
        WebcamPosition::TopLeft => (m, m),
        WebcamPosition::TopRight => ((screen_w - cam_w - m).max(0), m),
        WebcamPosition::BottomLeft => (m, (screen_h - cam_h - m).max(0)),
        WebcamPosition::BottomRight => {
            ((screen_w - cam_w - m).max(0), (screen_h - cam_h - m).max(0))
        }
    }
}
