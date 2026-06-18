### Wayle Configuration - Mail Module

## Mail Module Configuration

settings-modules-mail-format = Format
    .description = Label format string. Placeholders: {"{{ count }}"}

settings-modules-mail-query = Query
    .description = notmuch search query whose match count is shown (e.g. tag:unread)

settings-modules-mail-hide-when-zero = Hide When Zero
    .description = Hide the module entirely while the count is zero

settings-modules-mail-notify = Notify on New Mail
    .description = Fire a desktop notification when the unread count rises

settings-modules-mail-notify-summary = Notification Summary
    .description = Summary text. Placeholders: {"{{ count }}"} (total), {"{{ new }}"} (new)

settings-modules-mail-notify-body = Notification Body
    .description = Body text. Placeholders: {"{{ count }}"} (total), {"{{ new }}"} (new)

settings-modules-mail-icon-name = Icon
    .description = Module icon

settings-modules-mail-border-show = Show Border
    .description = Display border around button

settings-modules-mail-border-color = Border Color
    .description = Border color token

settings-modules-mail-icon-show = Show Icon
    .description = Display module icon

settings-modules-mail-icon-color = Icon Color
    .description = Icon foreground color

settings-modules-mail-icon-bg-color = Icon Background
    .description = Icon container background color

settings-modules-mail-label-show = Show Label
    .description = Display label

settings-modules-mail-label-color = Label Color
    .description = Label text color

settings-modules-mail-label-max-length = Label Max Length
    .description = Max characters before truncation

settings-modules-mail-button-bg-color = Button Background
    .description = Button background color

settings-modules-mail-left-click = Left Click
    .description = Action on left click. Empty for no action, or a shell command (e.g. your mail client)

settings-modules-mail-right-click = Right Click
    .description = Shell command on right click

settings-modules-mail-middle-click = Middle Click
    .description = Shell command on middle click

settings-modules-mail-scroll-up = Scroll Up
    .description = Shell command on scroll up

settings-modules-mail-scroll-down = Scroll Down
    .description = Shell command on scroll down
