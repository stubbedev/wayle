package bar

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/jinja"
)

// Custom module limits (watchers/command.rs, events.rs, helpers.rs).
const (
	commandTimeout     = 30 * time.Second
	customScrollWait   = 50 * time.Millisecond
	maxCustomJSONBytes = 64 * 1024
)

// parsedOutput is helpers.rs's ParsedOutput: the trimmed raw output,
// the reserved JSON fields, and the JSON itself for the template.
type parsedOutput struct {
	raw        string
	text       *string
	alt        *string
	percentage *int
	tooltip    *string
	class      []string
	json       any
}

// parseCustomOutput is ParsedOutput::parse: output starting with "{"
// or "[" (at most 64 KiB) that is valid JSON parses; anything else is
// raw text. The reserved fields decode all together as serde would, so
// one of the wrong type (a non-string text, a fractional percentage)
// leaves them all unset.
func parseCustomOutput(out string) parsedOutput {
	trimmed := strings.TrimSpace(out)
	p := parsedOutput{raw: trimmed}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") || len(trimmed) > maxCustomJSONBytes {
		return p
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil || dec.More() {
		return p
	}
	p.json = jsonValue(doc)
	if m, ok := p.json.(map[string]any); ok {
		applyReserved(&p, m)
	}
	return p
}

// applyReserved is serde's ReservedFields decode: all fields or none.
func applyReserved(p *parsedOutput, m map[string]any) {
	var r parsedOutput
	optString := func(key string) (*string, bool) {
		switch v := m[key].(type) {
		case nil:
			return nil, true
		case string:
			return &v, true
		}
		return nil, false
	}
	var ok bool
	if r.text, ok = optString("text"); !ok {
		return
	}
	if r.alt, ok = optString("alt"); !ok {
		return
	}
	if r.tooltip, ok = optString("tooltip"); !ok {
		return
	}
	switch v := m["percentage"].(type) {
	case nil:
	case int64:
		if v < 0 || v > 255 {
			return
		}
		pct := int(min(v, 100))
		r.percentage = &pct
	default:
		return
	}
	switch v := m["class"].(type) {
	case nil:
	case string:
		r.class = []string{v}
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return
			}
			r.class = append(r.class, s)
		}
	default:
		return
	}
	p.text, p.alt, p.tooltip, p.percentage, p.class = r.text, r.alt, r.tooltip, r.percentage, r.class
}

// jsonValue maps decoded JSON onto template values: numbers become
// integers when serde_json would (no fraction or exponent, in range).
func jsonValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, item := range x {
			x[k] = jsonValue(item)
		}
	case []any:
		for i, item := range x {
			x[i] = jsonValue(item)
		}
	}
	return v
}

// templateContext merges the JSON object's fields with the raw output
// as "output".
func (p parsedOutput) templateContext() map[string]any {
	ctx := map[string]any{}
	if m, ok := p.json.(map[string]any); ok {
		maps.Copy(ctx, m)
	}
	ctx["output"] = p.raw
	return ctx
}

func renderCustom(format string, p parsedOutput) string {
	return jinja.RenderOr(format, p.templateContext())
}

// formatCustomLabel is format_label: the JSON text wins over the
// rendered format.
func formatCustomLabel(def config.CustomModuleDefinition, p parsedOutput) string {
	if p.text != nil {
		return *p.text
	}
	return renderCustom(def.Format, p)
}

// formatCustomTooltip is format_tooltip: the JSON tooltip, else the
// rendered tooltip-format, else none.
func formatCustomTooltip(def config.CustomModuleDefinition, p parsedOutput) (string, bool) {
	if p.tooltip != nil {
		return *p.tooltip, true
	}
	if def.TooltipFormat == nil {
		return "", false
	}
	return renderCustom(*def.TooltipFormat, p), true
}

// resolveCustomIcon is resolve_icon: icon-map[alt], then icon-names by
// percentage, then icon-map["default"], then icon-name.
func resolveCustomIcon(def config.CustomModuleDefinition, p parsedOutput) string {
	if def.IconMap != nil && p.alt != nil {
		if icon, ok := (*def.IconMap)[*p.alt]; ok {
			return icon
		}
	}
	if def.IconNames != nil && len(*def.IconNames) > 0 && p.percentage != nil {
		names := *def.IconNames
		if i := *p.percentage * len(names) / 101; i < len(names) {
			return names[i]
		}
	}
	if def.IconMap != nil {
		if icon, ok := (*def.IconMap)["default"]; ok {
			return icon
		}
	}
	return def.IconName
}

// resolveCustomColors is resolve_colors: the color-map state for alt
// (else "default"), each unset color falling back to the static one.
// Nothing without a color-map.
func resolveCustomColors(def config.CustomModuleDefinition, p parsedOutput) (config.ThresholdColors, bool) {
	if def.ColorMap == nil {
		return config.ThresholdColors{}, false
	}
	state, ok := config.StateColors{}, false
	if p.alt != nil {
		state, ok = (*def.ColorMap)[*p.alt]
	}
	if !ok {
		state = (*def.ColorMap)["default"]
	}
	pick := func(sel *config.ColorValue, fallback config.ColorValue) *config.ColorValue {
		if sel != nil {
			return sel
		}
		return &fallback
	}
	return config.ThresholdColors{
		IconColor:     pick(state.IconColor, def.IconColor),
		IconBgColor:   pick(state.IconBgColor, def.IconBgColor),
		LabelColor:    pick(state.LabelColor, def.LabelColor),
		ButtonBgColor: pick(state.ButtonBgColor, def.ButtonBgColor),
		BorderColor:   pick(state.BorderColor, def.BorderColor),
	}, true
}

