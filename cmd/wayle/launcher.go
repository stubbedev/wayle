package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/launcheripc"
	"github.com/stubbedev/wayle/internal/shlex"
	"github.com/stubbedev/wayle/service/launcher"
)

// launcherHelp is wayle/src/cli/launcher/mod.rs HELP, verbatim.
const launcherHelp = `wayle launcher — rofi-compatible application launcher / dmenu

USAGE:
    wayle launcher -show <mode>       open a mode (drun, run, window, ssh, ...)
    wayle launcher -dmenu [...]       dmenu mode: rows from stdin, selection to stdout
    wayle launcher -e <message>       message dialog

Accepts the common rofi option surface (-p, -mesg, -multi-select, -matching,
-location, -drun-*, -window-*, -kb-*, ...). rasi theming options (-theme,
-theme-str) are accepted but ignored — style via wayle-settings instead.
Exit codes match rofi: 0 accept, 1 cancel, 10-28 for kb-custom-N.

GEOMETRY (per invocation, overriding [launcher] in config.toml):
    -width 60        percent of the monitor's width
    -width -30       width in characters
    -width 600px     width in pixels
    -lines N         visible result lines (alias of -l)
    -xoffset N       pixel offset from the anchored edge (needs -location 1-8)
    -yoffset N       pixel offset from the anchored edge (needs -location 1-8)

APPEARANCE (per invocation):
    -font 'Inter 12'   font, as a Pango description
    -style <name>      a named look from [launcher.styles] in config.toml

EVENT HOOKS (a command, run detached; {input} {entry} {mode} {error} are
substituted, one placeholder per argument, and no shell is involved):
    -on-selection-changed <cmd>   the highlighted row changed
    -on-entry-accepted <cmd>      a row (or custom input) was accepted
    -on-mode-changed <cmd>        the active mode changed
    -on-menu-canceled <cmd>       the menu was dismissed
    -on-menu-error <cmd>          the menu could not be built

THUMBNAILS:
    -preview-cmd '<cmd> {input} {output} {size}'
                     makes a row's picture instead of the system's XDG
                     thumbnailers. A row asks for one by prefixing its icon
                     with thumbnail://, e.g.
                     printf 'Name\\0icon\\x1fthumbnail:///path/to/file'
                     filebrowser and recursivebrowser ask for one per file.

MOUSE BINDINGS (-me-* buttons, -ml-* scroll; see -list-keybindings):
    -me-accept-entry MouseDPrimary     accept on a double click only
    -ml-row-down ScrollDown            wheel moves the selection

Local commands: -help, -version, -dump-config, -dump-theme, -list-keybindings`

// launcherDumpTheme is the -dump-theme answer: there are no rasi
// themes.
const launcherDumpTheme = `# wayle does not use rasi themes; the launcher is styled by the
# wayle palette/SCSS system. See ` + "`wayle-settings`" + ` (Launcher page)
# and the [styling] config section.`

