package config

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// DiagnosticKind is the severity of a Diagnostic.
type DiagnosticKind int

// Kinds.
const (
	DiagnosticError DiagnosticKind = iota
	DiagnosticWarning
)

// Diagnostic is a user-facing config problem: a title, aligned
// label/value fields, and an optional hint (crates/wayle-config/src/
// diagnostic.rs). A bad value in one field is a diagnostic, not a
// failed load: the field keeps its default and the rest applies, the
// way ApplyConfigLayer skips a field that fails to deserialize.
type Diagnostic struct {
	Kind   DiagnosticKind
	Title  string
	Fields [][2]string
	Hint   string
}

func (d Diagnostic) field(label, value string) Diagnostic {
	d.Fields = append(d.Fields, [2]string{label, value})
	return d
}

// Field returns the value of the labelled field, or "".
func (d Diagnostic) Field(label string) string {
	for _, f := range d.Fields {
		if f[0] == label {
			return f[1]
		}
	}
	return ""
}

// Plain renders the diagnostic without color (to_plain).
func (d Diagnostic) Plain() string { return d.render(false) }

// String renders the diagnostic with the ANSI colors the Rust Display
// uses (owo-colors: red/yellow bold title, cyan labels, green hint).
func (d Diagnostic) String() string { return d.render(true) }

// Error lets a diagnostic travel as an error.
func (d Diagnostic) Error() string { return strings.TrimSpace(d.Plain()) }

func (d Diagnostic) render(color bool) string {
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	label, titleCode := "error:", "1;31"
	if d.Kind == DiagnosticWarning {
		label, titleCode = "warning:", "1;33"
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(paint(titleCode, label) + " " + paint(titleCode, d.Title) + "\n")
	width := 0
	for _, f := range d.Fields {
		width = max(width, len(f[0]))
	}
	for _, f := range d.Fields {
		padded := fmt.Sprintf("%*s", width, f[0])
		b.WriteString("    " + paint("36", padded) + ": " + paint("37", f[1]) + "\n")
	}
	if d.Hint != "" {
		b.WriteString("\n")
		b.WriteString("    " + paint("32", "→") + " " + paint("32", d.Hint) + "\n")
	}
	return b.String()
}

// DiagnosticSink receives diagnostics as layers apply. The default
// writes them colored to stderr, like Diagnostic::emit.
type DiagnosticSink func(Diagnostic)

// StderrDiagnostics emits to stderr.
func StderrDiagnostics(d Diagnostic) { emitTo(os.Stderr, d) }

func emitTo(w io.Writer, d Diagnostic) { _, _ = fmt.Fprintln(w, d.String()) }

// DiscardDiagnostics drops diagnostics (re-applying layers that were
// already reported).
func DiscardDiagnostics(Diagnostic) {}
