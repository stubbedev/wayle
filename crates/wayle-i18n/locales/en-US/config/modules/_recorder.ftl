### Wayle Configuration - Recorder Module

## Recorder Module Configuration

settings-modules-recorder-icon-idle = Idle Icon
    .description = Icon when not recording

settings-modules-recorder-icon-recording = Recording Icon
    .description = Icon while recording

settings-modules-recorder-icon-paused = Paused Icon
    .description = Icon while recording is paused

settings-modules-recorder-format = Format
    .description = Format string using placeholders: {"{{ state }}"}, {"{{ elapsed }}"}

settings-modules-recorder-microphone = Microphone
    .description = Capture the microphone in the recording

settings-modules-recorder-microphone-device = Microphone Device
    .description = Microphone source name (empty uses the default source)

settings-modules-recorder-system-audio = System Audio
    .description = Capture desktop audio in the recording

settings-modules-recorder-bitrate-kbps = Bitrate
    .description = Video bitrate in kilobits per second

settings-modules-recorder-audio-bitrate-kbps = Audio Bitrate
    .description = Audio bitrate per track in kilobits per second

settings-modules-recorder-separate-audio-tracks = Separate Audio Tracks
    .description = Keep microphone and system audio as separate, editable tracks

settings-modules-recorder-encoder-preset = Encoder Preset
    .description = Speed/quality trade-off; slower presets produce smaller files

settings-modules-recorder-framerate = Framerate
    .description = Capture framerate in frames per second

settings-modules-recorder-webcam-enabled = Webcam Frame
    .description = Overlay a webcam picture-in-picture frame into the recording

settings-modules-recorder-webcam-device = Webcam Device
    .description = V4L2 device path (empty auto-selects the first camera)

settings-modules-recorder-webcam-position = Webcam Position
    .description = Corner the webcam frame is anchored to

settings-modules-recorder-webcam-size = Webcam Size
    .description = Webcam frame width as a percentage of the recording width

settings-modules-recorder-output-directory = Output Directory
    .description = Where recordings are saved (empty uses the XDG Videos directory)

settings-modules-recorder-output-format = Output Format
    .description = Container format / codec preset

settings-modules-recorder-show-cursor = Show Cursor
    .description = Draw the mouse cursor in the recording

settings-modules-recorder-border-show = Show Border
    .description = Display border around button

settings-modules-recorder-border-color = Border Color
    .description = Border color token

settings-modules-recorder-icon-show = Show Icon
    .description = Display module icon

settings-modules-recorder-icon-color = Icon Color
    .description = Icon foreground color

settings-modules-recorder-icon-bg-color = Icon Background
    .description = Icon container background color

settings-modules-recorder-label-show = Show Label
    .description = Display label

settings-modules-recorder-label-color = Label Color
    .description = Label text color

settings-modules-recorder-label-max-length = Label Max Length
    .description = Max characters before truncation

settings-modules-recorder-button-bg-color = Button Background
    .description = Button background color

settings-modules-recorder-left-click = Left Click
    .description = Shell command on left click

settings-modules-recorder-right-click = Right Click
    .description = Shell command on right click

settings-modules-recorder-middle-click = Middle Click
    .description = Shell command on middle click

settings-modules-recorder-scroll-up = Scroll Up
    .description = Shell command on scroll up

settings-modules-recorder-scroll-down = Scroll Down
    .description = Shell command on scroll down


## WebcamPosition variants
enum-webcam-position-top-left = Top Left
enum-webcam-position-top-right = Top Right
enum-webcam-position-bottom-left = Bottom Left
enum-webcam-position-bottom-right = Bottom Right

## EncoderPreset variants
enum-encoder-preset-speed = Speed
enum-encoder-preset-balanced = Balanced
enum-encoder-preset-quality = Quality

## RecorderFormat variants
enum-recorder-format-mp4 = MP4
enum-recorder-format-mkv = MKV
enum-recorder-format-webm = WebM
