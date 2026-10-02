package printdialog

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/internal/ipp"
	"github.com/stubbedev/wayle/internal/xdg"
)

// paper is one size the form offers: its label, GTK's paper name (the
// settings the portal returns), its size in millimetres, and the PWG
// media name IPP takes.
type paper struct {
	label, gtk    string
	width, height float64
	media         string
}

// papers are the form's sizes (shell/print PAPER_SIZES), A4 first.
var papers = []paper{
	{"A4", "iso_a4", 210, 297, "iso_a4_210x297mm"},
	{"Letter", "na_letter", 215.9, 279.4, "na_letter_8.5x11in"},
	{"Legal", "na_legal", 215.9, 355.6, "na_legal_8.5x14in"},
	{"A3", "iso_a3", 297, 420, "iso_a3_297x420mm"},
	{"A5", "iso_a5", 148, 210, "iso_a5_148x210mm"},
	{"Executive", "na_executive", 184.15, 266.7, "na_executive_7.25x10.5in"},
}

// The form's choices, in their dropdown order.
var (
	orientations = []string{"Portrait", "Landscape"}
	colors       = []string{"Color", "Grayscale"}
	duplexes     = []string{"One-sided", "Two-sided (long edge)", "Two-sided (short edge)"}
	qualities    = []string{"Normal", "Draft", "High"}
)

// form is what the user chose; the indices follow the dropdowns.
type form struct {
	copies    int
	pages     string
	landscape bool
	paper     int
	grayscale bool
	duplex    int
	quality   int
}

// The copies bounds (the Rust spin button's adjustment).
const (
	minCopies = 1
	maxCopies = 999
)

// pageRanges parses "1-5, 8" into 1-based ranges, each low to high;
// false for an empty input or one that does not parse, which prints
// every page (shell/print's parse_page_ranges).
func pageRanges(input string) ([][2]int32, bool) {
	var out [][2]int32
	for tok := range strings.SplitSeq(input, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(tok, "-")
		a, err := strconv.ParseUint(strings.TrimSpace(lo), 10, 31)
		if err != nil {
			return nil, false
		}
		b := a
		if isRange {
			if b, err = strconv.ParseUint(strings.TrimSpace(hi), 10, 31); err != nil {
				return nil, false
			}
		}
		first, last := int32(max(a, 1)), int32(max(b, 1))
		out = append(out, [2]int32{min(first, last), max(first, last)})
	}
	return out, len(out) > 0
}

// gtkRanges is GTK's zero-based page-ranges value ("0-4,7-7").
func gtkRanges(rs [][2]int32) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = strconv.Itoa(int(r[0]-1)) + "-" + strconv.Itoa(int(r[1]-1))
	}
	return strings.Join(parts, ",")
}

// paperOf is the chosen paper, A4 for an out-of-range index.
func (f form) paperOf() paper {
	if f.paper >= 0 && f.paper < len(papers) {
		return papers[f.paper]
	}
	return papers[0]
}

func mm(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// settings are the GtkPrintSettings keys the portal returns, as the
// Rust host set them, plus the printer GTK's dialog records.
func (f form) settings(printer string) []Setting {
	p := f.paperOf()
	orientation := "portrait"
	if f.landscape {
		orientation = "landscape"
	}
	s := []Setting{
		{"printer", printer},
		{"n-copies", strconv.Itoa(f.copies)},
		{"orientation", orientation},
		{"paper-format", p.gtk},
		{"paper-width", mm(p.width)},
		{"paper-height", mm(p.height)},
		{"use-color", strconv.FormatBool(!f.grayscale)},
		{"duplex", [...]string{"simplex", "horizontal", "vertical"}[f.duplex]},
		{"quality", [...]string{"normal", "draft", "high"}[f.quality]},
	}
	if rs, ok := pageRanges(f.pages); ok {
		return append(s, Setting{"print-pages", "ranges"}, Setting{"page-ranges", gtkRanges(rs)})
	}
	return append(s, Setting{"print-pages", "all"})
}

// jobAttrs are the form as IPP job template attributes (what GTK's
// CUPS backend maps the settings to).
func (f form) jobAttrs() []ipp.Attr {
	orientation, color := int32(3), "color"
	if f.landscape {
		orientation = 4
	}
	if f.grayscale {
		color = "monochrome"
	}
	attrs := []ipp.Attr{
		ipp.Int("copies", int32(f.copies)),
		ipp.Enum("orientation-requested", orientation),
		ipp.String(ipp.TagKeyword, "media", f.paperOf().media),
		ipp.String(ipp.TagKeyword, "print-color-mode", color),
		ipp.String(ipp.TagKeyword, "sides", [...]string{"one-sided", "two-sided-long-edge", "two-sided-short-edge"}[f.duplex]),
		ipp.Enum("print-quality", [...]int32{4, 3, 5}[f.quality]),
	}
	if rs, ok := pageRanges(f.pages); ok {
		attrs = append(attrs, ipp.Ranges("page-ranges", rs))
	}
	return attrs
}

// printerStatus is the row's status: GTK's paused, not accepting, the
// state message, else ready.
func printerStatus(p ipp.Printer) string {
	switch {
	case p.State == ipp.StateStopped:
		return "Paused"
	case !p.Accepting:
		return "Not accepting jobs"
	case p.StateMessage != "":
		return p.StateMessage
	}
	return "Ready"
}

// printerDetail is the row's muted line: location · status.
func printerDetail(location, status string) string {
	var parts []string
	for _, s := range []string{location, status} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// fileQueue is GTK's always-present file backend printer.
const fileQueue = "Print to File"

// FileOutput is where Print to File writes, GTK's default: output.pdf
// in the documents directory, else the home directory.
func FileOutput() string {
	if dir, ok := xdg.UserDir("DOCUMENTS"); ok {
		return filepath.Join(dir, "output.pdf")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "output.pdf"
	}
	return filepath.Join(home, "output.pdf")
}
