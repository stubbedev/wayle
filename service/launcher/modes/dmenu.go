package modes

import (
	"context"

	"github.com/stubbedev/wayle/service/launcher"
)

// DmenuConfig is the part of the CLI's dmenu flags the engine needs.
type DmenuConfig struct {
	// Prompt is -p; nil for the mode's default.
	Prompt *string
	// Message is -mesg.
	Message     *string
	MarkupRows  bool
	MultiSelect bool
	// NoCustom is -no-custom or -only-match.
	NoCustom bool
	// Urgent and Active are the -u and -a indices, ranges expanded.
	Urgent []uint32
	Active []uint32
}

// Dmenu is dmenu mode: rows arrive from the CLI over a channel, and an
// accept exits the session with the selection (dmenu.rs).
type Dmenu struct {
	cfg   DmenuConfig
	rows  <-chan []string
	texts []string
}

// NewDmenu builds the mode over the CLI's row stream; the channel
// closes when the rows are done.
func NewDmenu(cfg DmenuConfig, rows <-chan []string) *Dmenu {
	return &Dmenu{cfg: cfg, rows: rows}
}

// Name is "dmenu".
func (*Dmenu) Name() string { return "dmenu" }

// Load drains the whole row stream, then builds the list.
func (d *Dmenu) Load(ctx context.Context) launcher.ModeState {
	var items []launcher.Item
	var texts []string
	if d.rows != nil {
	drain:
		for {
			select {
			case chunk, ok := <-d.rows:
				if !ok {
					break drain
				}
				for _, raw := range chunk {
					parsed := parseRow(raw)
					texts = append(texts, parsed.text)
					items = append(items, parsed.item)
				}
			case <-ctx.Done():
				break drain
			}
		}
		d.rows = nil
	}
	for _, i := range d.cfg.Urgent {
		if int(i) < len(items) {
			items[i].Flags |= launcher.FlagUrgent
		}
	}
	for _, i := range d.cfg.Active {
		if int(i) < len(items) {
			items[i].Flags |= launcher.FlagActive
		}
	}
	d.texts = texts
	prompt := "dmenu"
	if d.cfg.Prompt != nil {
		prompt = *d.cfg.Prompt
	}
	return launcher.ModeState{
		Items:       items,
		Prompt:      prompt,
		Message:     d.cfg.Message,
		MarkupRows:  d.cfg.MarkupRows,
		MultiSelect: d.cfg.MultiSelect,
		NoCustom:    d.cfg.NoCustom,
		// kb-custom-N always exits with 10..28.
		UseHotKeys: true,
	}
}

func (d *Dmenu) selected(i uint32) (launcher.Selection, bool) {
	if int(i) >= len(d.texts) {
		return launcher.Selection{}, false
	}
	return launcher.Selection{Index: int64(i), Text: d.texts[i]}, true
}

// Activate exits with the row (or the typed text, index -1) and rofi's
// code: 0, or 9+N for kb-custom-N.
func (d *Dmenu) Activate(_ context.Context, target launcher.Target, kind launcher.ActivateKind, input string) launcher.Action {
	code := 0
	if k, ok := kind.(launcher.ActivateKbCustom); ok {
		code = 9 + int(k.N)
	}
	var selected []launcher.Selection
	if custom, ok := kind.(launcher.ActivateCustom); ok {
		selected = []launcher.Selection{{Index: -1, Text: custom.Text}}
	} else if row, ok := target.Row(); ok {
		if s, ok := d.selected(row); ok {
			selected = []launcher.Selection{s}
		}
	} else {
		selected = []launcher.Selection{{Index: -1, Text: input}}
	}
	if len(selected) == 0 {
		return launcher.ActionNothing{}
	}
	return launcher.ActionExit{Code: code, Selected: selected}
}

// ActivateMany exits with every picked row, in the given order.
func (d *Dmenu) ActivateMany(_ context.Context, indices []uint32, _ string) launcher.Action {
	var selected []launcher.Selection
	for _, i := range indices {
		if s, ok := d.selected(i); ok {
			selected = append(selected, s)
		}
	}
	if len(selected) == 0 {
		return launcher.ActionNothing{}
	}
	return launcher.ActionExit{Selected: selected}
}
