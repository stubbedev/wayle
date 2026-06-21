

settings-lock-enabled = Enabled
    .description = Let Wayle lock the session (responds to loginctl lock-session and `wayle lock`)

settings-lock-background-mode = Background
    .description = How the lock screen background is drawn: solid color, an image, or the wallpaper

settings-lock-background-image = Background Image
    .description = Image file shown behind the lock screen when background is set to "image"

settings-lock-background-color = Background Color
    .description = Fill color shown behind the lock screen when background is set to "color"

settings-lock-blur = Blur
    .description = Gaussian blur radius applied to image/wallpaper backgrounds (0 = none)

settings-lock-show-clock = Show Clock
    .description = Display a clock on the lock screen

settings-lock-clock-format = Clock Format
    .description = strftime format for the lock-screen time (e.g. %H:%M)

settings-lock-date-format = Date Format
    .description = strftime format for the lock-screen date (e.g. %A, %B %-d)

settings-lock-grace-period-ms = Grace Period
    .description = Window after locking during which the screen unlocks without a password (ms, 0 = always require it)

settings-lock-max-attempts = Max Attempts
    .description = Failed password attempts before input is blocked (0 = unlimited). Screen stays locked.

settings-lock-show-failed-attempts = Show Failed Attempts
    .description = Show the failed-attempt count on the lock screen

settings-lock-blank-timeout-ms = Blank Timeout
    .description = Turn displays off after this idle time on the lock screen (ms, 0 = never)

settings-lock-pam-service = PAM Service
    .description = PAM service used to verify the password (e.g. system-auth, login)


## LockBackground variants
enum-lock-background-color = Color
enum-lock-background-image = Image
enum-lock-background-wallpaper = Wallpaper
