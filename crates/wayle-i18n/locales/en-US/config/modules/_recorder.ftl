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

settings-modules-recorder-framerate = Framerate
    .description = Capture framerate in frames per second

settings-modules-recorder-webcam-enabled = Webcam Frame
    .description = Overlay a webcam picture-in-picture frame into the recording

settings-modules-recorder-webcam-device = Webcam Device
    .description = V4L2 device path (empty auto-selects the first camera)

settings-modules-recorder-webcam-x = Webcam X Position
    .description = Horizontal position (0% = left, 100% = right), relative so it survives resolution changes

settings-modules-recorder-webcam-y = Webcam Y Position
    .description = Vertical position (0% = top, 100% = bottom), relative so it survives resolution changes

settings-modules-recorder-webcam-size = Webcam Size
    .description = Webcam frame width as a percentage of the recording width

settings-modules-recorder-output-directory = Output Directory
    .description = Where recordings are saved (empty uses the XDG Videos directory)

settings-modules-recorder-output-format = Output Format
    .description = Container format / codec preset

settings-modules-recorder-show-cursor = Show Cursor
    .description = Draw the mouse cursor in the recording

settings-modules-recorder-start-delay-ms = Start Delay
    .description = Delay between choosing the source and recording, so the start toast clears the screen first

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


## RecorderFormat variants
enum-recorder-format-mp4 = MP4
enum-recorder-format-mkv = MKV
enum-recorder-format-webm = WebM
