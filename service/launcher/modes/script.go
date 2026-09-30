// Package modes holds the launch modes (crates/wayle-launcher/src/
// modes): drun, run, window, ssh, the file browsers, script and dmenu,
// combi, and the wayle-only calc, emoji, clipboard, and keys modes.
package modes

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/service/launcher"
)

// Script is a custom script mode speaking the rofi-script(5) protocol
// (script.rs): the script runs with no argument to produce the list,
// and again with the selected entry as argv[1] on every interaction.
// ROFI_RETV says why (0 init, 1 select, 2 custom, 3 delete, 10..28
// kb-custom-N); ROFI_INFO, ROFI_DATA, and ROFI_INPUT carry the row's
// info, script-owned state, and the query. Header lines
// (\0key\x1fvalue) set mode options; row suffixes
// (text\0icon\x1fname\x1f...) set row metadata.
type Script struct {
	name   string
	script string
	// data is \0data\x1f... state carried between runs (ROFI_DATA).
	data    *string
	hotKeys bool
	// texts and infos parallel the items: what the script gets back.
	texts []string
	infos []*string
}

// NewScript is a script mode named name running script.
func NewScript(name, script string) *Script { return &Script{name: name, script: script} }

// Name is the configured mode name.
func (s *Script) Name() string { return s.name }

// scriptResult is the outcome of one run.
type scriptResult interface{ isScriptResult() }

type (
	scriptEmpty  struct{}
	scriptState  struct{ state launcher.ModeState }
	scriptSwitch struct{ mode string }
)

func (scriptEmpty) isScriptResult()  {}
func (scriptState) isScriptResult()  {}
func (scriptSwitch) isScriptResult() {}

// action is what a post-selection run means: no output closes (rofi),
// rows reload, switch-mode switches.
func action(r scriptResult) launcher.Action {
	switch r := r.(type) {
	case scriptState:
		return launcher.ActionReload{State: r.state}
	case scriptSwitch:
		return launcher.ActionSwitchMode{Name: r.mode}
	}
	return launcher.ActionClose{}
}

// invoke runs the script and turns its stdout into the next result.
func (s *Script) invoke(ctx context.Context, arg *string, retv int, info *string, input string) scriptResult {
	var args []string
	if arg != nil {
		args = append(args, *arg)
	}
	cmd := exec.CommandContext(ctx, s.script, args...) //nolint:gosec // the user's own script mode
	cmd.Env = append(os.Environ(), "ROFI_RETV="+strconv.Itoa(retv), "ROFI_INPUT="+input)
	if info != nil {
		cmd.Env = append(cmd.Env, "ROFI_INFO="+*info)
	}
	if s.data != nil {
		cmd.Env = append(cmd.Env, "ROFI_DATA="+*s.data)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// A non-zero exit still hands back its output, as tokio's
		// output() does; only a failure to run at all is empty.
		if exitErr := (*exec.ExitError)(nil); !errors.As(err, &exitErr) {
			log.Printf("launcher: script mode %s exec failed: %v", s.script, err)
			return scriptEmpty{}
		}
	}
	parsed := parseOutput(strings.ToValidUTF8(stdout.String(), "�"))
	if parsed.data != nil {
		s.data = parsed.data
	}
	if parsed.useHotKeys {
		s.hotKeys = true
	}
	if parsed.switchMode != nil {
		return scriptSwitch{mode: *parsed.switchMode}
	}
	if len(parsed.rows) == 0 {
		return scriptEmpty{}
	}
	return scriptState{state: s.buildState(parsed)}
}

