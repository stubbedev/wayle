package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/internal/launcheripc"
	"github.com/stubbedev/wayle/service/launcher/modes"
)

// launcherLocal is a command resolved entirely in the CLI.
type launcherLocal uint8

// Local commands (wayle/src/cli/launcher/args.rs LocalCmd).
const (
	localNone launcherLocal = iota
	localHelp
	localVersion
	localDumpConfig
	localDumpTheme
	localListKeybindings
)

// launcherInvocation is a parsed `wayle launcher` command line.
type launcherInvocation struct {
	options launcheripc.SessionOptions
	// replace is -replace: displace a live session.
	replace bool
	// format is -format, applied CLI-side (dmenu output).
	format string
	// inputFile is -input: rows from a file instead of stdin.
	inputFile *string
	// rowSeparator is -sep, rows split CLI-side.
	rowSeparator *string
	local        launcherLocal
}

// ignoredWithValue are value flags accepted and ignored (X11-only, rasi
// theming, or daemon-irrelevant); each warns once.
var ignoredWithValue = []string{
	"config", "pid", "dpi", "w", "scroll-method", "eh", "refilter-timeout-limit", "threads",
	"cache-dir", "max-history-size", "wayland-layer", "display", "async-pre-read",
}

// ignoredBare are bare flags accepted and ignored.
var ignoredBare = []string{
	"no-lazy-grab", "normal-window", "transient-window", "steal-focus", "no-steal-focus",
	"click-to-exit", "no-click-to-exit", "xserver-i300-workaround", "no-config",
	"drun-use-desktop-cache", "drun-reload-desktop-cache",
}

