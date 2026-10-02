package settings

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// workspaceStyle is one workspace_style_map row: the workspace key and
// its WorkspaceStyle, icon and color nil while unset.
type workspaceStyle struct {
	key, label  string
	icon, color any
}

// workspaceStyleItem is the row: key entry, icon picker, label entry
// and the optional color, editing a local style.
type workspaceStyleItem struct {
	*widget.Box
	key, label *widget.Entry
	icon       *iconEditor
	color      control
	style      *workspaceStyle
}

func (w workspaceStyleItem) itemValue() any {
	s := *w.style
	s.key, s.label = w.key.Text(), w.label.Text()
	return s
}

// workspaceStyleMap is workspace_style_map::workspace_style_map over
// a map of WorkspaceStyle: numeric keys (a Hyprland workspace id, the
// map's order numeric, a key that does not parse dropped on write) or
// names (sorted, an empty one dropped).
func workspaceStyleMap(numeric bool) rowOpt {
	return listRow(func(config.FieldMeta) listSpec {
		t := i18n.Settings()
		return listSpec{
			addKey: "settings-map-add", rowClass: "string-map-row", blank: workspaceStyle{},
			classes: []string{"workspace-style-editor"}, listCls: "string-map",
			item: func(k *kit, v any, changed func()) listItem {
				style, _ := v.(workspaceStyle)
				w := workspaceStyleItem{Box: widget.NewBox(widget.Row, 4, 0), key: k.entry(), label: k.entry(), style: &style}
				w.key.SetPlaceholder(t.Get("settings-workspace-key-placeholder"))
				w.label.SetPlaceholder(t.Get("settings-workspace-label-placeholder"))
				w.key.SetText(style.key)
				w.label.SetText(style.label)
				w.key.OnChanged = func(string) { changed() }
				w.label.OnChanged = func(string) { changed() }
				local := func(field *any) slot {
					return slot{
						get:   func() any { return *field },
						set:   func(v any) error { *field = v; changed(); return nil },
						unset: func() { *field = nil; changed() },
					}
				}
				w.icon = newIconEditor(k, local(&style.icon))
				w.color = optionalColor(k, local(&style.color))
				w.Append(w.key, true)
				w.Append(w.icon, true)
				w.Append(w.label, true)
				w.AppendAligned(w.color, false, widget.AlignCenter)
				return w
			},
			decode: func(v any) []any {
				m, _ := v.(map[string]any)
				keys := slices.Collect(maps.Keys(m))
				slices.SortFunc(keys, func(a, b string) int {
					if numeric {
						x, _ := strconv.Atoi(a)
						y, _ := strconv.Atoi(b)
						return cmp.Compare(x, y)
					}
					return cmp.Compare(a, b)
				})
				out := make([]any, len(keys))
				for i, key := range keys {
					fields, _ := m[key].(map[string]any)
					label, _ := fields["label"].(string)
					out[i] = workspaceStyle{key: key, label: label, icon: fields["icon"], color: fields["color"]}
				}
				return out
			},
			encode: func(items []any) any {
				out := map[string]any{}
				for _, it := range items {
					s, _ := it.(workspaceStyle)
					key := s.key
					if numeric {
						id, err := strconv.ParseInt(strings.TrimSpace(key), 10, 32)
						if err != nil {
							continue
						}
						key = strconv.FormatInt(id, 10)
					} else if key == "" {
						continue
					}
					fields := map[string]any{}
					if name, _ := s.icon.(string); name != "" {
						fields["icon"] = name
					}
					if s.color != nil {
						fields["color"] = s.color
					}
					if s.label != "" {
						fields["label"] = s.label
					}
					out[key] = fields
				}
				return out
			},
		}
	})
}