// resolveCustomClasses is resolve_classes: the JSON classes, then the
// rendered class-format split on whitespace, without repeats.
func resolveCustomClasses(def config.CustomModuleDefinition, p parsedOutput) []string {
	classes := slices.Clone(p.class)
	if def.ClassFormat != nil {
		for c := range strings.FieldsSeq(renderCustom(*def.ClassFormat, p)) {
			if !slices.Contains(classes, c) {
				classes = append(classes, c)
			}
		}
	}
	return classes
}

// shouldHideCustom is helpers.rs's should_hide: empty, "0", or "false"
// (any case) hides the module when hide-if-empty is set.
func shouldHideCustom(output string, hideIfEmpty bool) bool {
	if !hideIfEmpty {
		return false
	}
	return output == "" || output == "0" || strings.EqualFold(output, "false")
}

// customUpdates routes widget.update pushes to every module with the
// target id (one per bar, as every Rust instance hears the bus) and
// keeps each id's last output, so a module rebuilt by a reload starts
// from it.
type customUpdates struct {
	mu      sync.Mutex
	modules map[string][]*customModule
	last    map[string]string
}

func newCustomUpdates() *customUpdates {
	return &customUpdates{modules: map[string][]*customModule{}, last: map[string]string{}}
}

func (c *customUpdates) register(m *customModule) (last string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.modules[m.def.Id] = append(c.modules[m.def.Id], m)
	last, ok = c.last[m.def.Id]
	return last, ok
}

func (c *customUpdates) unregister(m *customModule) {
	c.mu.Lock()
	c.modules[m.def.Id] = slices.DeleteFunc(c.modules[m.def.Id], func(x *customModule) bool { return x == m })
	c.mu.Unlock()
}

func (c *customUpdates) remember(id, output string) {
	c.mu.Lock()
	c.last[id] = output
	c.mu.Unlock()
}

// dispatch applies one push to every module with the id; an unknown id
// is a quiet no-op.
func (c *customUpdates) dispatch(id, output string) {
	c.mu.Lock()
	targets := slices.Clone(c.modules[id])
	c.mu.Unlock()
	for _, m := range targets {
		m.ctx.Invoke(func() { m.apply(output) })
	}
}

// customModule is one [[modules.custom]] definition on a bar.
type customModule struct {
	buttonRef
	ctx   ModuleContext
	def   config.CustomModuleDefinition
	label *widget.Label
	icon  *widget.Icon
	root  widget.Widget

	life    context.Context
	stop    context.CancelFunc
	mu      sync.Mutex
	command context.CancelFunc // the in-flight command (poll or on-action)
	scroll  *time.Timer        // the on-action scroll debounce
	classes []string
}

// newCustomByID resolves the definition behind a "custom-<id>" layout
// entry.
func newCustomByID(ctx ModuleContext, id string) (Module, error) {
	def, ok := ctx.Config.CustomByID(id)
	if !ok {
		return nil, errors.New("custom: no definition with id " + id)
	}
	return newCustom(ctx, def)
}

func newCustom(ctx ModuleContext, def config.CustomModuleDefinition) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("custom: requires the application loop")
	}
	m := &customModule{ctx: ctx, def: def}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, formatCustomLabel(def, parsedOutput{}), ctx.Style.fg)
	m.icon = moduleIcon(ctx, def.Icon())
	m.root = assembleModule(ctx, m.icon, m.label)
	m.life, m.stop = context.WithCancel(ctx.Life())
	initial := ""
	if ctx.CustomUpdates != nil {
		if last, ok := ctx.CustomUpdates.register(m); ok {
			initial = last
		}
	}
	m.apply(initial)
	if def.Command != nil {
		switch def.Mode {
		case config.ExecutionModeWatch:
			go m.supervise(*def.Command)
		default:
			m.runCommand(*def.Command)
			if def.IntervalMs > 0 {
				go m.poll(time.Duration(def.IntervalMs) * time.Millisecond)
			}
		}
	}
	return m, nil
}

// setButton receives the module's bar button; the pending threshold
// colors, the tooltip, the classes, and the visibility land on it.
func (m *customModule) setButton(b *barButton) {
	m.buttonRef.setButton(b)
	m.apply(m.lastOutput())
}

func (m *customModule) lastOutput() string {
	if m.ctx.CustomUpdates == nil {
		return ""
	}
	m.ctx.CustomUpdates.mu.Lock()
	defer m.ctx.CustomUpdates.mu.Unlock()
	return m.ctx.CustomUpdates.last[m.def.Id]
}