func (s *Script) buildState(p parsedOutput) launcher.ModeState {
	items := make([]launcher.Item, len(p.rows))
	s.texts = make([]string, len(p.rows))
	s.infos = make([]*string, len(p.rows))
	for i, row := range p.rows {
		it := row.item
		if containsIndex(p.urgent, i) {
			it.Flags |= launcher.FlagUrgent
		}
		if containsIndex(p.active, i) {
			it.Flags |= launcher.FlagActive
		}
		items[i] = it
		s.texts[i] = row.text
		s.infos[i] = it.Info
	}
	prompt := s.name
	if p.prompt != nil {
		prompt = *p.prompt
	}
	return launcher.ModeState{
		Items:         items,
		Prompt:        prompt,
		Message:       p.message,
		MarkupRows:    p.markupRows,
		NoCustom:      p.noCustom,
		UseHotKeys:    s.hotKeys,
		KeepSelection: p.keepSelection,
		NewSelection:  p.newSelection,
		KeepFilter:    p.keepFilter,
	}
}

func containsIndex(list []uint32, i int) bool {
	for _, v := range list {
		if int(v) == i {
			return true
		}
	}
	return false
}

// row returns the entry text and info behind a target.
func (s *Script) row(target launcher.Target) (*string, *string) {
	i, ok := target.Row()
	if !ok || int(i) >= len(s.texts) {
		return nil, nil
	}
	return &s.texts[i], s.infos[i]
}

// Load runs the script for its initial list. An initially empty script
// is an empty list; closing only applies after a selection.
func (s *Script) Load(ctx context.Context) launcher.ModeState {
	if st, ok := s.invoke(ctx, nil, 0, nil, "").(scriptState); ok {
		return st.state
	}
	return launcher.ModeState{Prompt: s.name}
}

// Activate re-runs the script with the accepted entry.
func (s *Script) Activate(ctx context.Context, target launcher.Target, kind launcher.ActivateKind, input string) launcher.Action {
	text, info := s.row(target)
	var arg string
	var retv int
	switch k := kind.(type) {
	case launcher.ActivateCustom:
		arg, retv = k.Text, 2
	case launcher.ActivateKbCustom:
		if !s.hotKeys {
			return launcher.ActionNothing{}
		}
		arg, retv = input, 9+int(k.N)
		if text != nil {
			arg = *text
		}
	default:
		if text == nil {
			return launcher.ActionNothing{}
		}
		arg, retv = *text, 1
	}
	return action(s.invoke(ctx, &arg, retv, info, input))
}

// Delete re-runs the script with ROFI_RETV=3 for the row.
func (s *Script) Delete(ctx context.Context, index uint32) launcher.Action {
	text, info := s.row(launcher.RowTarget(index))
	if text == nil {
		return launcher.ActionNothing{}
	}
	arg := *text
	return action(s.invoke(ctx, &arg, 3, info, ""))
}

// parsedRow is one parsed row: the item plus the original entry text
// (what a script or dmenu consumer gets back).
type parsedRow struct {
	item launcher.Item
	text string
}

type parsedOutput struct {
	rows          []parsedRow
	prompt        *string
	message       *string
	markupRows    bool
	noCustom      bool
	useHotKeys    bool
	keepSelection bool
	keepFilter    bool
	newSelection  *uint32
	data          *string
	switchMode    *string
	urgent        []uint32
	active        []uint32
}

// parseOutput parses a script's whole stdout: header lines and rows.
// A \0delim\x1fX header changes the row separator; it is found first.
func parseOutput(stdout string) parsedOutput {
	var p parsedOutput
	delim := "\n"
	for line := range strings.SplitSeq(stdout, "\n") {
		if v, ok := strings.CutPrefix(line, "\x00delim\x1f"); ok {
			if d, ok := unescapeDelim(strings.TrimRight(v, "\n")); ok {
				delim = d
			}
			break
		}
	}
	for entry := range strings.SplitSeq(stdout, delim) {
		entry = strings.TrimSuffix(entry, "\n")
		if entry == "" {
			continue
		}
		if header, ok := strings.CutPrefix(entry, "\x00"); ok {
			applyHeader(header, &p)
		} else {
			p.rows = append(p.rows, parseRow(entry))
		}
	}
	return p
}

