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
/// with a letterboxed `v4l2src` webcam picture-in-picture, encoded, and muxed.
/// Each audio source becomes its own track unless mixing is requested.
pub(crate) fn build(opts: &RecordOptions, cast: &ScreenCast) -> String {
    let fd = cast.fd.as_raw_fd();
    let node = cast.node_id;
    let fps = opts.framerate.max(1);
    let (screen_w, screen_h) = cast.size.unwrap_or(FALLBACK_SIZE);
    let path = &opts.output_path;

    let video_encoder = video_encoder(opts.format, opts.bitrate_kbps, opts.preset, fps);
    let audio_encoder = format!("opusenc bitrate={}", opts.audio.bitrate_kbps.max(16) * 1000);
    let muxer = muxer(opts.format);

    let mut desc = String::new();

    if let Some(cam) = &opts.webcam {
        let cam_w =
            (f64::from(screen_w) * f64::from(cam.size_percent.clamp(1, 100)) / 100.0) as i32;
        let cam_w = cam_w.max(80);
        let cam_h = (cam_w * 9 / 16).max(60);
        let (xpos, ypos) = webcam_xy(cam.position, screen_w, screen_h, cam_w, cam_h);
        let device = if cam.device.is_empty() {
            String::new()
        } else {
            format!(" device={}", cam.device)
        };

        // `add-borders=true` letterboxes the webcam into the box, so a camera
        // that isn't 16:9 keeps its aspect instead of being stretched.
        desc.push_str(&format!(
            "compositor name=comp background=black \
             sink_1::xpos={xpos} sink_1::ypos={ypos} sink_1::width={cam_w} sink_1::height={cam_h} \
             ! videoconvert ! queue ! {video_encoder} ! queue ! {muxer} name=mux ! filesink location=\"{path}\" \
             pipewiresrc fd={fd} path={node} do-timestamp=true ! videorate ! video/x-raw,framerate={fps}/1 ! videoconvert ! queue ! comp.sink_0 \
             v4l2src{device} ! videorate ! videoconvert ! videoscale add-borders=true ! video/x-raw,width={cam_w},height={cam_h},framerate={fps}/1 ! queue ! comp.sink_1 "
        ));
    } else {
        desc.push_str(&format!(
            "pipewiresrc fd={fd} path={node} do-timestamp=true ! videorate ! video/x-raw,framerate={fps}/1 \
             ! videoconvert ! queue ! {video_encoder} ! queue ! {muxer} name=mux ! filesink location=\"{path}\" "
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

    let mix = !opts.audio.separate_tracks && audio_sources.len() > 1;
    if mix {
        desc.push_str(&format!(
            "audiomixer name=amix ! audioconvert ! audioresample ! {audio_encoder} ! queue ! mux. "
        ));
        for source in &audio_sources {
            desc.push_str(&format!(
                "{source} ! queue ! audioconvert ! audioresample ! amix. "
            ));
        }
    } else {
        // One encoded track per source -> separate, individually editable tracks.
        for source in &audio_sources {
            desc.push_str(&format!(
                "{source} ! queue ! audioconvert ! audioresample ! {audio_encoder} ! queue ! mux. "
            ));
        }
    }

    desc.trim_end().to_owned()
}

/// Builds the video encoder element for a format.
fn video_encoder(
    format: OutputFormat,
    bitrate_kbps: u32,
    preset: crate::options::EncoderPreset,
    fps: u32,
) -> String {
    let bitrate = bitrate_kbps.max(500);
    // Keyframe every ~2s keeps files seekable without inflating size.
    let keyint = fps.saturating_mul(2).max(1);
    match format {
        // `tune=zerolatency` disables the lookahead and B-frame buffering that
        // otherwise make x264enc hold ~55 input frames before emitting its
        // first encoded frame. The screen source is damage-driven (variable
        // framerate), so a short recording of a near-static screen can deliver
        // fewer frames than that buffer, leaving the muxer with no data and
        // producing a 0-byte file. Zerolatency flushes each frame straight
        // through, so even brief or motionless captures always write output.
        OutputFormat::Mp4 | OutputFormat::Mkv => format!(
            "x264enc bitrate={bitrate} speed-preset={} tune=zerolatency key-int-max={keyint}",
            preset.x264()
        ),
        // `lag-in-frames=0` is the VP9 equivalent: no alt-ref lookahead, so the
        // encoder emits frames immediately instead of buffering them.
        OutputFormat::Webm => format!(
            "vp9enc target-bitrate={} cpu-used={} deadline=realtime lag-in-frames=0 keyframe-max-dist={keyint}",
            bitrate.saturating_mul(1000),
            preset.vp9_cpu_used()
        ),
    }
}

/// Returns the muxer element for a format.
fn muxer(format: OutputFormat) -> &'static str {
    match format {
        OutputFormat::Mp4 => "mp4mux",
        OutputFormat::Mkv => "matroskamux",
        OutputFormat::Webm => "webmmux",
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