// parseLauncherArgs maps rofi's single-dash long flags (args.rs parse).
// Session options travel to the shell, unsupported flags warn on
// stderr and are skipped (friendlier than rofi's hard error for a
// shim), and a few are handled locally. A value flag missing its value
// is a usage error, as is a -width that is not a number.
func parseLauncherArgs(args []string, stderr io.Writer) (launcherInvocation, error) {
	inv := launcherInvocation{format: "s"}
	opts := &inv.options
	for i := 0; i < len(args); i++ {
		flag := strings.TrimLeft(args[i], "-")
		if flag == "" {
			continue
		}
		value := func(name string) (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("-%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		str := func(name string, dst **string) error {
			v, err := value(name)
			if err == nil {
				*dst = &v
			}
			return err
		}
		list := func(name string, dst *[]string) error {
			v, err := value(name)
			if err == nil {
				*dst = splitList(v)
			}
			return err
		}
		var err error
		switch flag {
		// ---- local ----
		case "help", "h":
			inv.local = localHelp
		case "version", "v":
			inv.local = localVersion
		case "dump-config":
			inv.local = localDumpConfig
		case "dump-theme":
			inv.local = localDumpTheme
		case "list-keybindings":
			inv.local = localListKeybindings
		case "replace":
			inv.replace = true
		case "format":
			var v string
			if v, err = value("format"); err == nil {
				inv.format = v
			}
		case "input":
			err = str("input", &inv.inputFile)

		// ---- session: mode selection ----
		case "show":
			err = str("show", &opts.Mode)
		case "modes", "modi":
			err = list("modes", &opts.Modes)
		case "dmenu":
			opts.Dmenu = true
		case "e":
			err = str("e", &opts.ErrorMessage)

		// ---- session: display ----
		case "p":
			err = str("p", &opts.Prompt)
		case "l", "lines":
			opts.Lines, err = parsedU32(value(flag))
		case "width":
			var v string
			if v, err = value("width"); err == nil {
				var w launcheripc.Width
				if w, err = launcheripc.ParseWidth(v); err == nil {
					opts.Width = &w
				}
			}
		case "xoffset":
			opts.XOffset, err = parsedI32(value("xoffset"))
		case "yoffset":
			opts.YOffset, err = parsedI32(value("yoffset"))
		case "mesg":
			err = str("mesg", &opts.Mesg)
		case "filter":
			err = str("filter", &opts.Filter)
		case "select":
			err = str("select", &opts.Select)
		case "selected-row":
			opts.SelectedRow, err = parsedU32(value("selected-row"))
		case "window-title":
			err = str("window-title", &opts.WindowTitle)
		case "location":
			var v string
			if v, err = value("location"); err == nil {
				opts.Location = nil
				if n, perr := strconv.ParseUint(v, 10, 8); perr == nil {
					u := uint8(n)
					opts.Location = &u
				}
			}
		case "monitor", "m":
			err = str("monitor", &opts.Monitor)
		case "fixed-num-lines":
			opts.NoFixedNumLines = false
		case "no-fixed-num-lines":
			opts.NoFixedNumLines = true
		case "sidebar-mode", "no-sidebar-mode":
			opts.SidebarMode = new(flag == "sidebar-mode")
		case "cycle", "no-cycle":
			opts.Cycle = new(flag == "cycle")
		case "auto-select", "no-auto-select":
			opts.AutoSelect = new(flag == "auto-select")
		case "hover-select", "no-hover-select":
			opts.HoverSelect = new(flag == "hover-select")
		case "show-icons", "no-show-icons":
			opts.ShowIcons = new(flag == "show-icons")
		case "icon-theme":
			err = str("icon-theme", &opts.IconTheme)

		// ---- session: dmenu ----
		case "i":
			opts.CaseInsensitive = new(true)
		case "sep":
			err = str("sep", &inv.rowSeparator)
		case "multi-select":
			opts.MultiSelect = true
		case "only-match":
			opts.OnlyMatch = true
		case "no-custom":
			opts.NoCustom = true
		case "password":
			opts.Password = true
		case "markup-rows", "markup":
			opts.MarkupRows = true
		case "sync":
			opts.Sync = true
		case "dump":
			opts.Dump = true
		case "u":
			var v string
			if v, err = value("u"); err == nil {
				opts.Urgent = nonNil(modes.ParseRanges(v))
			}
		case "a":
			var v string
			if v, err = value("a"); err == nil {
				opts.Active = nonNil(modes.ParseRanges(v))
			}
		case "ballot-selected-str":
			err = str("ballot-selected-str", &opts.BallotSelected)
		case "ballot-unselected-str":
			err = str("ballot-unselected-str", &opts.BallotUnselected)
		case "display-columns":
			var v string
			if v, err = value("display-columns"); err == nil {
				opts.DisplayColumns = []uint32{}
				for part := range strings.SplitSeq(v, ",") {
					if n, perr := strconv.ParseUint(strings.TrimSpace(part), 10, 32); perr == nil {
						opts.DisplayColumns = append(opts.DisplayColumns, uint32(n))
					}
				}
			}
		case "display-column-separator":
			err = str("display-column-separator", &opts.DisplayColumnSeparator)
		case "ellipsize-mode":
			err = str("ellipsize-mode", &opts.EllipsizeMode)
		case "keep-right":
			opts.KeepRight = true

		// ---- session: matching ----
		case "matching":
			err = str("matching", &opts.Matching)
		case "tokenize", "no-tokenize":
			opts.Tokenize = new(flag == "tokenize")
		case "matching-negate-char":
			var v string
			if v, err = value("matching-negate-char"); err == nil {
				opts.NegateChar = nil
				for _, r := range v {
					opts.NegateChar = new(string(r))
					break
				}
			}
		case "normalize-match", "no-normalize-match":
			opts.NormalizeMatch = new(flag == "normalize-match")
		case "sort", "no-sort":
			opts.Sort = new(flag == "sort")
		case "sorting-method":
			err = str("sorting-method", &opts.SortingMethod)
		case "case-sensitive", "no-case-sensitive":
			opts.CaseSensitive = new(flag == "case-sensitive")
		case "case-smart", "no-case-smart":
			opts.CaseSmart = new(flag == "case-smart")

		// ---- session: per-mode ----
		case "terminal":
			err = str("terminal", &opts.Terminal)
		case "run-command":
			err = str("run-command", &opts.RunCommand)
		case "run-shell-command":
			err = str("run-shell-command", &opts.RunShellCommand)
		case "run-list-command":
			err = str("run-list-command", &opts.RunListCommand)
		case "ssh-client":
			err = str("ssh-client", &opts.SSHClient)
		case "ssh-command":
			err = str("ssh-command", &opts.SSHCommand)
		case "parse-hosts", "no-parse-hosts":
			opts.ParseHosts = new(flag == "parse-hosts")
		case "parse-known-hosts", "no-parse-known-hosts":
			opts.ParseKnownHosts = new(flag == "parse-known-hosts")
		case "window-format":
			err = str("window-format", &opts.WindowFormat)
		case "window-command":
			err = str("window-command", &opts.WindowCommand)
		case "window-match-fields":
			err = list("window-match-fields", &opts.WindowMatchFields)
		case "window-hide-active-window", "hide-active-window":
			opts.HideActiveWindow = new(true)
		case "drun-categories":
			err = list("drun-categories", &opts.DrunCategories)
		case "drun-exclude-categories":
			err = list("drun-exclude-categories", &opts.DrunExcludeCategories)
		case "drun-match-fields":
			err = list("drun-match-fields", &opts.DrunMatchFields)
		case "drun-display-format":
			err = str("drun-display-format", &opts.DrunDisplayFormat)
		case "drun-show-actions", "no-drun-show-actions":
			opts.DrunShowActions = new(flag == "drun-show-actions")
		case "drun-url-launcher":
			err = str("drun-url-launcher", &opts.DrunURLLauncher)
		case "application-fallback-icon":
			err = str("application-fallback-icon", &opts.ApplicationFallbackIcon)
		case "ignored-prefixes":
			err = list("ignored-prefixes", &opts.IgnoredPrefixes)
		case "combi-modes", "combi-modi":
			err = list("combi-modes", &opts.CombiModes)
		case "combi-display-format":
			err = str("combi-display-format", &opts.CombiDisplayFormat)

		// ---- session: event hooks (the value is the command) ----
		case "on-selection-changed":
			err = str("on-selection-changed", &opts.OnSelectionChanged)
		case "on-entry-accepted":
			err = str("on-entry-accepted", &opts.OnEntryAccepted)
		case "on-mode-changed":
			err = str("on-mode-changed", &opts.OnModeChanged)
		case "on-menu-canceled", "on-menu-cancelled":
			err = str(flag, &opts.OnMenuCanceled)
		case "on-menu-error":
			err = str("on-menu-error", &opts.OnMenuError)
		case "on-screenshot-taken":
			// The launcher takes no screenshots: say so, but consume the
			// value or it would be read as the next flag.
			_, _ = value(flag)
			fmt.Fprintf(stderr, "wayle launcher: -%s is ignored — the launcher takes no screenshots\n", flag)

		// ---- session: appearance ----
		case "font":
			err = str("font", &opts.Font)
		case "style":
			err = str("style", &opts.Style)
		case "preview-cmd":
			err = str("preview-cmd", &opts.PreviewCmd)
		case "completer-mode":
			err = str("completer-mode", &opts.CompleterMode)

		// ---- accepted + ignored ----
		case "plugin-path", "plugins", "no-plugins":
			if flag == "plugin-path" {
				_, _ = value(flag)
			}
			fmt.Fprintf(stderr, "wayle launcher: -%s is ignored — wayle has no plugin ABI; use a script mode (-modes 'name:/path/to/script' or [launcher.scripts])\n", flag)
		case "theme", "theme-str":
			_, _ = value(flag)
			fmt.Fprintf(stderr, "wayle launcher: -%s is ignored — wayle has no rasi themes; style the launcher with [styling] in config.toml\n", flag)

		default:
			switch {
			case strings.HasPrefix(flag, "kb-"):
				var v string
				if v, err = value(flag); err == nil {
					if opts.KbOverrides == nil {
						opts.KbOverrides = map[string]string{}
					}
					opts.KbOverrides[strings.TrimPrefix(flag, "kb-")] = v
				}
			// me- (button) and ml- (scroll) keep their prefix: the two
			// namespaces have their own action names.
			case strings.HasPrefix(flag, "me-") || strings.HasPrefix(flag, "ml-"):
				var v string
				if v, err = value(flag); err == nil {
					if opts.MouseOverrides == nil {
						opts.MouseOverrides = map[string]string{}
					}
					opts.MouseOverrides[flag] = v
				}
			case strings.HasPrefix(flag, "display-"):
				var v string
				if v, err = value(flag); err == nil {
					if opts.DisplayNames == nil {
						opts.DisplayNames = map[string]string{}
					}
					opts.DisplayNames[strings.TrimPrefix(flag, "display-")] = v
				}
			case slices.Contains(ignoredWithValue, flag):
				_, _ = value(flag)
				fmt.Fprintf(stderr, "wayle launcher: -%s is not supported by wayle and was ignored\n", flag)
			case slices.Contains(ignoredBare, flag):
				fmt.Fprintf(stderr, "wayle launcher: -%s is not supported by wayle and was ignored\n", flag)
			default:
				fmt.Fprintf(stderr, "wayle launcher: unknown option -%s (skipped)\n", flag)
			}
		}
		if err != nil {
			return inv, err
		}
	}
	warnCenteredOffsets(&inv.options, stderr)
	return inv, nil
}

// parsedU32 is a value flag parsed with `.parse().ok()`: a bad number
// is no value, a missing one the usage error.
func parsedU32(v string, err error) (*uint32, error) {
	if err != nil {
		return nil, err
	}
	n, perr := strconv.ParseUint(v, 10, 32)
	if perr != nil {
		return nil, nil
	}
	u := uint32(n)
	return &u, nil
}

func parsedI32(v string, err error) (*int32, error) {
	if err != nil {
		return nil, err
	}
	n, perr := strconv.ParseInt(v, 10, 32)
	if perr != nil {
		return nil, nil
	}
	i := int32(n)
	return &i, nil
}

func nonNil(v []uint32) []uint32 {
	if v == nil {
		return []uint32{}
	}
	return v
}

// warnCenteredOffsets: -xoffset/-yoffset are margins from the anchored
// edge, and a centered axis has none - the layer-shell protocol ignores
// the margin. Only an explicit -location 0 is diagnosed; an absent one
// means the configured location, which the CLI cannot see.
func warnCenteredOffsets(opts *launcheripc.SessionOptions, stderr io.Writer) {
	if opts.Location == nil || *opts.Location != 0 {
		return
	}
	if opts.XOffset != nil || opts.YOffset != nil {
		fmt.Fprintln(stderr, "wayle launcher: -xoffset/-yoffset have no effect with -location 0 (centered); pick an edge location (1-8) to offset from")
	}
}

// splitList splits a rofi list flag on commas (and `#`, which modes
// also accept), trimming and dropping empty parts.
func splitList(raw string) []string {
	var out []string
	for part := range strings.FieldsFuncSeq(raw, func(r rune) bool { return r == ',' || r == '#' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// errNothingToShow is a session request with no mode, dmenu, or -e.
var errNothingToShow = errors.New("nothing to show: pass -show <mode>, -dmenu, or -e <message>")
