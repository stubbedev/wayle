#compdef wayle

autoload -U is-at-least

_wayle() {
    typeset -A opt_args
    typeset -a _arguments_options
    local ret=1

    if is-at-least 5.2; then
        _arguments_options=(-s -S -C)
    else
        _arguments_options=(-s -C)
    fi

    local context curcontext="$curcontext" state line
    _arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'-V[Print version]' \
'--version[Print version]' \
":: :_wayle_commands" \
"*::: :->wayle" \
&& ret=0
    case $state in
    (wayle)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-command-$line[1]:"
        case $line[1] in
            (audio)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__audio_commands" \
"*::: :->audio" \
&& ret=0

    case $state in
    (audio)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-audio-command-$line[1]:"
        case $line[1] in
            (output-volume)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::level -- Volume level (0-100) or relative adjustment (+5, -10):_default' \
&& ret=0
;;
(output-mute)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(input-volume)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::level -- Volume level (0-100) or relative adjustment (+5, -10):_default' \
&& ret=0
;;
(input-mute)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(sinks)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(sources)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__audio__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-audio-help-command-$line[1]:"
        case $line[1] in
            (output-volume)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(output-mute)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(input-volume)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(input-mute)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(sinks)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(sources)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(config)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__config_commands" \
"*::: :->config" \
&& ret=0

    case $state in
    (config)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-config-command-$line[1]:"
        case $line[1] in
            (get)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':path -- The configuration path to retrieve (e.g., "modules.battery.enabled"):_default' \
&& ret=0
;;
(set)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':path -- The configuration path to set (e.g., "modules.battery.enabled"):_default' \
':value -- The value to set (use JSON format for complex types):_default' \
&& ret=0
;;
(reset)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':path -- The configuration path to reset (e.g., "bar.button_gap"):_default' \
&& ret=0
;;
(schema)
_arguments "${_arguments_options[@]}" : \
'--stdout[Print to stdout instead of writing to config directory]' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(default)
_arguments "${_arguments_options[@]}" : \
'--stdout[Print to stdout instead of writing config.toml.example]' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(docs)
_arguments "${_arguments_options[@]}" : \
'--out=[Output directory for the generated pages]:OUT:_files' \
'--only=[Regenerate only the named module]:ONLY:_default' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__config__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-config-help-command-$line[1]:"
        case $line[1] in
            (get)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(set)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(reset)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(schema)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(default)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(docs)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(icons)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__icons_commands" \
"*::: :->icons" \
&& ret=0

    case $state in
    (icons)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-icons-command-$line[1]:"
        case $line[1] in
            (setup)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(install)
_arguments "${_arguments_options[@]}" : \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
':source -- Source name (run '\''wayle icons sources'\'' to see available sources):_default' \
'*::slugs -- Icon slugs to install (e.g., home settings bell):_default' \
&& ret=0
;;
(import)
_arguments "${_arguments_options[@]}" : \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
':path -- Path to SVG file or directory:_files' \
'::name -- Icon name (required for single file, ignored for directory):_default' \
&& ret=0
;;
(remove)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'*::names -- Icon names to remove (e.g., tb-home-symbolic si-firefox-symbolic):_default' \
&& ret=0
;;
(sources)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(list)
_arguments "${_arguments_options[@]}" : \
'-s+[Filter by source prefix (e.g., tb, si, md)]:SOURCE:_default' \
'--source=[Filter by source prefix (e.g., tb, si, md)]:SOURCE:_default' \
'-i[Interactive fuzzy search (requires fzf)]' \
'--interactive[Interactive fuzzy search (requires fzf)]' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(open)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(export)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':destination -- Destination directory for exported icons:_files' \
&& ret=0
;;
(sync)
_arguments "${_arguments_options[@]}" : \
'--dry-run[Preview what would be installed without making changes]' \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__icons__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-icons-help-command-$line[1]:"
        case $line[1] in
            (setup)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(install)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(import)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(remove)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(sources)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(open)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(export)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(sync)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(media)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__media_commands" \
"*::: :->media" \
&& ret=0

    case $state in
    (media)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-media-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(play-pause)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::player -- Player identifier (number or partial name match):_default' \
&& ret=0
;;
(next)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::player -- Player identifier (number or partial name match):_default' \
&& ret=0
;;
(previous)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::player -- Player identifier (number or partial name match):_default' \
&& ret=0
;;
(shuffle)
_arguments "${_arguments_options[@]}" : \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
'::state -- Shuffle state:((on\:"Enable shuffle"
off\:"Disable shuffle"
toggle\:"Toggle shuffle state"))' \
'::player -- Player identifier (number or partial name match):_default' \
&& ret=0
;;
(loop)
_arguments "${_arguments_options[@]}" : \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
':mode -- Loop mode:((none\:"No looping"
track\:"Loop current track"
playlist\:"Loop entire playlist"))' \
'::player -- Player identifier (number or partial name match):_default' \
&& ret=0
;;
(active)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::player -- Player to set as active (number or partial name match):_default' \
&& ret=0
;;
(info)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::player -- Player identifier (number or partial name match):_default' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__media__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-media-help-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(play-pause)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(next)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(previous)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(shuffle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(loop)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(active)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(info)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(notify)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__notify_commands" \
"*::: :->notify" \
&& ret=0

    case $state in
    (notify)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-notify-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(dismiss)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':id -- Notification ID to dismiss:_default' \
&& ret=0
;;
(dismiss-all)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(dnd)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__notify__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-notify-help-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dismiss)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dismiss-all)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dnd)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(panel)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__panel_commands" \
"*::: :->panel" \
&& ret=0

    case $state in
    (panel)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-panel-command-$line[1]:"
        case $line[1] in
            (start)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(restart)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(settings)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(inspect)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(hide)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::monitor -- Monitor connector name (e.g., "DP-1"). Omit to hide all:_default' \
&& ret=0
;;
(show)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::monitor -- Monitor connector name (e.g., "DP-1"). Omit to show all:_default' \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::monitor -- Monitor connector name (e.g., "DP-1"). Omit to toggle all:_default' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__panel__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-panel-help-command-$line[1]:"
        case $line[1] in
            (start)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(restart)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(settings)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(inspect)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(hide)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(show)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(power)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__power_commands" \
"*::: :->power" \
&& ret=0

    case $state in
    (power)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-power-command-$line[1]:"
        case $line[1] in
            (status)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(set)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':profile -- Profile name (power-saver, balanced, performance):_default' \
&& ret=0
;;
(cycle)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(list)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__power__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-power-help-command-$line[1]:"
        case $line[1] in
            (status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(set)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(cycle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(systray)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__systray_commands" \
"*::: :->systray" \
&& ret=0

    case $state in
    (systray)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-systray-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(activate)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':id -- Tray item ID to activate:_default' \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__systray__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-systray-help-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(activate)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(wallpaper)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__wallpaper_commands" \
"*::: :->wallpaper" \
&& ret=0

    case $state in
    (wallpaper)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-wallpaper-command-$line[1]:"
        case $line[1] in
            (set)
_arguments "${_arguments_options[@]}" : \
'-f+[Image fit mode]:FIT:((fill\:"Scale to cover entire display"
fit\:"Scale to fit within display"
center\:"Display at original size, centered"
tile\:"Tile the image"
stretch\:"Stretch to fill"))' \
'--fit=[Image fit mode]:FIT:((fill\:"Scale to cover entire display"
fit\:"Scale to fit within display"
center\:"Display at original size, centered"
tile\:"Tile the image"
stretch\:"Stretch to fill"))' \
'--monitor=[Target monitor (e.g., DP-1, HDMI-A-1). If omitted, applies to all monitors]:MONITOR:_default' \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
':path -- Path to wallpaper image:_files' \
&& ret=0
;;
(cycle)
_arguments "${_arguments_options[@]}" : \
'-i+[Interval in seconds between changes]:INTERVAL:_default' \
'--interval=[Interval in seconds between changes]:INTERVAL:_default' \
'-m+[Cycling mode]:MODE:((sequential\:"Cycle in alphabetical order"
shuffle\:"Cycle in random order"))' \
'--mode=[Cycling mode]:MODE:((sequential\:"Cycle in alphabetical order"
shuffle\:"Cycle in random order"))' \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
':directory -- Directory containing wallpaper images:_files' \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(next)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(previous)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(info)
_arguments "${_arguments_options[@]}" : \
'--monitor=[Target monitor (e.g., DP-1, HDMI-A-1). If omitted, shows global state]:MONITOR:_default' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(theming-monitor)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':monitor -- Monitor connector name (e.g., DP-1). Use empty string for default:_default' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__wallpaper__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-wallpaper-help-command-$line[1]:"
        case $line[1] in
            (set)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(cycle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(next)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(previous)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(info)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(theming-monitor)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(idle)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__idle_commands" \
"*::: :->idle" \
&& ret=0

    case $state in
    (idle)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-idle-command-$line[1]:"
        case $line[1] in
            (on)
_arguments "${_arguments_options[@]}" : \
'-i[Force indefinite mode (ignore default duration)]' \
'--indefinite[Force indefinite mode (ignore default duration)]' \
'-h[Print help]' \
'--help[Print help]' \
'::minutes -- Duration in minutes (omit to use default duration):_default' \
&& ret=0
;;
(off)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(duration)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':value -- +N to add, -N to subtract, N to set absolute:_default' \
&& ret=0
;;
(remaining)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':value -- +N to add, -N to subtract, N to set absolute:_default' \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
'-i[Use indefinite mode when enabling]' \
'--indefinite[Use indefinite mode when enabling]' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__idle__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-idle-help-command-$line[1]:"
        case $line[1] in
            (on)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(off)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(duration)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(remaining)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(launcher)
_arguments "${_arguments_options[@]}" : \
'*::args -- Raw rofi-style args (single-dash long flags, e.g. `-show drun`):_default' \
&& ret=0
;;
(lock)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(vpn)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__vpn_commands" \
"*::: :->vpn" \
&& ret=0

    case $state in
    (vpn)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-vpn-command-$line[1]:"
        case $line[1] in
            (sso-callback)
_arguments "${_arguments_options[@]}" : \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
':uri -- The `globalprotectcallback\:` URI the browser was sent to:_default' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__vpn__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-vpn-help-command-$line[1]:"
        case $line[1] in
            (sso-callback)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(recorder)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__recorder_commands" \
"*::: :->recorder" \
&& ret=0

    case $state in
    (recorder)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-recorder-command-$line[1]:"
        case $line[1] in
            (start)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(pause)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(resume)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__recorder__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-recorder-help-command-$line[1]:"
        case $line[1] in
            (start)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(pause)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(resume)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(screenshot)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__screenshot_commands" \
"*::: :->screenshot" \
&& ret=0

    case $state in
    (screenshot)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-screenshot-command-$line[1]:"
        case $line[1] in
            (region)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(output)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
'::name -- Output connector name (e.g. DP-1):_default' \
&& ret=0
;;
(window)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__screenshot__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-screenshot-help-command-$line[1]:"
        case $line[1] in
            (region)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(output)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(window)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(widget)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__widget_commands" \
"*::: :->widget" \
&& ret=0

    case $state in
    (widget)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-widget-command-$line[1]:"
        case $line[1] in
            (update)
_arguments "${_arguments_options[@]}" : \
'-h[Print help (see more with '\''--help'\'')]' \
'--help[Print help (see more with '\''--help'\'')]' \
':id -- Config id of the target widget (e.g. a custom module'\''s `id`):_default' \
':output -- Output payload (plain text, or a JSON object):_default' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__widget__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-widget-help-command-$line[1]:"
        case $line[1] in
            (update)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(toast)