func applyHeader(header string, p *parsedOutput) {
	key, value, _ := strings.Cut(header, "\x1f")
	truthy := value == "true"
	switch key {
	case "prompt":
		p.prompt = &value
	case "message":
		p.message = &value
	case "markup-rows":
		p.markupRows = truthy
	case "no-custom":
		p.noCustom = truthy
	case "use-hot-keys":
		p.useHotKeys = truthy
	case "keep-selection":
		p.keepSelection = truthy
	case "keep-filter":
		p.keepFilter = truthy
	case "new-selection":
		if n, err := strconv.ParseUint(value, 10, 32); err == nil {
			v := uint32(n)
			p.newSelection = &v
		} else {
			p.newSelection = nil
		}
	case "data":
		p.data = &value
	case "switch-mode":
		p.switchMode = &value
	case "urgent":
		p.urgent = ParseRanges(value)
	case "active":
		p.active = ParseRanges(value)
	case "delim":
		// handled up front
	case "theme":
		// No rasi: accepted and ignored (a documented wayle divergence).
	default:
		log.Printf("launcher: unknown script mode option %q", key)
	}
}

// parseRow parses text[\0key\x1fvalue\x1f...] into an item.
func parseRow(entry string) parsedRow {
	text, options, hasOptions := strings.Cut(entry, "\x00")
	item := launcher.NewItem(text)
	if hasOptions {
		parts := strings.Split(options, "\x1f")
		for i := 0; i+1 < len(parts); i += 2 {
			applyRowOption(&item, text, parts[i], parts[i+1])
		}
	}
	return parsedRow{item: item, text: text}
}

// ParseIcon reads one icon value: a thumbnail:// request, an absolute
// path, or a theme name.
func ParseIcon(value string) launcher.Icon {
	if file, ok := strings.CutPrefix(value, launcher.ThumbnailScheme); ok {
		return launcher.IconThumbnail{Path: file, Fallback: launcher.MimeIcon(file)}
	}
	if strings.HasPrefix(value, "/") {
		return launcher.IconFile(value)
	}
	return launcher.IconName(value)
}

func applyRowOption(item *launcher.Item, text, key, value string) {
	truthy := value == "true"
	switch key {
	case "icon":
		// Comma-separated icon values are fallbacks; the first wins.
		first, _, _ := strings.Cut(value, ",")
		item.Icon = ParseIcon(first)
	case "display":
		item.Display = value
	case "meta":
		item.MatchText = text + " " + value
	case "info":
		v := value
		item.Info = &v
	case "nonselectable":
		if truthy {
			item.Flags |= launcher.FlagNonselectable
		}
	case "permanent":
		if truthy {
			item.Flags |= launcher.FlagPermanent
		}
	case "urgent":
		if truthy {
			item.Flags |= launcher.FlagUrgent
		}
	case "active":
		if truthy {
			item.Flags |= launcher.FlagActive
		}
	}
}

// ParseRanges reads a dmenu-style index list, "1,3-5,8"; parts that do
// not parse are skipped.
func ParseRanges(raw string) []uint32 {
	var out []uint32
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if start, end, isRange := strings.Cut(part, "-"); isRange {
			a, errA := strconv.ParseUint(strings.TrimSpace(start), 10, 32)
			b, errB := strconv.ParseUint(strings.TrimSpace(end), 10, 32)
			if errA == nil && errB == nil {
				for v := a; v <= b; v++ {
					out = append(out, uint32(v))
				}
			}
			continue
		}
		if v, err := strconv.ParseUint(part, 10, 32); err == nil {
			out = append(out, uint32(v))
		}
	}
	return out
}

func unescapeDelim(v string) (string, bool) {
	switch v {
	case `\n`:
		return "\n", true
	case `\0`:
		return "\x00", true
	case `\t`:
		return "\t", true
	}
	for _, r := range v {
		return string(r), true
	}
	return "", false
}
