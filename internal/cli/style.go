package cli

import (
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// style is one anstyle Style from wayle/src/cli/app.rs's get_styles:
// an optional bold effect and an optional ANSI foreground. It renders
// the way anstyle does (effects first, then the color, one SGR each).
type style struct {
	bold bool
	fg   int // 30-37, 0 for none
}

// The palette from app.rs's get_styles. clap's context styles stay
// plain there, so the [possible values: ...] / [default: ...] spec
// values carry no color.
var (
	styleHeader      = style{bold: true, fg: 33}
	styleError       = style{bold: true, fg: 31}
	styleUsage       = style{bold: true, fg: 32}
	styleLiteral     = style{bold: true, fg: 32}
	stylePlaceholder = style{fg: 32}
	styleValid       = style{bold: true, fg: 32}
	styleInvalid     = style{bold: true, fg: 33}
)

// wrap renders text inside the style; a plain style adds nothing.
func (s style) wrap(text string) string {
	if s == (style{}) {
		return text
	}
	var b strings.Builder
	if s.bold {
		b.WriteString("\x1b[1m")
	}
	if s.fg != 0 {
		b.WriteString("\x1b[")
		b.WriteString(strconv.Itoa(s.fg))
		b.WriteString("m")
	}
	b.WriteString(text)
	b.WriteString("\x1b[0m")
	return b.String()
}

// strip drops every ANSI escape sequence (anstream's StripStream),
// including raw ones embedded in help text such as the icons
// after_long_help's styled "Examples:" header.
func strip(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		i++ // a two-byte escape
	}
	return b.String()
}

// displayWidth is the rendered width: the rune count with the styling
// removed (clap without the unicode feature counts chars).
func displayWidth(s string) int { return utf8.RuneCountInString(strip(s)) }

// ColorEnabled reports whether output to f should be colored, with
// anstream's auto choice: NO_COLOR wins, then CLICOLOR_FORCE, then
// CLICOLOR=0; otherwise a terminal colors when TERM is set to
// something other than dumb, CLICOLOR is on, or CI is set.
func ColorEnabled(f *os.File) bool {
	return colorChoice(isTerminal(f), os.LookupEnv)
}

func colorChoice(terminal bool, lookup func(string) (string, bool)) bool {
	if v, ok := lookup("NO_COLOR"); ok && v != "" {
		return false
	}
	if v, ok := lookup("CLICOLOR_FORCE"); ok && v != "0" {
		return true
	}
	clicolor, clicolorSet := lookup("CLICOLOR")
	if clicolorSet && clicolor == "0" {
		return false
	}
	if !terminal {
		return false
	}
	term, termSet := lookup("TERM")
	_, ci := lookup("CI")
	return (termSet && term != "dumb") || clicolorSet || ci
}

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}