// runLauncher is `wayle launcher` (and the whole argv of a `rofi`
// symlink): rofi's flag surface against the shell's launcher socket.
// It returns the process exit code: rofi's 0 accept, 1 cancel, 10-28
// kb-custom-N; a usage error prints "Error: ..." and is 1, as the Rust
// binary does.
func runLauncher(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	inv, err := parseLauncherArgs(args, stderr)
	if err == nil && inv.local != localNone {
		err = runLauncherLocal(inv.local, stdout)
		if err == nil {
			return 0
		}
	}
	if err == nil {
		o := inv.options
		if o.Mode == nil && !o.Dmenu && o.ErrorMessage == nil && o.Modes == nil {
			err = errNothingToShow
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return runLauncherSession(inv, stdin, stdout, stderr)
}

func runLauncherLocal(local launcherLocal, stdout io.Writer) error {
	switch local {
	case localHelp:
		fmt.Fprintln(stdout, launcherHelp)
	case localVersion:
		fmt.Fprintf(stdout, "wayle launcher %s\n", version)
	case localDumpTheme:
		fmt.Fprintln(stdout, launcherDumpTheme)
	case localDumpConfig:
		cfg, err := config.Load()
		if cfg == nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return toml.NewEncoder(stdout).Encode(launcherTOML(cfg.Launcher))
	case localListKeybindings:
		cfg, err := config.Load()
		if cfg == nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		for _, b := range launcher.EffectiveKeybindings(cfg.Launcher.Keybindings) {
			fmt.Fprintf(stdout, "kb-%s: %s\n", b.Action, b.Keys)
		}
		// The mouse bindings share the listing: it is where someone looks
		// to find out what is bound.
		for _, b := range launcher.EffectiveMouseBindings(cfg.Launcher.MouseBindings) {
			fmt.Fprintf(stdout, "%s: %s\n", b.Action, b.Keys)
		}
	}
	return nil
}

// runLauncherSession drives one session: stream rows (dmenu), await the
// terminal frame, print it rofi-style, return the code.
func runLauncherSession(inv launcherInvocation, stdin io.Reader, stdout, stderr io.Writer) int {
	prompt := ""
	if inv.options.Prompt != nil {
		prompt = *inv.options.Prompt
	}
	client, err := launcheripc.Open(inv.options, inv.replace)
	if err != nil {
		fmt.Fprintf(stderr, "wayle launcher: %v\n", err)
		return 1
	}
	// The write half stays open for the whole session, dmenu included:
	// closing it is the EOF the shell reads as the client dying.
	defer func() { _ = client.Close() }()
	if inv.options.Dmenu {
		separator := "\n"
		if inv.rowSeparator != nil {
			separator = *inv.rowSeparator
		}
		go pumpRows(client, inv.inputFile, separator, stdin, stderr)
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	frames := make(chan frameOrErr, 1)
	go func() {
		for {
			f, err := client.NextFrame()
			frames <- frameOrErr{f, err}
			if err != nil || f.Type != launcheripc.FrameOpened {
				return
			}
		}
	}()
	for {
		var fe frameOrErr
		select {
		case <-interrupt:
			return 1
		case fe = <-frames:
		}
		if errors.Is(fe.err, launcheripc.ErrDisconnected) {
			return 1
		}
		if fe.err != nil {
			fmt.Fprintf(stderr, "wayle launcher: %v\n", fe.err)
			return 1
		}
		switch f := fe.frame; f.Type {
		case launcheripc.FrameOpened:
		case launcheripc.FrameBusy:
			fmt.Fprintln(stderr, "wayle launcher: another session is active (use -replace)")
			return 1
		case launcheripc.FrameResult:
			printLauncherResult(stdout, inv.format, f.Selected, f.Filter, prompt)
			return f.Code
		case launcheripc.FrameCancelled:
			return f.Code
		case launcheripc.FrameDump:
			for _, item := range f.Items {
				fmt.Fprintln(stdout, item)
			}
			return 0
		default:
			fmt.Fprintf(stderr, "wayle launcher: launcher socket protocol error: unknown frame %q\n", f.Type)
			return 1
		}
	}
}

type frameOrErr struct {
	frame launcheripc.ServerFrame
	err   error
}

// pumpRows reads the dmenu rows (stdin or -input), splits them on the
// separator, and streams them in chunks, then rows-done. The writer is
// deliberately left open afterwards.
func pumpRows(client *launcheripc.Client, inputFile *string, separator string, stdin io.Reader, stderr io.Writer) {
	var raw []byte
	var err error
	if inputFile != nil {
		raw, err = os.ReadFile(*inputFile)
	} else {
		raw, err = io.ReadAll(stdin)
	}
	var rows []string
	if err != nil {
		fmt.Fprintf(stderr, "wayle launcher: reading input failed: %v\n", err)
	} else {
		rows = splitRows(string(raw), separator)
	}
	for start := 0; start < len(rows); start += launcheripc.RowChunk {
		end := min(start+launcheripc.RowChunk, len(rows))
		if client.SendRows(rows[start:end]) != nil {
			return
		}
	}
	_ = client.FinishRows()
}

// splitRows splits the input on -sep (\n, \0, \t spelled escaped are
// the chars); a trailing separator's empty phantom row is dropped.
func splitRows(raw, separator string) []string {
	switch separator {
	case `\n`:
		separator = "\n"
	case `\0`:
		separator = "\x00"
	case `\t`:
		separator = "\t"
	}
	rows := strings.Split(raw, separator)
	if rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// printLauncherResult is rofi -format: s text, i 0-based index, d
// 1-based, q quoted text, p prompt, f filter, F quoted filter. The
// filter and prompt formats still print once when nothing was selected.
func printLauncherResult(w io.Writer, format string, selected []launcheripc.Selected, filter, prompt string) {
	for _, row := range selected {
		line := row.Text
		switch format {
		case "i":
			line = strconv.FormatInt(row.Index, 10)
		case "d":
			line = strconv.FormatInt(row.Index+1, 10)
		case "q":
			line = shlex.MustQuote(row.Text)
		case "p":
			line = prompt
		case "f":
			line = filter
		case "F":
			line = shlex.MustQuote(filter)
		}
		fmt.Fprintln(w, line)
	}
	if len(selected) == 0 {
		switch format {
		case "p":
			fmt.Fprintln(w, prompt)
		case "F":
			fmt.Fprintln(w, shlex.MustQuote(filter))
		case "f":
			fmt.Fprintln(w, filter)
		}
	}
}

// launcherTOMLDoc is [launcher] as -dump-config prints it: the
// effective values under their config keys, in schema order.
type launcherTOMLDoc struct {
	Location       string            `toml:"location"`
	Width          any               `toml:"width"`
	Lines          uint32            `toml:"lines"`
	Monitor        string            `toml:"monitor"`
	Modes          []string          `toml:"modes"`
	Cycle          bool              `toml:"cycle"`
	Matching       string            `toml:"matching"`
	Tokenize       bool              `toml:"tokenize"`
	NegateChar     string            `toml:"negate-char"`
	NormalizeMatch bool              `toml:"normalize-match"`
	Sort           bool              `toml:"sort"`
	SortingMethod  string            `toml:"sorting-method"`
	Case           string            `toml:"case"`
	Terminal       string            `toml:"terminal"`
	ShowIcons      bool              `toml:"show-icons"`
	IconTheme      string            `toml:"icon-theme"`
	SidebarMode    bool              `toml:"sidebar-mode"`
	AutoSelect     bool              `toml:"auto-select"`
	HoverSelect    bool              `toml:"hover-select"`
	FixedNumLines  bool              `toml:"fixed-num-lines"`
	Font           string            `toml:"font"`
	PreviewCmd     string            `toml:"preview-cmd"`
	DisplayNames   map[string]string `toml:"display-names"`
	Scripts        map[string]string `toml:"scripts"`
	Keybindings    map[string]string `toml:"keybindings"`
	MouseBindings  map[string]string `toml:"mouse-bindings"`
	Styles         map[string]string `toml:"styles"`
	History        struct {
		Enable  bool   `toml:"enable"`
		MaxSize uint32 `toml:"max-size"`
	} `toml:"history"`
	Drun struct {
		Categories        []string `toml:"categories"`
		ExcludeCategories []string `toml:"exclude-categories"`
		MatchFields       []string `toml:"match-fields"`
		DisplayFormat     string   `toml:"display-format"`
		ShowActions       bool     `toml:"show-actions"`
		URLLauncher       string   `toml:"url-launcher"`
	} `toml:"drun"`
	Run struct {
		RunCommand   string `toml:"run-command"`
		ShellCommand string `toml:"shell-command"`
		ListCommand  string `toml:"list-command"`
	} `toml:"run"`
	Window struct {
		Format        string   `toml:"format"`
		MatchFields   []string `toml:"match-fields"`
		HideActive    bool     `toml:"hide-active"`
		CloseOnDelete bool     `toml:"close-on-delete"`
	} `toml:"window"`
	SSH struct {
		Client          string `toml:"client"`
		Command         string `toml:"command"`
		ParseHosts      bool   `toml:"parse-hosts"`
		ParseKnownHosts bool   `toml:"parse-known-hosts"`
	} `toml:"ssh"`
	Filebrowser struct {
		Directory        string `toml:"directory"`
		SortingMethod    string `toml:"sorting-method"`
		DirectoriesFirst bool   `toml:"directories-first"`
		ShowHidden       bool   `toml:"show-hidden"`
		Command          string `toml:"command"`
	} `toml:"filebrowser"`
	Combi struct {
		Modes         []string `toml:"modes"`
		DisplayFormat string   `toml:"display-format"`
	} `toml:"combi"`
}

func stringsOf[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

func launcherTOML(l config.LauncherConfig) launcherTOMLDoc {
	var d launcherTOMLDoc
	d.Location, d.Lines, d.Monitor, d.Modes, d.Cycle = string(l.Location), l.Lines, l.Monitor, l.Modes, l.Cycle
	d.Width = l.Width.Value
	if l.Width.Unit == config.SizePixels {
		d.Width = strconv.FormatFloat(l.Width.Value, 'f', -1, 64) + "px"
	}
	d.Matching, d.Tokenize, d.NegateChar, d.NormalizeMatch = string(l.Matching), l.Tokenize, l.NegateChar, l.NormalizeMatch
	d.Sort, d.SortingMethod, d.Case, d.Terminal = l.Sort, string(l.SortingMethod), string(l.Case), l.Terminal
	d.ShowIcons, d.IconTheme, d.SidebarMode, d.AutoSelect = l.ShowIcons, l.IconTheme, l.SidebarMode, l.AutoSelect
	d.HoverSelect, d.FixedNumLines, d.Font, d.PreviewCmd = l.HoverSelect, l.FixedNumLines, l.Font, l.PreviewCmd
	d.DisplayNames, d.Scripts, d.Keybindings, d.MouseBindings, d.Styles = l.DisplayNames, l.Scripts, l.Keybindings, l.MouseBindings, l.Styles
	d.History.Enable, d.History.MaxSize = l.History.Enable, l.History.MaxSize
	d.Drun.Categories, d.Drun.ExcludeCategories = l.Drun.Categories, l.Drun.ExcludeCategories
	d.Drun.MatchFields, d.Drun.DisplayFormat = stringsOf(l.Drun.MatchFields), l.Drun.DisplayFormat
	d.Drun.ShowActions, d.Drun.URLLauncher = l.Drun.ShowActions, l.Drun.URLLauncher
	d.Run.RunCommand, d.Run.ShellCommand, d.Run.ListCommand = l.Run.RunCommand, l.Run.ShellCommand, l.Run.ListCommand
	d.Window.Format, d.Window.MatchFields = l.Window.Format, stringsOf(l.Window.MatchFields)
	d.Window.HideActive, d.Window.CloseOnDelete = l.Window.HideActive, l.Window.CloseOnDelete
	d.SSH.Client, d.SSH.Command, d.SSH.ParseHosts, d.SSH.ParseKnownHosts = l.SSH.Client, l.SSH.Command, l.SSH.ParseHosts, l.SSH.ParseKnownHosts
	d.Filebrowser.Directory, d.Filebrowser.SortingMethod = l.Filebrowser.Directory, string(l.Filebrowser.SortingMethod)
	d.Filebrowser.DirectoriesFirst, d.Filebrowser.ShowHidden, d.Filebrowser.Command = l.Filebrowser.DirectoriesFirst, l.Filebrowser.ShowHidden, l.Filebrowser.Command
	d.Combi.Modes, d.Combi.DisplayFormat = l.Combi.Modes, l.Combi.DisplayFormat
	return d
}

// launcherCommand is app.rs's Launcher: every argument, flags
// included, passes through raw to the rofi-compatible parser, and the
// session's rofi exit code is the process's.
func launcherCommand() *cli.Command {
	return &cli.Command{
		Name:            "launcher",
		About:           "Application launcher / dmenu with rofi-compatible flags",
		DisableHelpFlag: true,
		Args: []*cli.Arg{
			{ID: "args", Multiple: true, TrailingVarArg: true, Help: "Raw rofi-style args (single-dash long flags, e.g. `-show drun`)"},
		},
		Run: func(m *cli.Matches) error {
			args := cli.Values[string](m, "args")
			if code := runLauncher(args, os.Stdin, m.Stdout(), m.Stderr()); code != 0 {
				return cli.ExitError{Code: code}
			}
			return nil
		},
	}
}
