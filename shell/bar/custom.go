package bar

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// commandTimeout kills any single custom-module run after 30 seconds,
// matching the schema's documented behavior.
const commandTimeout = 30 * time.Second

// parsedOutput carries the reserved fields helpers.rs extracts from
// JSON command output; plain output only fills Raw.
type parsedOutput struct {
	raw        string
	text       string
	alt        string
	percentage int
	tooltip    string
	vars       map[string]string
}

// parseCustomOutput: output starting with { or [ parses as JSON with
// the reserved fields (text, alt, percentage, tooltip) plus every
// top-level field as a template variable; anything else is raw text.
func parseCustomOutput(out string) parsedOutput {
	trimmed := strings.TrimSpace(out)
	parsed := parsedOutput{raw: trimmed, vars: map[string]string{}}
	if !strings.HasPrefix(trimmed, "{") {
		parsed.vars["output"] = trimmed
		return parsed
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		parsed.vars["output"] = trimmed
		return parsed
	}
	for key, value := range doc {
		parsed.vars[key] = scalarString(value)
	}
	if text, ok := doc["text"].(string); ok {
		parsed.text = text
	}
	if alt, ok := doc["alt"].(string); ok {
		parsed.alt = alt
	}
	if pct, ok := doc["percentage"].(float64); ok {
		parsed.percentage = int(min(max(pct, 0), 100))
	}
	if tip, ok := doc["tooltip"].(string); ok {
		parsed.tooltip = tip
	}
	parsed.vars["output"] = trimmed
	return parsed
}

// scalarString renders one JSON leaf for the template context.
func scalarString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return ""
	}
}

// formatLabel is helpers.rs's format_label: an explicit JSON text
// field wins over the rendered format.
func formatCustomLabel(def config.CustomModuleConfig, parsed parsedOutput) string {
	if parsed.text != "" {
		return parsed.text
	}
	return renderTemplate(def.Format, parsed.vars)
}

// renderTemplate substitutes {{ path }} occurrences, walking dot
// paths through the variable map. Unknown variables render empty, like
// the level modules' whitespace-insensitive braces.
func renderTemplate(format string, vars map[string]string) string {
	out := format
	for {
		start := strings.Index(out, "{{")
		if start < 0 {
			return out
		}
		end := strings.Index(out[start:], "}}")
		if end < 0 {
			return out
		}
		end += start
		name := strings.TrimSpace(out[start+2 : end])
		out = out[:start] + vars[name] + out[end+2:]
	}
}

// shouldHide is helpers.rs's should_hide: empty, "0", or "false"
// (case-insensitive) hides the module.
func shouldHideCustom(output string, hideIfEmpty bool) bool {
	if !hideIfEmpty {
		return false
	}
	return output == "" || output == "0" || strings.EqualFold(output, "false")
}

// custom is the module: a shell command rendered into a label.
type customModule struct {
	ctx    ModuleContext
	def    config.CustomModuleConfig
	label  *widget.Label
	cancel context.CancelFunc
}

// customUpdates routes widget.update pushes to the module with the
// target id. RunWith owns one; modules register at construction.
type customUpdates struct {
	mu      sync.Mutex
	modules map[string]*customModule
}

func newCustomUpdates() *customUpdates {
	return &customUpdates{modules: make(map[string]*customModule)}
}

func (c *customUpdates) register(m *customModule) {
	c.mu.Lock()
	c.modules[m.def.ID] = m
	c.mu.Unlock()
}

func (c *customUpdates) unregister(m *customModule) {
	c.mu.Lock()
	if cur, ok := c.modules[m.def.ID]; ok && cur == m {
		delete(c.modules, m.def.ID)
	}
	c.mu.Unlock()
}

// dispatch applies one push; an unknown id is a quiet no-op (the
// Rust shell drops it the same way). Headless modules apply inline.
func (c *customUpdates) dispatch(id, output string) {
	c.mu.Lock()
	m := c.modules[id]
	c.mu.Unlock()
	if m == nil {
		return
	}
	if m.ctx.App == nil {
		m.apply(output)
		return
	}
	m.ctx.Invoke(func() { m.apply(output) })
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

func newCustom(ctx ModuleContext, def config.CustomModuleConfig) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("custom: requires the application loop")
	}
	m := &customModule{ctx: ctx, def: def}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	if ctx.CustomUpdates != nil {
		ctx.CustomUpdates.register(m)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	switch def.Mode {
	case config.CustomModeWatch:
		go m.watch(runCtx)
	default:
		if def.IntervalMs > 0 {
			m.poll(runCtx)
		}
	}
	return m, nil
}

// poll runs the command on the interval, then keeps the last output on
// display (the Rust watcher leaves the stale value until the next
// success).
func (m *customModule) poll(ctx context.Context) {
	run := func() {
		out, err := runCommand(ctx, m.def.Command)
		if err != nil {
			log.Printf("custom %s: %v", m.def.ID, err)
			return
		}
		m.apply(out)
	}
	go func() {
		every := time.Duration(m.def.IntervalMs) * time.Millisecond
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			run()
			timer.Reset(every)
		}
	}()
}

// watch streams the command's stdout line by line; each line replaces
// the label. The restart policy (never, the schema default) means an
// exiting command just ends the stream.
func (m *customModule) watch(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "sh", "-c", m.def.Command) //nolint:gosec // the command comes from the user's own config
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("custom %s: %v", m.def.ID, err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("custom %s: %v", m.def.ID, err)
		return
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		m.ctx.Invoke(func() { m.apply(line) })
	}
	_ = cmd.Wait()
	if err := scanner.Err(); err != nil {
		log.Printf("custom %s: %v", m.def.ID, err)
	}
}

// runCommand executes one poll through sh -c with the 30s timeout.
func runCommand(ctx context.Context, command string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	if command == "" {
		return "", nil
	}
	out, err := exec.CommandContext(runCtx, "sh", "-c", command).Output() //nolint:gosec // the command comes from the user's own config
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// apply renders the output into the label.
func (m *customModule) apply(out string) {
	parsed := parseCustomOutput(out)
	label := ""
	if m.def.LabelShow {
		label = formatCustomLabel(m.def, parsed)
	}
	if m.def.LabelMaxLength > 0 {
		label = truncateLabel(label, m.def.LabelMaxLength)
	}
	m.label.SetText(label)
	if shouldHideCustom(parsed.raw, m.def.HideIfEmpty) {
		m.label.SetText("")
	}
}

func (m *customModule) Root() widget.Widget {
	return assembleModule(m.ctx, moduleIcon(m.ctx, m.def.Icon), m.label)
}

// Stop releases the poll/watch goroutine and the update registration.
func (m *customModule) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.ctx.CustomUpdates != nil {
		m.ctx.CustomUpdates.unregister(m)
	}
}

// customDefinition resolves the definition behind a "custom-<id>"
// layout name; plain module names return false.
func customDefinition(name string, cfg *config.Config) (config.CustomModuleConfig, bool) {
	const prefix = "custom-"
	if !strings.HasPrefix(name, prefix) {
		return config.CustomModuleConfig{}, false
	}
	return cfg.CustomByID(name[len(prefix):])
}
