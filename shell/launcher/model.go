package launcher

import (
	"strings"

	"github.com/stubbedev/wayle/internal/pango"
	engine "github.com/stubbedev/wayle/service/launcher"
)

// matchModel is match_model.rs: the mode's rows and the matcher's
// order over them. A list position indexes matched; matched holds item
// indices.
type matchModel struct {
	items   []engine.Item
	matched []uint32
}

// update swaps in a fresh match result.
func (m *matchModel) update(items []engine.Item, matched []uint32) {
	m.items, m.matched = items, matched
}

func (m *matchModel) len() int { return len(m.matched) }

// itemIndex is the item behind list position pos.
func (m *matchModel) itemIndex(pos int) (uint32, bool) {
	if pos < 0 || pos >= len(m.matched) {
		return 0, false
	}
	return m.matched[pos], true
}

// item is the row at list position pos.
func (m *matchModel) item(pos int) (engine.Item, bool) {
	i, ok := m.itemIndex(pos)
	if !ok || int(i) >= len(m.items) {
		return engine.Item{}, false
	}
	return m.items[i], true
}

// findPosition is the first position whose match text contains needle,
// case-insensitively (-select).
func (m *matchModel) findPosition(needle string) (int, bool) {
	needle = strings.ToLower(needle)
	for pos, i := range m.matched {
		if int(i) < len(m.items) && strings.Contains(strings.ToLower(m.items[i].MatchText), needle) {
			return pos, true
		}
	}
	return 0, false
}

// textAt is the row's text as the user reads it: markup stripped to
// its text (pango_parse_markup), the display verbatim otherwise or when
// the markup does not parse; "" past the list.
func (m *matchModel) textAt(pos int) string {
	it, ok := m.item(pos)
	if !ok {
		return ""
	}
	if it.Flags.Has(engine.FlagMarkup) {
		if text, ok := pango.PlainText(it.Display); ok {
			return text
		}
	}
	return it.Display
}

// texts are every matched row's display in order (-dump).
func (m *matchModel) texts() []string {
	out := make([]string, 0, len(m.matched))
	for _, i := range m.matched {
		if int(i) < len(m.items) {
			out = append(out, m.items[i].Display)
		}
	}
	return out
}