// poll is spawn_command_poller: the command every interval, the first
// one interval after the start (the start itself already ran it).
func (m *customModule) poll(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-m.life.Done():
			return
		case <-ticker.C:
		}
		m.runCommand(*m.def.Command)
	}
}

// runCommand is run_command_async: the command through sh -c with the
// 30s timeout, replacing any command still in flight; its trimmed
// stdout is the module's output. A failure or timeout changes nothing.
func (m *customModule) runCommand(command string) {
	ctx, cancel := context.WithTimeout(m.life, commandTimeout)
	m.mu.Lock()
	if m.command != nil {
		m.command()
	}
	m.command = cancel
	m.mu.Unlock()
	go func() {
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // the command comes from the user's own config
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			switch {
			case errors.Is(ctx.Err(), context.DeadlineExceeded):
				log.Printf("custom %s: command timed out after %s", m.def.Id, commandTimeout)
				return
			case ctx.Err() != nil:
				return // superseded or the module went away
			case !exitedWithOutput(err):
				log.Printf("custom %s: command execution failed: %v", m.def.Id, err)
				return
			}
			// A nonzero exit still has its output, as tokio's output().
		}
		output := strings.TrimSpace(strings.ToValidUTF8(out.String(), "�"))
		m.ctx.Invoke(func() { m.apply(output) })
	}()
}

// exitedWithOutput reports a command that ran and exited nonzero: its
// stdout still counts (tokio's output() keeps it).
func exitedWithOutput(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit)
}

// supervise is spawn_command_watcher: each stdout line of the running
// command is an output; on exit the restart policy decides, after
// restart-interval-ms, whether it runs again.
func (m *customModule) supervise(command string) {
	delay := time.Duration(m.def.RestartIntervalMs) * time.Millisecond
	for {
		exitErr, ran := m.watchOnce(command)
		if !ran || m.life.Err() != nil {
			return
		}
		switch m.def.RestartPolicy {
		case config.RestartPolicyOnExit:
		case config.RestartPolicyOnFailure:
			if exitErr == nil {
				return
			}
		default:
			return
		}
		select {
		case <-m.life.Done():
			return
		case <-time.After(delay):
		}
	}
}

// watchOnce runs the command once, streaming its lines; ran is false
// when it could not start.
func (m *customModule) watchOnce(command string) (exitErr error, ran bool) {
	cmd := exec.CommandContext(m.life, "sh", "-c", command) //nolint:gosec // the command comes from the user's own config
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("custom %s: watch command started without stdout: %v", m.def.Id, err)
		return nil, false
	}
	if err := cmd.Start(); err != nil {
		log.Printf("custom %s: failed to spawn watch command: %v", m.def.Id, err)
		return nil, false
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		m.ctx.Invoke(func() { m.apply(line) })
	}
	return cmd.Wait(), true
}

// followAction is the on-action hook (mod.rs update): after a shell
// binding, the on-action command runs and its output applies; scrolls
// debounce it by 50ms.
func (m *customModule) followAction(action config.ClickAction, scroll bool) {
	if action.Kind != config.ClickShell || m.def.OnAction == nil {
		return
	}
	onAction := *m.def.OnAction
	if !scroll {
		m.runCommand(onAction)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scroll != nil {
		m.scroll.Stop()
	}
	m.scroll = time.AfterFunc(customScrollWait, func() {
		if m.life.Err() == nil {
			m.runCommand(onAction)
		}
	})
}

// apply is apply_output: label, icon, tooltip, visibility, the
// color-map colors, and the dynamic classes from one output.
func (m *customModule) apply(output string) {
	if m.ctx.CustomUpdates != nil && output != "" {
		m.ctx.CustomUpdates.remember(m.def.Id, output)
	}
	p := parseCustomOutput(output)
	m.label.SetText(formatCustomLabel(m.def, p))
	if m.icon != nil {
		m.icon.SetThemeName(resolveCustomIcon(m.def, p))
	}
	btn := m.btn
	if btn == nil {
		return
	}
	if tip, ok := formatCustomTooltip(m.def, p); ok {
		btn.SetTooltip(tip)
	} else {
		btn.SetTooltip("")
	}
	btn.SetVisible(!shouldHideCustom(p.raw, m.def.HideIfEmpty))
	if colors, ok := resolveCustomColors(m.def, p); ok {
		btn.SetThresholds(colors)
	}
	classes := resolveCustomClasses(m.def, p)
	btn.RemoveClass(m.classes...)
	btn.AddClass(classes...)
	m.classes = classes
}

func (m *customModule) Root() widget.Widget { return m.root }

// Stop ends the commands and the update registration.
func (m *customModule) Stop() {
	if m.stop != nil {
		m.stop()
	}
	if m.ctx.CustomUpdates != nil {
		m.ctx.CustomUpdates.unregister(m)
	}
}

// customDefinition resolves the definition behind a "custom-<id>"
// layout name; plain module names return false.
func customDefinition(name string, cfg *config.Config) (config.CustomModuleDefinition, bool) {
	const prefix = "custom-"
	if !strings.HasPrefix(name, prefix) {
		return config.CustomModuleDefinition{}, false
	}
	return cfg.CustomByID(name[len(prefix):])
}