_arguments "${_arguments_options[@]}" : \
'--icon=[Icon name shown beside the text]:ICON:_default' \
'--percentage=[Progress percentage (0-100); shows a progress bar when set]:PERCENTAGE:_default' \
'--duration=[Auto-dismiss duration in milliseconds (toast config default when unset)]:DURATION:_default' \
'--preset=[Preset id from \`\[\[toasts.presets\]\]\` to base this toast on]:PRESET:_default' \
'--class=[Extra CSS class applied to the toast for custom styling]:CLASS:_default' \
'-h[Print help]' \
'--help[Print help]' \
'::label -- Toast text. Optional when `--preset` supplies one:_default' \
&& ret=0
;;
(portal)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__portal_commands" \
"*::: :->portal" \
&& ret=0

    case $state in
    (portal)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-portal-command-$line[1]:"
        case $line[1] in
            (run)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(share-picker)
_arguments "${_arguments_options[@]}" : \
'--allow-token[Pre-check the "allow restore token" box]' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(show)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
":: :_wayle__portal__show_commands" \
"*::: :->show" \
&& ret=0

    case $state in
    (show)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-portal-show-command-$line[1]:"
        case $line[1] in
            (file-chooser)
_arguments "${_arguments_options[@]}" : \
'--save[Show the save dialog instead of the open dialog]' \
'--multiple[Allow selecting multiple files (open only)]' \
'--directory[Select a directory instead of files (open only)]' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(screenshot)
_arguments "${_arguments_options[@]}" : \
'--mode=[Capture mode\: \`region\`, \`output\`, \`screen\`, or \`window\`]:MODE:_default' \
'--target=[Output connector name (used by \`output\` mode)]:TARGET:_default' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(color)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(print)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(screen-cast)
_arguments "${_arguments_options[@]}" : \
'--allow-token[Pre-check the "allow restore token" box]' \
'--multiple[Allow selecting multiple sources]' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(access)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(account)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(app-chooser)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(dynamic-launcher)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(wallpaper)
_arguments "${_arguments_options[@]}" : \
'--uri=[\`file\://\` image URI to preview (defaults to a placeholder)]:URI:_default' \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__portal__show__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-portal-show-help-command-$line[1]:"
        case $line[1] in
            (file-chooser)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(screenshot)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(color)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(print)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(screen-cast)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(access)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(account)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(app-chooser)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dynamic-launcher)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(wallpaper)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__portal__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-portal-help-command-$line[1]:"
        case $line[1] in
            (run)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(share-picker)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(show)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__portal__help__show_commands" \
"*::: :->show" \
&& ret=0

    case $state in
    (show)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-portal-help-show-command-$line[1]:"
        case $line[1] in
            (file-chooser)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(screenshot)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(color)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(print)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(screen-cast)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(access)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(account)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(app-chooser)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dynamic-launcher)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(wallpaper)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(shell)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
