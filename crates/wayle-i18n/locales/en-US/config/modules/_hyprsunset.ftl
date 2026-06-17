### Wayle Configuration - Hyprsunset Module

## Hyprsunset Module Configuration

settings-modules-hyprsunset-temperature = Temperature
    .description = Color temperature in Kelvin when filter is enabled (1000-20000)

settings-modules-hyprsunset-gamma = Gamma
    .description = Display gamma percentage when filter is enabled (0-200)

settings-modules-hyprsunset-auto-schedule = Auto Schedule
    .description = Automatically enable at night and disable during the day, based on local sunrise/sunset. Location is detected via GeoClue, falling back to the coordinates below

settings-modules-hyprsunset-latitude = Latitude
    .description = Fallback latitude in decimal degrees (north positive, -90 to 90), used when GeoClue is unavailable

settings-modules-hyprsunset-longitude = Longitude
    .description = Fallback longitude in decimal degrees (east positive, -180 to 180), used when GeoClue is unavailable

settings-modules-hyprsunset-icon-off = Icon Off
    .description = Icon when filter is disabled

settings-modules-hyprsunset-icon-on = Icon On
    .description = Icon when filter is enabled

settings-modules-hyprsunset-format = Format
    .description = Label format string. Placeholders: {"{{ status }}"}, {"{{ temp }}"}, {"{{ gamma }}"}, {"{{ config_temp }}"}, {"{{ config_gamma }}"}

settings-modules-hyprsunset-border-show = Show Border
    .description = Display border around button

settings-modules-hyprsunset-border-color = Border Color
    .description = Border color token

settings-modules-hyprsunset-icon-show = Show Icon
    .description = Display module icon

settings-modules-hyprsunset-icon-color = Icon Color
    .description = Icon foreground color

settings-modules-hyprsunset-icon-bg-color = Icon Background
    .description = Icon container background color

settings-modules-hyprsunset-label-show = Show Label
    .description = Display label

settings-modules-hyprsunset-label-color = Label Color
    .description = Label text color

settings-modules-hyprsunset-label-max-length = Label Max Length
    .description = Max characters before truncation

settings-modules-hyprsunset-button-bg-color = Button Background
    .description = Button background color

settings-modules-hyprsunset-left-click = Left Click
    .description = Action on left click. Use `:toggle` for built-in on/off, empty for no action, or a shell command

settings-modules-hyprsunset-right-click = Right Click
    .description = Shell command on right click

settings-modules-hyprsunset-middle-click = Middle Click
    .description = Shell command on middle click

settings-modules-hyprsunset-scroll-up = Scroll Up
    .description = Shell command on scroll up

settings-modules-hyprsunset-scroll-down = Scroll Down
    .description = Shell command on scroll down
