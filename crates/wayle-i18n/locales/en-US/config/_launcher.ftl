# Launcher (rofi replacement)

settings-launcher-location = Position
    .description = Where the launcher appears on screen

settings-launcher-width = Width
    .description = Surface width — a multiplier of the default (1.0) or pixels (e.g. "800px")

settings-launcher-lines = Lines
    .description = Visible result lines

settings-launcher-monitor = Monitor
    .description = Output connector to show on (empty = focused output)

settings-launcher-modes = Modes
    .description = Enabled modes, in tab order (drun, run, window, ssh, filebrowser, combi, keys, or a script name)

settings-launcher-cycle = Cycle Selection
    .description = Wrap the selection at list edges

settings-launcher-matching = Matching Method
    .description = How typed text is matched against entries

settings-launcher-tokenize = Tokenize
    .description = Match each word of the query independently

settings-launcher-negate-char = Negation Character
    .description = Token prefix that excludes matches (rofi -matching-negate-char)

settings-launcher-normalize-match = Normalize Match
    .description = Ignore accents and Unicode variants while matching

settings-launcher-sort = Sort Results
    .description = Rank results by match quality instead of list order

settings-launcher-sorting-method = Sorting Method
    .description = Ranking algorithm used when sorting is enabled

settings-launcher-case = Case Sensitivity
    .description = How letter case affects matching

settings-launcher-terminal = Terminal
    .description = Terminal emulator for terminal apps (empty = autodetect)

settings-launcher-show-icons = Show Icons
    .description = Show icons beside entries

settings-launcher-icon-theme = Icon Theme
    .description = Icon theme override (empty = system theme)

settings-launcher-sidebar-mode = Sidebar Mode
    .description = Show mode tabs at the bottom of the launcher

settings-launcher-auto-select = Auto Select
    .description = Accept automatically when exactly one result remains

settings-launcher-hover-select = Hover Select
    .description = Select the row under the mouse cursor

settings-launcher-fixed-num-lines = Fixed Height
    .description = Keep the list height fixed at the configured line count

settings-launcher-display-names = Mode Display Names
    .description = Per-mode display-name overrides (e.g. drun = "apps")

settings-launcher-scripts = Script Modes
    .description = Custom script modes: name → executable (rofi script protocol)

settings-launcher-keybindings = Keybindings
    .description = Keybinding overrides: action → comma-separated keys (unset actions keep rofi defaults)

settings-launcher-history = History
    .description = Launch history and frecency ranking

settings-launcher-drun = Applications
    .description = drun (application) mode

settings-launcher-run = Commands
    .description = run (command) mode

settings-launcher-window = Windows
    .description = Window switcher mode

settings-launcher-ssh = SSH
    .description = SSH host mode

settings-launcher-filebrowser = File Browser
    .description = File browser mode

settings-launcher-combi = Combi
    .description = Combined-modes mode

settings-launcher-history-enable = Enable History
    .description = Record launches and rank frequently used entries first

settings-launcher-history-max-size = History Size
    .description = Maximum remembered entries per mode

settings-launcher-drun-categories = Categories
    .description = Only show applications within these categories (empty = all)

settings-launcher-drun-exclude-categories = Excluded Categories
    .description = Hide applications within these categories

settings-launcher-drun-match-fields = Match Fields
    .description = Desktop-entry fields searched while typing

settings-launcher-drun-display-format = Display Format
    .description = Row template: {"{name}"}, {"{generic}"}, {"{exec}"}, {"{categories}"}, {"{comment}"}; [..] renders only when filled

settings-launcher-drun-show-actions = Show Actions
    .description = Expose desktop-file actions as extra rows

settings-launcher-drun-url-launcher = URL Launcher
    .description = Command opening Type=Link desktop entries

settings-launcher-run-run-command = Run Command
    .description = Plain accept template ({"{cmd}"})

settings-launcher-run-shell-command = Terminal Command
    .description = Run-in-terminal template ({"{terminal}"}, {"{cmd}"})

settings-launcher-run-list-command = List Command
    .description = Extra command whose stdout lines add entries

settings-launcher-window-format = Display Format
    .description = Row template: {"{w}"} workspace, {"{c}"} class, {"{t}"} title, {"{n}"} name, {"{r}"} role

settings-launcher-window-match-fields = Match Fields
    .description = Window fields searched while typing

settings-launcher-window-hide-active = Hide Active Window
    .description = Hide the currently focused window from the list

settings-launcher-window-close-on-delete = Close On Delete
    .description = Shift-Delete closes the selected window

settings-launcher-ssh-client = SSH Client
    .description = SSH client binary

settings-launcher-ssh-command = Connect Command
    .description = Connect template ({"{terminal}"}, {"{ssh-client}"}, {"{host}"})

settings-launcher-ssh-parse-hosts = Parse /etc/hosts
    .description = Include hosts from /etc/hosts

settings-launcher-ssh-parse-known-hosts = Parse known_hosts
    .description = Include hosts from ~/.ssh/known_hosts

settings-launcher-filebrowser-directory = Start Directory
    .description = Directory the browser opens in (empty = home)

settings-launcher-filebrowser-sorting-method = Sorting
    .description = File ordering

settings-launcher-filebrowser-directories-first = Directories First
    .description = List directories before files

settings-launcher-filebrowser-show-hidden = Show Hidden
    .description = Show hidden files

settings-launcher-filebrowser-command = Open Command
    .description = Command opening the picked file (empty = xdg-open)

settings-launcher-combi-modes = Combined Modes
    .description = Modes merged into the combined list

settings-launcher-combi-display-format = Display Format
    .description = Row template ({"{mode}"}, {"{text}"})

## LauncherLocation variants
enum-launcher-location-center = Center
enum-launcher-location-north-west = Top Left
enum-launcher-location-north = Top
enum-launcher-location-north-east = Top Right
enum-launcher-location-east = Right
enum-launcher-location-south-east = Bottom Right
enum-launcher-location-south = Bottom
enum-launcher-location-south-west = Bottom Left
enum-launcher-location-west = Left

## LauncherMatching variants
enum-launcher-matching-normal = Normal
enum-launcher-matching-regex = Regex
enum-launcher-matching-glob = Glob
enum-launcher-matching-fuzzy = Fuzzy
enum-launcher-matching-prefix = Prefix

## LauncherSorting variants
enum-launcher-sorting-levenshtein = Levenshtein
enum-launcher-sorting-fzf = FZF

## LauncherCase variants
enum-launcher-case-insensitive = Insensitive
enum-launcher-case-smart = Smart
enum-launcher-case-sensitive = Sensitive

## LauncherDrunField variants
enum-launcher-drun-field-name = Name
enum-launcher-drun-field-generic = Generic Name
enum-launcher-drun-field-exec = Command
enum-launcher-drun-field-categories = Categories
enum-launcher-drun-field-comment = Comment
enum-launcher-drun-field-keywords = Keywords

## LauncherWindowField variants
enum-launcher-window-field-title = Title
enum-launcher-window-field-class = Class
enum-launcher-window-field-name = Name
enum-launcher-window-field-role = Role
enum-launcher-window-field-desktop = Workspace

## LauncherFileSort variants
enum-launcher-file-sort-name = Name
enum-launcher-file-sort-mtime = Modified
enum-launcher-file-sort-atime = Accessed
enum-launcher-file-sort-ctime = Created

## Animations
settings-animations-launcher = Launcher
    .description = Enter/exit animation override for the application launcher