&& ret=0
;;
(completions)
_arguments "${_arguments_options[@]}" : \
'-h[Print help]' \
'--help[Print help]' \
':shell -- Shell to generate completions for:(bash elvish fish powershell zsh)' \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help_commands" \
"*::: :->help" \
&& ret=0

    case $state in
    (help)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-command-$line[1]:"
        case $line[1] in
            (audio)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__audio_commands" \
"*::: :->audio" \
&& ret=0

    case $state in
    (audio)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-audio-command-$line[1]:"
        case $line[1] in
            (output-volume)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(output-mute)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(input-volume)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(input-mute)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(sinks)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(sources)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(config)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__config_commands" \
"*::: :->config" \
&& ret=0

    case $state in
    (config)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-config-command-$line[1]:"
        case $line[1] in
            (get)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(set)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(reset)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(schema)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(default)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(docs)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(icons)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__icons_commands" \
"*::: :->icons" \
&& ret=0

    case $state in
    (icons)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-icons-command-$line[1]:"
        case $line[1] in
            (setup)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(install)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(import)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(remove)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(sources)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(open)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(export)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(sync)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(media)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__media_commands" \
"*::: :->media" \
&& ret=0

    case $state in
    (media)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-media-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(play-pause)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(next)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(previous)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(shuffle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(loop)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(active)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(info)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(notify)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__notify_commands" \
"*::: :->notify" \
&& ret=0

    case $state in
    (notify)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-notify-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dismiss)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dismiss-all)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dnd)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(panel)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__panel_commands" \
"*::: :->panel" \
&& ret=0

    case $state in
    (panel)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-panel-command-$line[1]:"
        case $line[1] in
            (start)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(restart)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(settings)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(inspect)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(hide)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(show)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(power)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__power_commands" \
"*::: :->power" \
&& ret=0

    case $state in
    (power)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-power-command-$line[1]:"
        case $line[1] in
            (status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(set)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(cycle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(systray)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__systray_commands" \
"*::: :->systray" \
&& ret=0

    case $state in
    (systray)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-systray-command-$line[1]:"
        case $line[1] in
            (list)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(activate)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(wallpaper)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__wallpaper_commands" \
"*::: :->wallpaper" \
&& ret=0

    case $state in
    (wallpaper)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-wallpaper-command-$line[1]:"
        case $line[1] in
            (set)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(cycle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(next)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(previous)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(info)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(theming-monitor)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(idle)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__idle_commands" \
"*::: :->idle" \
&& ret=0

    case $state in
    (idle)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-idle-command-$line[1]:"
        case $line[1] in
            (on)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(off)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(duration)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(remaining)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(launcher)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(lock)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(vpn)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__vpn_commands" \
"*::: :->vpn" \
&& ret=0

    case $state in
    (vpn)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-vpn-command-$line[1]:"
        case $line[1] in
            (sso-callback)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(recorder)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__recorder_commands" \
"*::: :->recorder" \
&& ret=0

    case $state in
    (recorder)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-recorder-command-$line[1]:"
        case $line[1] in
            (start)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(stop)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(toggle)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(pause)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(resume)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(status)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(screenshot)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__screenshot_commands" \
"*::: :->screenshot" \
&& ret=0

    case $state in
    (screenshot)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-screenshot-command-$line[1]:"
        case $line[1] in
            (region)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(output)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(window)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(widget)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__widget_commands" \
"*::: :->widget" \
&& ret=0

    case $state in
    (widget)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-widget-command-$line[1]:"
        case $line[1] in
            (update)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
(toast)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(portal)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__portal_commands" \
"*::: :->portal" \
&& ret=0

    case $state in
    (portal)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-portal-command-$line[1]:"
        case $line[1] in
            (run)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(share-picker)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(show)
_arguments "${_arguments_options[@]}" : \
":: :_wayle__help__portal__show_commands" \
"*::: :->show" \
&& ret=0

    case $state in
    (show)
        words=($line[1] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:wayle-help-portal-show-command-$line[1]:"
        case $line[1] in
            (file-chooser)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(screenshot)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(color)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(print)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(screen-cast)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(access)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(account)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(app-chooser)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(dynamic-launcher)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(wallpaper)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
;;
(shell)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(completions)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
(help)
_arguments "${_arguments_options[@]}" : \
&& ret=0
;;
        esac
    ;;
esac
;;
        esac
    ;;
esac
}

(( $+functions[_wayle_commands] )) ||
_wayle_commands() {
    local commands; commands=(
'audio:Audio control commands' \
'config:Configuration management commands' \
'icons:Icon management commands' \
'media:Media player control commands' \
'notify:Notification control commands' \
'panel:Panel management commands' \
'power:Power profile commands' \
'systray:System tray commands' \
'wallpaper:Wallpaper control commands' \
'idle:Idle inhibit control commands' \
'launcher:Application launcher / dmenu with rofi-compatible flags' \
'lock:Lock the session via Wayle'\''s lock screen' \
'vpn:VPN commands' \
'recorder:Screen recorder control commands' \
'screenshot:Screenshot capture commands' \
'widget:Widget control commands' \
'toast:Show a custom on-screen toast' \
'portal:xdg-desktop-portal backend, screencast picker stub, and dialog previewer' \
'shell:Run the desktop shell in the foreground' \
'completions:Generate shell completions' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle commands' commands "$@"
}
(( $+functions[_wayle__audio_commands] )) ||
_wayle__audio_commands() {
    local commands; commands=(
'output-volume:Get or set output volume level' \
'output-mute:Toggle output mute state' \
'input-volume:Get or set input volume level' \
'input-mute:Toggle input mute state' \
'sinks:List available audio sinks (outputs)' \
'sources:List available audio sources (inputs)' \
'status:Show current audio status' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle audio commands' commands "$@"
}
(( $+functions[_wayle__audio__help_commands] )) ||
_wayle__audio__help_commands() {
    local commands; commands=(
'output-volume:Get or set output volume level' \
'output-mute:Toggle output mute state' \
'input-volume:Get or set input volume level' \
'input-mute:Toggle input mute state' \
'sinks:List available audio sinks (outputs)' \
'sources:List available audio sources (inputs)' \
'status:Show current audio status' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle audio help commands' commands "$@"
}
(( $+functions[_wayle__audio__help__help_commands] )) ||
_wayle__audio__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio help help commands' commands "$@"
}
(( $+functions[_wayle__audio__help__input-mute_commands] )) ||
_wayle__audio__help__input-mute_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio help input-mute commands' commands "$@"
}
(( $+functions[_wayle__audio__help__input-volume_commands] )) ||
_wayle__audio__help__input-volume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio help input-volume commands' commands "$@"
}
(( $+functions[_wayle__audio__help__output-mute_commands] )) ||
_wayle__audio__help__output-mute_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio help output-mute commands' commands "$@"
}
(( $+functions[_wayle__audio__help__output-volume_commands] )) ||
_wayle__audio__help__output-volume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio help output-volume commands' commands "$@"
}
(( $+functions[_wayle__audio__help__sinks_commands] )) ||
_wayle__audio__help__sinks_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio help sinks commands' commands "$@"
}
(( $+functions[_wayle__audio__help__sources_commands] )) ||
_wayle__audio__help__sources_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio help sources commands' commands "$@"
}
(( $+functions[_wayle__audio__help__status_commands] )) ||
_wayle__audio__help__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio help status commands' commands "$@"
}
(( $+functions[_wayle__audio__input-mute_commands] )) ||
_wayle__audio__input-mute_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio input-mute commands' commands "$@"
}
(( $+functions[_wayle__audio__input-volume_commands] )) ||
_wayle__audio__input-volume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio input-volume commands' commands "$@"
}
(( $+functions[_wayle__audio__output-mute_commands] )) ||
_wayle__audio__output-mute_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio output-mute commands' commands "$@"
}
(( $+functions[_wayle__audio__output-volume_commands] )) ||
_wayle__audio__output-volume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio output-volume commands' commands "$@"
}
(( $+functions[_wayle__audio__sinks_commands] )) ||
_wayle__audio__sinks_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio sinks commands' commands "$@"
}
(( $+functions[_wayle__audio__sources_commands] )) ||
_wayle__audio__sources_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio sources commands' commands "$@"
}
(( $+functions[_wayle__audio__status_commands] )) ||
_wayle__audio__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle audio status commands' commands "$@"
}
(( $+functions[_wayle__completions_commands] )) ||
_wayle__completions_commands() {
    local commands; commands=()
    _describe -t commands 'wayle completions commands' commands "$@"
}
(( $+functions[_wayle__config_commands] )) ||
_wayle__config_commands() {
    local commands; commands=(
'get:Get the value of a configuration path' \
'set:Set the value of a configuration path' \
'reset:Reset a configuration path to its default value' \
'schema:Output JSON Schema for the configuration (for editor intellisense)' \
'default:Output the default configuration as TOML' \
'docs:Generate markdown reference pages for every registered schema' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle config commands' commands "$@"
}
(( $+functions[_wayle__config__default_commands] )) ||
_wayle__config__default_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config default commands' commands "$@"
}
(( $+functions[_wayle__config__docs_commands] )) ||
_wayle__config__docs_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config docs commands' commands "$@"
}
(( $+functions[_wayle__config__get_commands] )) ||
_wayle__config__get_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config get commands' commands "$@"
}
(( $+functions[_wayle__config__help_commands] )) ||
_wayle__config__help_commands() {
    local commands; commands=(
'get:Get the value of a configuration path' \
'set:Set the value of a configuration path' \
'reset:Reset a configuration path to its default value' \
'schema:Output JSON Schema for the configuration (for editor intellisense)' \
'default:Output the default configuration as TOML' \
'docs:Generate markdown reference pages for every registered schema' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle config help commands' commands "$@"
}
(( $+functions[_wayle__config__help__default_commands] )) ||
_wayle__config__help__default_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config help default commands' commands "$@"
}
(( $+functions[_wayle__config__help__docs_commands] )) ||
_wayle__config__help__docs_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config help docs commands' commands "$@"
}
(( $+functions[_wayle__config__help__get_commands] )) ||
_wayle__config__help__get_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config help get commands' commands "$@"
}
(( $+functions[_wayle__config__help__help_commands] )) ||
_wayle__config__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config help help commands' commands "$@"
}
(( $+functions[_wayle__config__help__reset_commands] )) ||
_wayle__config__help__reset_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config help reset commands' commands "$@"
}
(( $+functions[_wayle__config__help__schema_commands] )) ||
_wayle__config__help__schema_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config help schema commands' commands "$@"
}
(( $+functions[_wayle__config__help__set_commands] )) ||
_wayle__config__help__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config help set commands' commands "$@"
}
(( $+functions[_wayle__config__reset_commands] )) ||
_wayle__config__reset_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config reset commands' commands "$@"
}
(( $+functions[_wayle__config__schema_commands] )) ||
_wayle__config__schema_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config schema commands' commands "$@"
}
(( $+functions[_wayle__config__set_commands] )) ||
_wayle__config__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle config set commands' commands "$@"
}
(( $+functions[_wayle__help_commands] )) ||
_wayle__help_commands() {
    local commands; commands=(
'audio:Audio control commands' \
'config:Configuration management commands' \
'icons:Icon management commands' \
'media:Media player control commands' \
'notify:Notification control commands' \
'panel:Panel management commands' \
'power:Power profile commands' \
'systray:System tray commands' \
'wallpaper:Wallpaper control commands' \
'idle:Idle inhibit control commands' \
'launcher:Application launcher / dmenu with rofi-compatible flags' \
'lock:Lock the session via Wayle'\''s lock screen' \
'vpn:VPN commands' \
'recorder:Screen recorder control commands' \
'screenshot:Screenshot capture commands' \
'widget:Widget control commands' \
'toast:Show a custom on-screen toast' \
'portal:xdg-desktop-portal backend, screencast picker stub, and dialog previewer' \
'shell:Run the desktop shell in the foreground' \
'completions:Generate shell completions' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle help commands' commands "$@"
}
(( $+functions[_wayle__help__audio_commands] )) ||
_wayle__help__audio_commands() {
    local commands; commands=(
'output-volume:Get or set output volume level' \
'output-mute:Toggle output mute state' \
'input-volume:Get or set input volume level' \
'input-mute:Toggle input mute state' \
'sinks:List available audio sinks (outputs)' \
'sources:List available audio sources (inputs)' \
'status:Show current audio status' \
    )
    _describe -t commands 'wayle help audio commands' commands "$@"
}
(( $+functions[_wayle__help__audio__input-mute_commands] )) ||
_wayle__help__audio__input-mute_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help audio input-mute commands' commands "$@"
}
(( $+functions[_wayle__help__audio__input-volume_commands] )) ||
_wayle__help__audio__input-volume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help audio input-volume commands' commands "$@"
}
(( $+functions[_wayle__help__audio__output-mute_commands] )) ||
_wayle__help__audio__output-mute_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help audio output-mute commands' commands "$@"
}
(( $+functions[_wayle__help__audio__output-volume_commands] )) ||
_wayle__help__audio__output-volume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help audio output-volume commands' commands "$@"
}
(( $+functions[_wayle__help__audio__sinks_commands] )) ||
_wayle__help__audio__sinks_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help audio sinks commands' commands "$@"
}
(( $+functions[_wayle__help__audio__sources_commands] )) ||
_wayle__help__audio__sources_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help audio sources commands' commands "$@"
}
(( $+functions[_wayle__help__audio__status_commands] )) ||
_wayle__help__audio__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help audio status commands' commands "$@"
}
(( $+functions[_wayle__help__completions_commands] )) ||
_wayle__help__completions_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help completions commands' commands "$@"
}
(( $+functions[_wayle__help__config_commands] )) ||
_wayle__help__config_commands() {
    local commands; commands=(
'get:Get the value of a configuration path' \
'set:Set the value of a configuration path' \
'reset:Reset a configuration path to its default value' \
'schema:Output JSON Schema for the configuration (for editor intellisense)' \
'default:Output the default configuration as TOML' \
'docs:Generate markdown reference pages for every registered schema' \
    )
    _describe -t commands 'wayle help config commands' commands "$@"
}
(( $+functions[_wayle__help__config__default_commands] )) ||
_wayle__help__config__default_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help config default commands' commands "$@"
}
(( $+functions[_wayle__help__config__docs_commands] )) ||
_wayle__help__config__docs_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help config docs commands' commands "$@"
}
(( $+functions[_wayle__help__config__get_commands] )) ||
_wayle__help__config__get_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help config get commands' commands "$@"
}
(( $+functions[_wayle__help__config__reset_commands] )) ||
_wayle__help__config__reset_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help config reset commands' commands "$@"
}
(( $+functions[_wayle__help__config__schema_commands] )) ||
_wayle__help__config__schema_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help config schema commands' commands "$@"
}
(( $+functions[_wayle__help__config__set_commands] )) ||
_wayle__help__config__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help config set commands' commands "$@"
}
(( $+functions[_wayle__help__help_commands] )) ||
_wayle__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help help commands' commands "$@"
}
(( $+functions[_wayle__help__icons_commands] )) ||
_wayle__help__icons_commands() {
    local commands; commands=(
'setup:Install bundled icons required by Wayle components' \
'install:Install icons from a CDN source' \
'import:Import local SVG file(s) as icons' \
'remove:Remove installed icons' \
'sources:List available icon sources' \
'list:List installed icons' \
'open:Open the icons directory in file manager' \
'export:Export all installed icons to a directory' \
'sync:Install any icons referenced in config but not yet on disk' \
    )
    _describe -t commands 'wayle help icons commands' commands "$@"
}
(( $+functions[_wayle__help__icons__export_commands] )) ||
_wayle__help__icons__export_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons export commands' commands "$@"
}
(( $+functions[_wayle__help__icons__import_commands] )) ||
_wayle__help__icons__import_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons import commands' commands "$@"
}
(( $+functions[_wayle__help__icons__install_commands] )) ||
_wayle__help__icons__install_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons install commands' commands "$@"
}
(( $+functions[_wayle__help__icons__list_commands] )) ||
_wayle__help__icons__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons list commands' commands "$@"
}
(( $+functions[_wayle__help__icons__open_commands] )) ||
_wayle__help__icons__open_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons open commands' commands "$@"
}
(( $+functions[_wayle__help__icons__remove_commands] )) ||
_wayle__help__icons__remove_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons remove commands' commands "$@"
}
(( $+functions[_wayle__help__icons__setup_commands] )) ||
_wayle__help__icons__setup_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons setup commands' commands "$@"
}
(( $+functions[_wayle__help__icons__sources_commands] )) ||
_wayle__help__icons__sources_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons sources commands' commands "$@"
}
(( $+functions[_wayle__help__icons__sync_commands] )) ||
_wayle__help__icons__sync_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help icons sync commands' commands "$@"
}
(( $+functions[_wayle__help__idle_commands] )) ||
_wayle__help__idle_commands() {
    local commands; commands=(
'on:Enable idle inhibition' \
'off:Disable idle inhibition' \
'duration:Adjust timer duration (upper limit)' \
'remaining:Adjust remaining time on active timer' \
'status:Show current idle inhibit status' \
'toggle:Toggle idle inhibition on/off' \
    )
    _describe -t commands 'wayle help idle commands' commands "$@"
}
(( $+functions[_wayle__help__idle__duration_commands] )) ||
_wayle__help__idle__duration_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help idle duration commands' commands "$@"
}
(( $+functions[_wayle__help__idle__off_commands] )) ||
_wayle__help__idle__off_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help idle off commands' commands "$@"
}
(( $+functions[_wayle__help__idle__on_commands] )) ||
_wayle__help__idle__on_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help idle on commands' commands "$@"
}
(( $+functions[_wayle__help__idle__remaining_commands] )) ||
_wayle__help__idle__remaining_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help idle remaining commands' commands "$@"
}
(( $+functions[_wayle__help__idle__status_commands] )) ||
_wayle__help__idle__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help idle status commands' commands "$@"
}
(( $+functions[_wayle__help__idle__toggle_commands] )) ||
_wayle__help__idle__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help idle toggle commands' commands "$@"
}
(( $+functions[_wayle__help__launcher_commands] )) ||
_wayle__help__launcher_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help launcher commands' commands "$@"
}
(( $+functions[_wayle__help__lock_commands] )) ||
_wayle__help__lock_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help lock commands' commands "$@"
}
(( $+functions[_wayle__help__media_commands] )) ||
_wayle__help__media_commands() {
    local commands; commands=(
'list:List all available media players' \
'play-pause:Toggle play/pause for a media player' \
'next:Skip to next track' \
'previous:Go to previous track' \
'shuffle:Toggle or set shuffle mode' \
'loop:Set loop/repeat mode' \
'active:Get or set the active media player' \
'info:Display detailed information about a media player' \
    )
    _describe -t commands 'wayle help media commands' commands "$@"
}
(( $+functions[_wayle__help__media__active_commands] )) ||
_wayle__help__media__active_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help media active commands' commands "$@"
}
(( $+functions[_wayle__help__media__info_commands] )) ||
_wayle__help__media__info_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help media info commands' commands "$@"
}
(( $+functions[_wayle__help__media__list_commands] )) ||
_wayle__help__media__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help media list commands' commands "$@"
}
(( $+functions[_wayle__help__media__loop_commands] )) ||
_wayle__help__media__loop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help media loop commands' commands "$@"
}
(( $+functions[_wayle__help__media__next_commands] )) ||
_wayle__help__media__next_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help media next commands' commands "$@"
}
(( $+functions[_wayle__help__media__play-pause_commands] )) ||
_wayle__help__media__play-pause_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help media play-pause commands' commands "$@"
}
(( $+functions[_wayle__help__media__previous_commands] )) ||
_wayle__help__media__previous_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help media previous commands' commands "$@"
}
(( $+functions[_wayle__help__media__shuffle_commands] )) ||
_wayle__help__media__shuffle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help media shuffle commands' commands "$@"
}
(( $+functions[_wayle__help__notify_commands] )) ||
_wayle__help__notify_commands() {
    local commands; commands=(
'list:List all notifications' \
'dismiss:Dismiss a notification by ID' \
'dismiss-all:Dismiss all notifications' \
'dnd:Toggle Do Not Disturb mode' \
'status:Show notification status' \
    )
    _describe -t commands 'wayle help notify commands' commands "$@"
}
(( $+functions[_wayle__help__notify__dismiss_commands] )) ||
_wayle__help__notify__dismiss_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help notify dismiss commands' commands "$@"
}
(( $+functions[_wayle__help__notify__dismiss-all_commands] )) ||
_wayle__help__notify__dismiss-all_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help notify dismiss-all commands' commands "$@"
}
(( $+functions[_wayle__help__notify__dnd_commands] )) ||
_wayle__help__notify__dnd_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help notify dnd commands' commands "$@"
}
(( $+functions[_wayle__help__notify__list_commands] )) ||
_wayle__help__notify__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help notify list commands' commands "$@"
}
(( $+functions[_wayle__help__notify__status_commands] )) ||
_wayle__help__notify__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help notify status commands' commands "$@"
}
(( $+functions[_wayle__help__panel_commands] )) ||
_wayle__help__panel_commands() {
    local commands; commands=(
'start:Start the panel daemon' \
'stop:Stop the panel daemon' \
'restart:Restart the panel daemon' \
'status:Check panel status' \
'settings:Open panel settings' \
'inspect:Open GTK Inspector for debugging' \
'hide:Hide the bar on a monitor' \
'show:Show the bar on a monitor' \
'toggle:Toggle bar visibility on a monitor' \
    )
    _describe -t commands 'wayle help panel commands' commands "$@"
}
(( $+functions[_wayle__help__panel__hide_commands] )) ||
_wayle__help__panel__hide_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel hide commands' commands "$@"
}
(( $+functions[_wayle__help__panel__inspect_commands] )) ||
_wayle__help__panel__inspect_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel inspect commands' commands "$@"
}
(( $+functions[_wayle__help__panel__restart_commands] )) ||
_wayle__help__panel__restart_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel restart commands' commands "$@"
}
(( $+functions[_wayle__help__panel__settings_commands] )) ||
_wayle__help__panel__settings_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel settings commands' commands "$@"
}
(( $+functions[_wayle__help__panel__show_commands] )) ||
_wayle__help__panel__show_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel show commands' commands "$@"
}
(( $+functions[_wayle__help__panel__start_commands] )) ||
_wayle__help__panel__start_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel start commands' commands "$@"
}
(( $+functions[_wayle__help__panel__status_commands] )) ||
_wayle__help__panel__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel status commands' commands "$@"
}
(( $+functions[_wayle__help__panel__stop_commands] )) ||
_wayle__help__panel__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel stop commands' commands "$@"
}
(( $+functions[_wayle__help__panel__toggle_commands] )) ||
_wayle__help__panel__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help panel toggle commands' commands "$@"
}
(( $+functions[_wayle__help__portal_commands] )) ||
_wayle__help__portal_commands() {
    local commands; commands=(
'run:Run the xdg-desktop-portal backend (the default when no subcommand is given, so the installed D-Bus/systemd units can keep execing \`wayle portal\`)' \
'share-picker:xdg-desktop-portal-hyprland screencast picker stub (invoked by the portal, not by hand)' \
'show:Preview a portal dialog UI without an application request (developer tool; talks to the running shell over D-Bus, same as the real backend)' \
    )
    _describe -t commands 'wayle help portal commands' commands "$@"
}
(( $+functions[_wayle__help__portal__run_commands] )) ||
_wayle__help__portal__run_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal run commands' commands "$@"
}
(( $+functions[_wayle__help__portal__share-picker_commands] )) ||
_wayle__help__portal__share-picker_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal share-picker commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show_commands] )) ||
_wayle__help__portal__show_commands() {
    local commands; commands=(
'file-chooser:File open/save dialog (\`org.freedesktop.impl.portal.FileChooser\`)' \
'screenshot:Screenshot capture (\`org.freedesktop.impl.portal.Screenshot\`)' \
'color:Interactive color picker (\`Screenshot.PickColor\`)' \
'print:Printer selection dialog (\`org.freedesktop.impl.portal.Print\`); only the printer/settings prepare step is shown, no document is spooled' \
'screen-cast:Screencast source picker (\`org.freedesktop.impl.portal.ScreenCast\`)' \
'access:Generic grant/deny access prompt (\`org.freedesktop.impl.portal.Access\`)' \
'account:Account info sharing consent (\`org.freedesktop.impl.portal.Account\`)' \
'app-chooser:Application chooser (\`org.freedesktop.impl.portal.AppChooser\`)' \
'dynamic-launcher:Dynamic launcher install confirmation (\`org.freedesktop.impl.portal.DynamicLauncher\`)' \
'wallpaper:Wallpaper preview confirmation (\`org.freedesktop.impl.portal.Wallpaper\` \`show-preview\`)' \
    )
    _describe -t commands 'wayle help portal show commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__access_commands] )) ||
_wayle__help__portal__show__access_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show access commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__account_commands] )) ||
_wayle__help__portal__show__account_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show account commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__app-chooser_commands] )) ||
_wayle__help__portal__show__app-chooser_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show app-chooser commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__color_commands] )) ||
_wayle__help__portal__show__color_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show color commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__dynamic-launcher_commands] )) ||
_wayle__help__portal__show__dynamic-launcher_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show dynamic-launcher commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__file-chooser_commands] )) ||
_wayle__help__portal__show__file-chooser_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show file-chooser commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__print_commands] )) ||
_wayle__help__portal__show__print_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show print commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__screen-cast_commands] )) ||
_wayle__help__portal__show__screen-cast_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show screen-cast commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__screenshot_commands] )) ||
_wayle__help__portal__show__screenshot_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show screenshot commands' commands "$@"
}
(( $+functions[_wayle__help__portal__show__wallpaper_commands] )) ||
_wayle__help__portal__show__wallpaper_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help portal show wallpaper commands' commands "$@"
}
(( $+functions[_wayle__help__power_commands] )) ||
_wayle__help__power_commands() {
    local commands; commands=(
'status:Show current power profile' \
'set:Set power profile' \
'cycle:Cycle to next power profile' \
'list:List available power profiles' \
    )
    _describe -t commands 'wayle help power commands' commands "$@"
}
(( $+functions[_wayle__help__power__cycle_commands] )) ||
_wayle__help__power__cycle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help power cycle commands' commands "$@"
}
(( $+functions[_wayle__help__power__list_commands] )) ||
_wayle__help__power__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help power list commands' commands "$@"
}
(( $+functions[_wayle__help__power__set_commands] )) ||
_wayle__help__power__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help power set commands' commands "$@"
}
(( $+functions[_wayle__help__power__status_commands] )) ||
_wayle__help__power__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help power status commands' commands "$@"
}
(( $+functions[_wayle__help__recorder_commands] )) ||
_wayle__help__recorder_commands() {
    local commands; commands=(
'start:Start recording' \
'stop:Stop recording' \
'toggle:Toggle recording on/off' \
'pause:Pause the active recording' \
'resume:Resume a paused recording' \
'status:Show current recorder status' \
    )
    _describe -t commands 'wayle help recorder commands' commands "$@"
}
(( $+functions[_wayle__help__recorder__pause_commands] )) ||
_wayle__help__recorder__pause_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help recorder pause commands' commands "$@"
}
(( $+functions[_wayle__help__recorder__resume_commands] )) ||
_wayle__help__recorder__resume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help recorder resume commands' commands "$@"
}
(( $+functions[_wayle__help__recorder__start_commands] )) ||
_wayle__help__recorder__start_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help recorder start commands' commands "$@"
}
(( $+functions[_wayle__help__recorder__status_commands] )) ||
_wayle__help__recorder__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help recorder status commands' commands "$@"
}
(( $+functions[_wayle__help__recorder__stop_commands] )) ||
_wayle__help__recorder__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help recorder stop commands' commands "$@"
}
(( $+functions[_wayle__help__recorder__toggle_commands] )) ||
_wayle__help__recorder__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help recorder toggle commands' commands "$@"
}
(( $+functions[_wayle__help__screenshot_commands] )) ||
_wayle__help__screenshot_commands() {
    local commands; commands=(
'region:Capture a drag-selected region' \
'output:Capture a whole output (the focused output when NAME is omitted)' \
'window:Capture the active window' \
    )
    _describe -t commands 'wayle help screenshot commands' commands "$@"
}
(( $+functions[_wayle__help__screenshot__output_commands] )) ||
_wayle__help__screenshot__output_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help screenshot output commands' commands "$@"
}
(( $+functions[_wayle__help__screenshot__region_commands] )) ||
_wayle__help__screenshot__region_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help screenshot region commands' commands "$@"
}
(( $+functions[_wayle__help__screenshot__window_commands] )) ||
_wayle__help__screenshot__window_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help screenshot window commands' commands "$@"
}
(( $+functions[_wayle__help__shell_commands] )) ||
_wayle__help__shell_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help shell commands' commands "$@"
}
(( $+functions[_wayle__help__systray_commands] )) ||
_wayle__help__systray_commands() {
    local commands; commands=(
'list:List all system tray items' \
'activate:Activate a tray item by ID' \
'status:Show system tray status' \
    )
    _describe -t commands 'wayle help systray commands' commands "$@"
}
(( $+functions[_wayle__help__systray__activate_commands] )) ||
_wayle__help__systray__activate_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help systray activate commands' commands "$@"
}
(( $+functions[_wayle__help__systray__list_commands] )) ||
_wayle__help__systray__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help systray list commands' commands "$@"
}
(( $+functions[_wayle__help__systray__status_commands] )) ||
_wayle__help__systray__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help systray status commands' commands "$@"
}
(( $+functions[_wayle__help__toast_commands] )) ||
_wayle__help__toast_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help toast commands' commands "$@"
}
(( $+functions[_wayle__help__vpn_commands] )) ||
_wayle__help__vpn_commands() {
    local commands; commands=(
'sso-callback:Hand a browser sign-in callback to the running shell' \
    )
    _describe -t commands 'wayle help vpn commands' commands "$@"
}
(( $+functions[_wayle__help__vpn__sso-callback_commands] )) ||
_wayle__help__vpn__sso-callback_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help vpn sso-callback commands' commands "$@"
}
(( $+functions[_wayle__help__wallpaper_commands] )) ||
_wayle__help__wallpaper_commands() {
    local commands; commands=(
'set:Set wallpaper from an image file' \
'cycle:Start cycling wallpapers from a directory' \
'stop:Stop wallpaper cycling' \
'next:Skip to next wallpaper' \
'previous:Go back to previous wallpaper' \
'info:Display current wallpaper information' \
'theming-monitor:Set which monitor to use for color extraction' \
    )
    _describe -t commands 'wayle help wallpaper commands' commands "$@"
}
(( $+functions[_wayle__help__wallpaper__cycle_commands] )) ||
_wayle__help__wallpaper__cycle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help wallpaper cycle commands' commands "$@"
}
(( $+functions[_wayle__help__wallpaper__info_commands] )) ||
_wayle__help__wallpaper__info_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help wallpaper info commands' commands "$@"
}
(( $+functions[_wayle__help__wallpaper__next_commands] )) ||
_wayle__help__wallpaper__next_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help wallpaper next commands' commands "$@"
}
(( $+functions[_wayle__help__wallpaper__previous_commands] )) ||
_wayle__help__wallpaper__previous_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help wallpaper previous commands' commands "$@"
}
(( $+functions[_wayle__help__wallpaper__set_commands] )) ||
_wayle__help__wallpaper__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help wallpaper set commands' commands "$@"
}
(( $+functions[_wayle__help__wallpaper__stop_commands] )) ||
_wayle__help__wallpaper__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help wallpaper stop commands' commands "$@"
}
(( $+functions[_wayle__help__wallpaper__theming-monitor_commands] )) ||
_wayle__help__wallpaper__theming-monitor_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help wallpaper theming-monitor commands' commands "$@"
}
(( $+functions[_wayle__help__widget_commands] )) ||
_wayle__help__widget_commands() {
    local commands; commands=(
'update:Push an output update to a bar widget by its config id' \
    )
    _describe -t commands 'wayle help widget commands' commands "$@"
}
(( $+functions[_wayle__help__widget__update_commands] )) ||
_wayle__help__widget__update_commands() {
    local commands; commands=()
    _describe -t commands 'wayle help widget update commands' commands "$@"
}
(( $+functions[_wayle__icons_commands] )) ||
_wayle__icons_commands() {
    local commands; commands=(
'setup:Install bundled icons required by Wayle components' \
'install:Install icons from a CDN source' \
'import:Import local SVG file(s) as icons' \
'remove:Remove installed icons' \
'sources:List available icon sources' \
'list:List installed icons' \
'open:Open the icons directory in file manager' \
'export:Export all installed icons to a directory' \
'sync:Install any icons referenced in config but not yet on disk' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle icons commands' commands "$@"
}
(( $+functions[_wayle__icons__export_commands] )) ||
_wayle__icons__export_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons export commands' commands "$@"
}
(( $+functions[_wayle__icons__help_commands] )) ||
_wayle__icons__help_commands() {
    local commands; commands=(
'setup:Install bundled icons required by Wayle components' \
'install:Install icons from a CDN source' \
'import:Import local SVG file(s) as icons' \
'remove:Remove installed icons' \
'sources:List available icon sources' \
'list:List installed icons' \
'open:Open the icons directory in file manager' \
'export:Export all installed icons to a directory' \
'sync:Install any icons referenced in config but not yet on disk' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle icons help commands' commands "$@"
}
(( $+functions[_wayle__icons__help__export_commands] )) ||
_wayle__icons__help__export_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help export commands' commands "$@"
}
(( $+functions[_wayle__icons__help__help_commands] )) ||
_wayle__icons__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help help commands' commands "$@"
}
(( $+functions[_wayle__icons__help__import_commands] )) ||
_wayle__icons__help__import_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help import commands' commands "$@"
}
(( $+functions[_wayle__icons__help__install_commands] )) ||
_wayle__icons__help__install_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help install commands' commands "$@"
}
(( $+functions[_wayle__icons__help__list_commands] )) ||
_wayle__icons__help__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help list commands' commands "$@"
}
(( $+functions[_wayle__icons__help__open_commands] )) ||
_wayle__icons__help__open_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help open commands' commands "$@"
}
(( $+functions[_wayle__icons__help__remove_commands] )) ||
_wayle__icons__help__remove_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help remove commands' commands "$@"
}
(( $+functions[_wayle__icons__help__setup_commands] )) ||
_wayle__icons__help__setup_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help setup commands' commands "$@"
}
(( $+functions[_wayle__icons__help__sources_commands] )) ||
_wayle__icons__help__sources_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help sources commands' commands "$@"
}
(( $+functions[_wayle__icons__help__sync_commands] )) ||
_wayle__icons__help__sync_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons help sync commands' commands "$@"
}
(( $+functions[_wayle__icons__import_commands] )) ||
_wayle__icons__import_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons import commands' commands "$@"
}
(( $+functions[_wayle__icons__install_commands] )) ||
_wayle__icons__install_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons install commands' commands "$@"
}
(( $+functions[_wayle__icons__list_commands] )) ||
_wayle__icons__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons list commands' commands "$@"
}
(( $+functions[_wayle__icons__open_commands] )) ||
_wayle__icons__open_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons open commands' commands "$@"
}
(( $+functions[_wayle__icons__remove_commands] )) ||
_wayle__icons__remove_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons remove commands' commands "$@"
}
(( $+functions[_wayle__icons__setup_commands] )) ||
_wayle__icons__setup_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons setup commands' commands "$@"
}
(( $+functions[_wayle__icons__sources_commands] )) ||
_wayle__icons__sources_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons sources commands' commands "$@"
}
(( $+functions[_wayle__icons__sync_commands] )) ||
_wayle__icons__sync_commands() {
    local commands; commands=()
    _describe -t commands 'wayle icons sync commands' commands "$@"
}
(( $+functions[_wayle__idle_commands] )) ||
_wayle__idle_commands() {
    local commands; commands=(
'on:Enable idle inhibition' \
'off:Disable idle inhibition' \
'duration:Adjust timer duration (upper limit)' \
'remaining:Adjust remaining time on active timer' \
'status:Show current idle inhibit status' \
'toggle:Toggle idle inhibition on/off' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle idle commands' commands "$@"
}
(( $+functions[_wayle__idle__duration_commands] )) ||
_wayle__idle__duration_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle duration commands' commands "$@"
}
(( $+functions[_wayle__idle__help_commands] )) ||
_wayle__idle__help_commands() {
    local commands; commands=(
'on:Enable idle inhibition' \
'off:Disable idle inhibition' \
'duration:Adjust timer duration (upper limit)' \
'remaining:Adjust remaining time on active timer' \
'status:Show current idle inhibit status' \
'toggle:Toggle idle inhibition on/off' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle idle help commands' commands "$@"
}
(( $+functions[_wayle__idle__help__duration_commands] )) ||
_wayle__idle__help__duration_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle help duration commands' commands "$@"
}
(( $+functions[_wayle__idle__help__help_commands] )) ||
_wayle__idle__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle help help commands' commands "$@"
}
(( $+functions[_wayle__idle__help__off_commands] )) ||
_wayle__idle__help__off_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle help off commands' commands "$@"
}
(( $+functions[_wayle__idle__help__on_commands] )) ||
_wayle__idle__help__on_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle help on commands' commands "$@"
}
(( $+functions[_wayle__idle__help__remaining_commands] )) ||
_wayle__idle__help__remaining_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle help remaining commands' commands "$@"
}
(( $+functions[_wayle__idle__help__status_commands] )) ||
_wayle__idle__help__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle help status commands' commands "$@"
}
(( $+functions[_wayle__idle__help__toggle_commands] )) ||
_wayle__idle__help__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle help toggle commands' commands "$@"
}
(( $+functions[_wayle__idle__off_commands] )) ||
_wayle__idle__off_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle off commands' commands "$@"
}
(( $+functions[_wayle__idle__on_commands] )) ||
_wayle__idle__on_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle on commands' commands "$@"
}
(( $+functions[_wayle__idle__remaining_commands] )) ||
_wayle__idle__remaining_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle remaining commands' commands "$@"
}
(( $+functions[_wayle__idle__status_commands] )) ||
_wayle__idle__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle status commands' commands "$@"
}
(( $+functions[_wayle__idle__toggle_commands] )) ||
_wayle__idle__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle idle toggle commands' commands "$@"
}
(( $+functions[_wayle__launcher_commands] )) ||
_wayle__launcher_commands() {
    local commands; commands=()
    _describe -t commands 'wayle launcher commands' commands "$@"
}
(( $+functions[_wayle__lock_commands] )) ||
_wayle__lock_commands() {
    local commands; commands=()
    _describe -t commands 'wayle lock commands' commands "$@"
}
(( $+functions[_wayle__media_commands] )) ||
_wayle__media_commands() {
    local commands; commands=(
'list:List all available media players' \
'play-pause:Toggle play/pause for a media player' \
'next:Skip to next track' \
'previous:Go to previous track' \
'shuffle:Toggle or set shuffle mode' \
'loop:Set loop/repeat mode' \
'active:Get or set the active media player' \
'info:Display detailed information about a media player' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle media commands' commands "$@"
}
(( $+functions[_wayle__media__active_commands] )) ||
_wayle__media__active_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media active commands' commands "$@"
}
(( $+functions[_wayle__media__help_commands] )) ||
_wayle__media__help_commands() {
    local commands; commands=(
'list:List all available media players' \
'play-pause:Toggle play/pause for a media player' \
'next:Skip to next track' \
'previous:Go to previous track' \
'shuffle:Toggle or set shuffle mode' \
'loop:Set loop/repeat mode' \
'active:Get or set the active media player' \
'info:Display detailed information about a media player' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle media help commands' commands "$@"
}
(( $+functions[_wayle__media__help__active_commands] )) ||
_wayle__media__help__active_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help active commands' commands "$@"
}
(( $+functions[_wayle__media__help__help_commands] )) ||
_wayle__media__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help help commands' commands "$@"
}
(( $+functions[_wayle__media__help__info_commands] )) ||
_wayle__media__help__info_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help info commands' commands "$@"
}
(( $+functions[_wayle__media__help__list_commands] )) ||
_wayle__media__help__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help list commands' commands "$@"
}
(( $+functions[_wayle__media__help__loop_commands] )) ||
_wayle__media__help__loop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help loop commands' commands "$@"
}
(( $+functions[_wayle__media__help__next_commands] )) ||
_wayle__media__help__next_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help next commands' commands "$@"
}
(( $+functions[_wayle__media__help__play-pause_commands] )) ||
_wayle__media__help__play-pause_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help play-pause commands' commands "$@"
}
(( $+functions[_wayle__media__help__previous_commands] )) ||
_wayle__media__help__previous_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help previous commands' commands "$@"
}
(( $+functions[_wayle__media__help__shuffle_commands] )) ||
_wayle__media__help__shuffle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media help shuffle commands' commands "$@"
}
(( $+functions[_wayle__media__info_commands] )) ||
_wayle__media__info_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media info commands' commands "$@"
}
(( $+functions[_wayle__media__list_commands] )) ||
_wayle__media__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media list commands' commands "$@"
}
(( $+functions[_wayle__media__loop_commands] )) ||
_wayle__media__loop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media loop commands' commands "$@"
}
(( $+functions[_wayle__media__next_commands] )) ||
_wayle__media__next_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media next commands' commands "$@"
}
(( $+functions[_wayle__media__play-pause_commands] )) ||
_wayle__media__play-pause_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media play-pause commands' commands "$@"
}
(( $+functions[_wayle__media__previous_commands] )) ||
_wayle__media__previous_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media previous commands' commands "$@"
}
(( $+functions[_wayle__media__shuffle_commands] )) ||
_wayle__media__shuffle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle media shuffle commands' commands "$@"
}
(( $+functions[_wayle__notify_commands] )) ||
_wayle__notify_commands() {
    local commands; commands=(
'list:List all notifications' \
'dismiss:Dismiss a notification by ID' \
'dismiss-all:Dismiss all notifications' \
'dnd:Toggle Do Not Disturb mode' \
'status:Show notification status' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle notify commands' commands "$@"
}
(( $+functions[_wayle__notify__dismiss_commands] )) ||
_wayle__notify__dismiss_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify dismiss commands' commands "$@"
}
(( $+functions[_wayle__notify__dismiss-all_commands] )) ||
_wayle__notify__dismiss-all_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify dismiss-all commands' commands "$@"
}
(( $+functions[_wayle__notify__dnd_commands] )) ||
_wayle__notify__dnd_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify dnd commands' commands "$@"
}
(( $+functions[_wayle__notify__help_commands] )) ||
_wayle__notify__help_commands() {
    local commands; commands=(
'list:List all notifications' \
'dismiss:Dismiss a notification by ID' \
'dismiss-all:Dismiss all notifications' \
'dnd:Toggle Do Not Disturb mode' \
'status:Show notification status' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle notify help commands' commands "$@"
}
(( $+functions[_wayle__notify__help__dismiss_commands] )) ||
_wayle__notify__help__dismiss_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify help dismiss commands' commands "$@"
}
(( $+functions[_wayle__notify__help__dismiss-all_commands] )) ||
_wayle__notify__help__dismiss-all_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify help dismiss-all commands' commands "$@"
}
(( $+functions[_wayle__notify__help__dnd_commands] )) ||
_wayle__notify__help__dnd_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify help dnd commands' commands "$@"
}
(( $+functions[_wayle__notify__help__help_commands] )) ||
_wayle__notify__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify help help commands' commands "$@"
}
(( $+functions[_wayle__notify__help__list_commands] )) ||
_wayle__notify__help__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify help list commands' commands "$@"
}
(( $+functions[_wayle__notify__help__status_commands] )) ||
_wayle__notify__help__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify help status commands' commands "$@"
}
(( $+functions[_wayle__notify__list_commands] )) ||
_wayle__notify__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify list commands' commands "$@"
}
(( $+functions[_wayle__notify__status_commands] )) ||
_wayle__notify__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle notify status commands' commands "$@"
}
(( $+functions[_wayle__panel_commands] )) ||
_wayle__panel_commands() {
    local commands; commands=(
'start:Start the panel daemon' \
'stop:Stop the panel daemon' \
'restart:Restart the panel daemon' \
'status:Check panel status' \
'settings:Open panel settings' \
'inspect:Open GTK Inspector for debugging' \
'hide:Hide the bar on a monitor' \
'show:Show the bar on a monitor' \
'toggle:Toggle bar visibility on a monitor' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle panel commands' commands "$@"
}
(( $+functions[_wayle__panel__help_commands] )) ||
_wayle__panel__help_commands() {
    local commands; commands=(
'start:Start the panel daemon' \
'stop:Stop the panel daemon' \
'restart:Restart the panel daemon' \
'status:Check panel status' \
'settings:Open panel settings' \
'inspect:Open GTK Inspector for debugging' \
'hide:Hide the bar on a monitor' \
'show:Show the bar on a monitor' \
'toggle:Toggle bar visibility on a monitor' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle panel help commands' commands "$@"
}
(( $+functions[_wayle__panel__help__help_commands] )) ||
_wayle__panel__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help help commands' commands "$@"
}
(( $+functions[_wayle__panel__help__hide_commands] )) ||
_wayle__panel__help__hide_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help hide commands' commands "$@"
}
(( $+functions[_wayle__panel__help__inspect_commands] )) ||
_wayle__panel__help__inspect_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help inspect commands' commands "$@"
}
(( $+functions[_wayle__panel__help__restart_commands] )) ||
_wayle__panel__help__restart_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help restart commands' commands "$@"
}
(( $+functions[_wayle__panel__help__settings_commands] )) ||
_wayle__panel__help__settings_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help settings commands' commands "$@"
}
(( $+functions[_wayle__panel__help__show_commands] )) ||
_wayle__panel__help__show_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help show commands' commands "$@"
}
(( $+functions[_wayle__panel__help__start_commands] )) ||
_wayle__panel__help__start_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help start commands' commands "$@"
}
(( $+functions[_wayle__panel__help__status_commands] )) ||
_wayle__panel__help__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help status commands' commands "$@"
}
(( $+functions[_wayle__panel__help__stop_commands] )) ||
_wayle__panel__help__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help stop commands' commands "$@"
}
(( $+functions[_wayle__panel__help__toggle_commands] )) ||
_wayle__panel__help__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel help toggle commands' commands "$@"
}
(( $+functions[_wayle__panel__hide_commands] )) ||
_wayle__panel__hide_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel hide commands' commands "$@"
}
(( $+functions[_wayle__panel__inspect_commands] )) ||
_wayle__panel__inspect_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel inspect commands' commands "$@"
}
(( $+functions[_wayle__panel__restart_commands] )) ||
_wayle__panel__restart_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel restart commands' commands "$@"
}
(( $+functions[_wayle__panel__settings_commands] )) ||
_wayle__panel__settings_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel settings commands' commands "$@"
}
(( $+functions[_wayle__panel__show_commands] )) ||
_wayle__panel__show_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel show commands' commands "$@"
}
(( $+functions[_wayle__panel__start_commands] )) ||
_wayle__panel__start_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel start commands' commands "$@"
}
(( $+functions[_wayle__panel__status_commands] )) ||
_wayle__panel__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel status commands' commands "$@"
}
(( $+functions[_wayle__panel__stop_commands] )) ||
_wayle__panel__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel stop commands' commands "$@"
}
(( $+functions[_wayle__panel__toggle_commands] )) ||
_wayle__panel__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle panel toggle commands' commands "$@"
}
(( $+functions[_wayle__portal_commands] )) ||
_wayle__portal_commands() {
    local commands; commands=(
'run:Run the xdg-desktop-portal backend (the default when no subcommand is given, so the installed D-Bus/systemd units can keep execing \`wayle portal\`)' \
'share-picker:xdg-desktop-portal-hyprland screencast picker stub (invoked by the portal, not by hand)' \
'show:Preview a portal dialog UI without an application request (developer tool; talks to the running shell over D-Bus, same as the real backend)' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle portal commands' commands "$@"
}
(( $+functions[_wayle__portal__help_commands] )) ||
_wayle__portal__help_commands() {
    local commands; commands=(
'run:Run the xdg-desktop-portal backend (the default when no subcommand is given, so the installed D-Bus/systemd units can keep execing \`wayle portal\`)' \
'share-picker:xdg-desktop-portal-hyprland screencast picker stub (invoked by the portal, not by hand)' \
'show:Preview a portal dialog UI without an application request (developer tool; talks to the running shell over D-Bus, same as the real backend)' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle portal help commands' commands "$@"
}
(( $+functions[_wayle__portal__help__help_commands] )) ||
_wayle__portal__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help help commands' commands "$@"
}
(( $+functions[_wayle__portal__help__run_commands] )) ||
_wayle__portal__help__run_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help run commands' commands "$@"
}
(( $+functions[_wayle__portal__help__share-picker_commands] )) ||
_wayle__portal__help__share-picker_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help share-picker commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show_commands] )) ||
_wayle__portal__help__show_commands() {
    local commands; commands=(
'file-chooser:File open/save dialog (\`org.freedesktop.impl.portal.FileChooser\`)' \
'screenshot:Screenshot capture (\`org.freedesktop.impl.portal.Screenshot\`)' \
'color:Interactive color picker (\`Screenshot.PickColor\`)' \
'print:Printer selection dialog (\`org.freedesktop.impl.portal.Print\`); only the printer/settings prepare step is shown, no document is spooled' \
'screen-cast:Screencast source picker (\`org.freedesktop.impl.portal.ScreenCast\`)' \
'access:Generic grant/deny access prompt (\`org.freedesktop.impl.portal.Access\`)' \
'account:Account info sharing consent (\`org.freedesktop.impl.portal.Account\`)' \
'app-chooser:Application chooser (\`org.freedesktop.impl.portal.AppChooser\`)' \
'dynamic-launcher:Dynamic launcher install confirmation (\`org.freedesktop.impl.portal.DynamicLauncher\`)' \
'wallpaper:Wallpaper preview confirmation (\`org.freedesktop.impl.portal.Wallpaper\` \`show-preview\`)' \
    )
    _describe -t commands 'wayle portal help show commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__access_commands] )) ||
_wayle__portal__help__show__access_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show access commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__account_commands] )) ||
_wayle__portal__help__show__account_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show account commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__app-chooser_commands] )) ||
_wayle__portal__help__show__app-chooser_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show app-chooser commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__color_commands] )) ||
_wayle__portal__help__show__color_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show color commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__dynamic-launcher_commands] )) ||
_wayle__portal__help__show__dynamic-launcher_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show dynamic-launcher commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__file-chooser_commands] )) ||
_wayle__portal__help__show__file-chooser_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show file-chooser commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__print_commands] )) ||
_wayle__portal__help__show__print_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show print commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__screen-cast_commands] )) ||
_wayle__portal__help__show__screen-cast_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show screen-cast commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__screenshot_commands] )) ||
_wayle__portal__help__show__screenshot_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show screenshot commands' commands "$@"
}
(( $+functions[_wayle__portal__help__show__wallpaper_commands] )) ||
_wayle__portal__help__show__wallpaper_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal help show wallpaper commands' commands "$@"
}
(( $+functions[_wayle__portal__run_commands] )) ||
_wayle__portal__run_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal run commands' commands "$@"
}
(( $+functions[_wayle__portal__share-picker_commands] )) ||
_wayle__portal__share-picker_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal share-picker commands' commands "$@"
}
(( $+functions[_wayle__portal__show_commands] )) ||
_wayle__portal__show_commands() {
    local commands; commands=(
'file-chooser:File open/save dialog (\`org.freedesktop.impl.portal.FileChooser\`)' \
'screenshot:Screenshot capture (\`org.freedesktop.impl.portal.Screenshot\`)' \
'color:Interactive color picker (\`Screenshot.PickColor\`)' \
'print:Printer selection dialog (\`org.freedesktop.impl.portal.Print\`); only the printer/settings prepare step is shown, no document is spooled' \
'screen-cast:Screencast source picker (\`org.freedesktop.impl.portal.ScreenCast\`)' \
'access:Generic grant/deny access prompt (\`org.freedesktop.impl.portal.Access\`)' \
'account:Account info sharing consent (\`org.freedesktop.impl.portal.Account\`)' \
'app-chooser:Application chooser (\`org.freedesktop.impl.portal.AppChooser\`)' \
'dynamic-launcher:Dynamic launcher install confirmation (\`org.freedesktop.impl.portal.DynamicLauncher\`)' \
'wallpaper:Wallpaper preview confirmation (\`org.freedesktop.impl.portal.Wallpaper\` \`show-preview\`)' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle portal show commands' commands "$@"
}
(( $+functions[_wayle__portal__show__access_commands] )) ||
_wayle__portal__show__access_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show access commands' commands "$@"
}
(( $+functions[_wayle__portal__show__account_commands] )) ||
_wayle__portal__show__account_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show account commands' commands "$@"
}
(( $+functions[_wayle__portal__show__app-chooser_commands] )) ||
_wayle__portal__show__app-chooser_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show app-chooser commands' commands "$@"
}
(( $+functions[_wayle__portal__show__color_commands] )) ||
_wayle__portal__show__color_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show color commands' commands "$@"
}
(( $+functions[_wayle__portal__show__dynamic-launcher_commands] )) ||
_wayle__portal__show__dynamic-launcher_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show dynamic-launcher commands' commands "$@"
}
(( $+functions[_wayle__portal__show__file-chooser_commands] )) ||
_wayle__portal__show__file-chooser_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show file-chooser commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help_commands] )) ||
_wayle__portal__show__help_commands() {
    local commands; commands=(
'file-chooser:File open/save dialog (\`org.freedesktop.impl.portal.FileChooser\`)' \
'screenshot:Screenshot capture (\`org.freedesktop.impl.portal.Screenshot\`)' \
'color:Interactive color picker (\`Screenshot.PickColor\`)' \
'print:Printer selection dialog (\`org.freedesktop.impl.portal.Print\`); only the printer/settings prepare step is shown, no document is spooled' \
'screen-cast:Screencast source picker (\`org.freedesktop.impl.portal.ScreenCast\`)' \
'access:Generic grant/deny access prompt (\`org.freedesktop.impl.portal.Access\`)' \
'account:Account info sharing consent (\`org.freedesktop.impl.portal.Account\`)' \
'app-chooser:Application chooser (\`org.freedesktop.impl.portal.AppChooser\`)' \
'dynamic-launcher:Dynamic launcher install confirmation (\`org.freedesktop.impl.portal.DynamicLauncher\`)' \
'wallpaper:Wallpaper preview confirmation (\`org.freedesktop.impl.portal.Wallpaper\` \`show-preview\`)' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle portal show help commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__access_commands] )) ||
_wayle__portal__show__help__access_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help access commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__account_commands] )) ||
_wayle__portal__show__help__account_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help account commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__app-chooser_commands] )) ||
_wayle__portal__show__help__app-chooser_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help app-chooser commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__color_commands] )) ||
_wayle__portal__show__help__color_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help color commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__dynamic-launcher_commands] )) ||
_wayle__portal__show__help__dynamic-launcher_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help dynamic-launcher commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__file-chooser_commands] )) ||
_wayle__portal__show__help__file-chooser_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help file-chooser commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__help_commands] )) ||
_wayle__portal__show__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help help commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__print_commands] )) ||
_wayle__portal__show__help__print_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help print commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__screen-cast_commands] )) ||
_wayle__portal__show__help__screen-cast_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help screen-cast commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__screenshot_commands] )) ||
_wayle__portal__show__help__screenshot_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help screenshot commands' commands "$@"
}
(( $+functions[_wayle__portal__show__help__wallpaper_commands] )) ||
_wayle__portal__show__help__wallpaper_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show help wallpaper commands' commands "$@"
}
(( $+functions[_wayle__portal__show__print_commands] )) ||
_wayle__portal__show__print_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show print commands' commands "$@"
}
(( $+functions[_wayle__portal__show__screen-cast_commands] )) ||
_wayle__portal__show__screen-cast_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show screen-cast commands' commands "$@"
}
(( $+functions[_wayle__portal__show__screenshot_commands] )) ||
_wayle__portal__show__screenshot_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show screenshot commands' commands "$@"
}
(( $+functions[_wayle__portal__show__wallpaper_commands] )) ||
_wayle__portal__show__wallpaper_commands() {
    local commands; commands=()
    _describe -t commands 'wayle portal show wallpaper commands' commands "$@"
}
(( $+functions[_wayle__power_commands] )) ||
_wayle__power_commands() {
    local commands; commands=(
'status:Show current power profile' \
'set:Set power profile' \
'cycle:Cycle to next power profile' \
'list:List available power profiles' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle power commands' commands "$@"
}
(( $+functions[_wayle__power__cycle_commands] )) ||
_wayle__power__cycle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power cycle commands' commands "$@"
}
(( $+functions[_wayle__power__help_commands] )) ||
_wayle__power__help_commands() {
    local commands; commands=(
'status:Show current power profile' \
'set:Set power profile' \
'cycle:Cycle to next power profile' \
'list:List available power profiles' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle power help commands' commands "$@"
}
(( $+functions[_wayle__power__help__cycle_commands] )) ||
_wayle__power__help__cycle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power help cycle commands' commands "$@"
}
(( $+functions[_wayle__power__help__help_commands] )) ||
_wayle__power__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power help help commands' commands "$@"
}
(( $+functions[_wayle__power__help__list_commands] )) ||
_wayle__power__help__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power help list commands' commands "$@"
}
(( $+functions[_wayle__power__help__set_commands] )) ||
_wayle__power__help__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power help set commands' commands "$@"
}
(( $+functions[_wayle__power__help__status_commands] )) ||
_wayle__power__help__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power help status commands' commands "$@"
}
(( $+functions[_wayle__power__list_commands] )) ||
_wayle__power__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power list commands' commands "$@"
}
(( $+functions[_wayle__power__set_commands] )) ||
_wayle__power__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power set commands' commands "$@"
}
(( $+functions[_wayle__power__status_commands] )) ||
_wayle__power__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle power status commands' commands "$@"
}
(( $+functions[_wayle__recorder_commands] )) ||
_wayle__recorder_commands() {
    local commands; commands=(
'start:Start recording' \
'stop:Stop recording' \
'toggle:Toggle recording on/off' \
'pause:Pause the active recording' \
'resume:Resume a paused recording' \
'status:Show current recorder status' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle recorder commands' commands "$@"
}
(( $+functions[_wayle__recorder__help_commands] )) ||
_wayle__recorder__help_commands() {
    local commands; commands=(
'start:Start recording' \
'stop:Stop recording' \
'toggle:Toggle recording on/off' \
'pause:Pause the active recording' \
'resume:Resume a paused recording' \
'status:Show current recorder status' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle recorder help commands' commands "$@"
}
(( $+functions[_wayle__recorder__help__help_commands] )) ||
_wayle__recorder__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder help help commands' commands "$@"
}
(( $+functions[_wayle__recorder__help__pause_commands] )) ||
_wayle__recorder__help__pause_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder help pause commands' commands "$@"
}
(( $+functions[_wayle__recorder__help__resume_commands] )) ||
_wayle__recorder__help__resume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder help resume commands' commands "$@"
}
(( $+functions[_wayle__recorder__help__start_commands] )) ||
_wayle__recorder__help__start_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder help start commands' commands "$@"
}
(( $+functions[_wayle__recorder__help__status_commands] )) ||
_wayle__recorder__help__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder help status commands' commands "$@"
}
(( $+functions[_wayle__recorder__help__stop_commands] )) ||
_wayle__recorder__help__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder help stop commands' commands "$@"
}
(( $+functions[_wayle__recorder__help__toggle_commands] )) ||
_wayle__recorder__help__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder help toggle commands' commands "$@"
}
(( $+functions[_wayle__recorder__pause_commands] )) ||
_wayle__recorder__pause_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder pause commands' commands "$@"
}
(( $+functions[_wayle__recorder__resume_commands] )) ||
_wayle__recorder__resume_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder resume commands' commands "$@"
}
(( $+functions[_wayle__recorder__start_commands] )) ||
_wayle__recorder__start_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder start commands' commands "$@"
}
(( $+functions[_wayle__recorder__status_commands] )) ||
_wayle__recorder__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder status commands' commands "$@"
}
(( $+functions[_wayle__recorder__stop_commands] )) ||
_wayle__recorder__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder stop commands' commands "$@"
}
(( $+functions[_wayle__recorder__toggle_commands] )) ||
_wayle__recorder__toggle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle recorder toggle commands' commands "$@"
}
(( $+functions[_wayle__screenshot_commands] )) ||
_wayle__screenshot_commands() {
    local commands; commands=(
'region:Capture a drag-selected region' \
'output:Capture a whole output (the focused output when NAME is omitted)' \
'window:Capture the active window' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle screenshot commands' commands "$@"
}
(( $+functions[_wayle__screenshot__help_commands] )) ||
_wayle__screenshot__help_commands() {
    local commands; commands=(
'region:Capture a drag-selected region' \
'output:Capture a whole output (the focused output when NAME is omitted)' \
'window:Capture the active window' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle screenshot help commands' commands "$@"
}
(( $+functions[_wayle__screenshot__help__help_commands] )) ||
_wayle__screenshot__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle screenshot help help commands' commands "$@"
}
(( $+functions[_wayle__screenshot__help__output_commands] )) ||
_wayle__screenshot__help__output_commands() {
    local commands; commands=()
    _describe -t commands 'wayle screenshot help output commands' commands "$@"
}
(( $+functions[_wayle__screenshot__help__region_commands] )) ||
_wayle__screenshot__help__region_commands() {
    local commands; commands=()
    _describe -t commands 'wayle screenshot help region commands' commands "$@"
}
(( $+functions[_wayle__screenshot__help__window_commands] )) ||
_wayle__screenshot__help__window_commands() {
    local commands; commands=()
    _describe -t commands 'wayle screenshot help window commands' commands "$@"
}
(( $+functions[_wayle__screenshot__output_commands] )) ||
_wayle__screenshot__output_commands() {
    local commands; commands=()
    _describe -t commands 'wayle screenshot output commands' commands "$@"
}
(( $+functions[_wayle__screenshot__region_commands] )) ||
_wayle__screenshot__region_commands() {
    local commands; commands=()
    _describe -t commands 'wayle screenshot region commands' commands "$@"
}
(( $+functions[_wayle__screenshot__window_commands] )) ||
_wayle__screenshot__window_commands() {
    local commands; commands=()
    _describe -t commands 'wayle screenshot window commands' commands "$@"
}
(( $+functions[_wayle__shell_commands] )) ||
_wayle__shell_commands() {
    local commands; commands=()
    _describe -t commands 'wayle shell commands' commands "$@"
}
(( $+functions[_wayle__systray_commands] )) ||
_wayle__systray_commands() {
    local commands; commands=(
'list:List all system tray items' \
'activate:Activate a tray item by ID' \
'status:Show system tray status' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle systray commands' commands "$@"
}
(( $+functions[_wayle__systray__activate_commands] )) ||
_wayle__systray__activate_commands() {
    local commands; commands=()
    _describe -t commands 'wayle systray activate commands' commands "$@"
}
(( $+functions[_wayle__systray__help_commands] )) ||
_wayle__systray__help_commands() {
    local commands; commands=(
'list:List all system tray items' \
'activate:Activate a tray item by ID' \
'status:Show system tray status' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle systray help commands' commands "$@"
}
(( $+functions[_wayle__systray__help__activate_commands] )) ||
_wayle__systray__help__activate_commands() {
    local commands; commands=()
    _describe -t commands 'wayle systray help activate commands' commands "$@"
}
(( $+functions[_wayle__systray__help__help_commands] )) ||
_wayle__systray__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle systray help help commands' commands "$@"
}
(( $+functions[_wayle__systray__help__list_commands] )) ||
_wayle__systray__help__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle systray help list commands' commands "$@"
}
(( $+functions[_wayle__systray__help__status_commands] )) ||
_wayle__systray__help__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle systray help status commands' commands "$@"
}
(( $+functions[_wayle__systray__list_commands] )) ||
_wayle__systray__list_commands() {
    local commands; commands=()
    _describe -t commands 'wayle systray list commands' commands "$@"
}
(( $+functions[_wayle__systray__status_commands] )) ||
_wayle__systray__status_commands() {
    local commands; commands=()
    _describe -t commands 'wayle systray status commands' commands "$@"
}
(( $+functions[_wayle__toast_commands] )) ||
_wayle__toast_commands() {
    local commands; commands=()
    _describe -t commands 'wayle toast commands' commands "$@"
}
(( $+functions[_wayle__vpn_commands] )) ||
_wayle__vpn_commands() {
    local commands; commands=(
'sso-callback:Hand a browser sign-in callback to the running shell' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle vpn commands' commands "$@"
}
(( $+functions[_wayle__vpn__help_commands] )) ||
_wayle__vpn__help_commands() {
    local commands; commands=(
'sso-callback:Hand a browser sign-in callback to the running shell' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle vpn help commands' commands "$@"
}
(( $+functions[_wayle__vpn__help__help_commands] )) ||
_wayle__vpn__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle vpn help help commands' commands "$@"
}
(( $+functions[_wayle__vpn__help__sso-callback_commands] )) ||
_wayle__vpn__help__sso-callback_commands() {
    local commands; commands=()
    _describe -t commands 'wayle vpn help sso-callback commands' commands "$@"
}
(( $+functions[_wayle__vpn__sso-callback_commands] )) ||
_wayle__vpn__sso-callback_commands() {
    local commands; commands=()
    _describe -t commands 'wayle vpn sso-callback commands' commands "$@"
}
(( $+functions[_wayle__wallpaper_commands] )) ||
_wayle__wallpaper_commands() {
    local commands; commands=(
'set:Set wallpaper from an image file' \
'cycle:Start cycling wallpapers from a directory' \
'stop:Stop wallpaper cycling' \
'next:Skip to next wallpaper' \
'previous:Go back to previous wallpaper' \
'info:Display current wallpaper information' \
'theming-monitor:Set which monitor to use for color extraction' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle wallpaper commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__cycle_commands] )) ||
_wayle__wallpaper__cycle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper cycle commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help_commands] )) ||
_wayle__wallpaper__help_commands() {
    local commands; commands=(
'set:Set wallpaper from an image file' \
'cycle:Start cycling wallpapers from a directory' \
'stop:Stop wallpaper cycling' \
'next:Skip to next wallpaper' \
'previous:Go back to previous wallpaper' \
'info:Display current wallpaper information' \
'theming-monitor:Set which monitor to use for color extraction' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle wallpaper help commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help__cycle_commands] )) ||
_wayle__wallpaper__help__cycle_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper help cycle commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help__help_commands] )) ||
_wayle__wallpaper__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper help help commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help__info_commands] )) ||
_wayle__wallpaper__help__info_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper help info commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help__next_commands] )) ||
_wayle__wallpaper__help__next_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper help next commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help__previous_commands] )) ||
_wayle__wallpaper__help__previous_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper help previous commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help__set_commands] )) ||
_wayle__wallpaper__help__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper help set commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help__stop_commands] )) ||
_wayle__wallpaper__help__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper help stop commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__help__theming-monitor_commands] )) ||
_wayle__wallpaper__help__theming-monitor_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper help theming-monitor commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__info_commands] )) ||
_wayle__wallpaper__info_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper info commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__next_commands] )) ||
_wayle__wallpaper__next_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper next commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__previous_commands] )) ||
_wayle__wallpaper__previous_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper previous commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__set_commands] )) ||
_wayle__wallpaper__set_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper set commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__stop_commands] )) ||
_wayle__wallpaper__stop_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper stop commands' commands "$@"
}
(( $+functions[_wayle__wallpaper__theming-monitor_commands] )) ||
_wayle__wallpaper__theming-monitor_commands() {
    local commands; commands=()
    _describe -t commands 'wayle wallpaper theming-monitor commands' commands "$@"
}
(( $+functions[_wayle__widget_commands] )) ||
_wayle__widget_commands() {
    local commands; commands=(
'update:Push an output update to a bar widget by its config id' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle widget commands' commands "$@"
}
(( $+functions[_wayle__widget__help_commands] )) ||
_wayle__widget__help_commands() {
    local commands; commands=(
'update:Push an output update to a bar widget by its config id' \
'help:Print this message or the help of the given subcommand(s)' \
    )
    _describe -t commands 'wayle widget help commands' commands "$@"
}
(( $+functions[_wayle__widget__help__help_commands] )) ||
_wayle__widget__help__help_commands() {
    local commands; commands=()
    _describe -t commands 'wayle widget help help commands' commands "$@"
}
(( $+functions[_wayle__widget__help__update_commands] )) ||
_wayle__widget__help__update_commands() {
    local commands; commands=()
    _describe -t commands 'wayle widget help update commands' commands "$@"
}
(( $+functions[_wayle__widget__update_commands] )) ||
_wayle__widget__update_commands() {
    local commands; commands=()
    _describe -t commands 'wayle widget update commands' commands "$@"
}

if [ "$funcstack[1]" = "_wayle" ]; then
    _wayle "$@"
else
    compdef _wayle wayle
fi
