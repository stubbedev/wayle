package settings

import (
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// The module pages' own list editors (editors/tray_override_list,
// editors/mail_account_list).

// trayOverride is one tray_override_list row's TrayItemOverride, the
// color nil while unset.
type trayOverride struct {
	name, icon string
	color      any
}

// trayOverrideItem is the row: the name and icon entries and the
// optional color.
type trayOverrideItem struct {
	*widget.Box
	name, icon *widget.Entry
	value      *trayOverride
}

func (r trayOverrideItem) itemValue() any {
	v := *r.value
	v.name, v.icon = r.name.Text(), r.icon.Text()
	return v
}

// trayOverrideList is tray_override_list::tray_override_list: an
// override per row, a nameless one not written, an empty icon unset.
var trayOverrideList = listRow(func(config.FieldMeta) listSpec {
	t := i18n.Settings()
	return listSpec{
		addKey: "settings-map-add", rowClass: "string-map-row", blank: trayOverride{}, listCls: "string-map",
		item: func(k *kit, v any, changed func()) listItem {
			o, _ := v.(trayOverride)
			r := trayOverrideItem{Box: widget.NewBox(widget.Row, 4, 0), name: k.entry(), icon: k.entry(), value: &o}
			r.name.SetPlaceholder(t.Get("settings-tray-name-placeholder"))
			r.icon.SetPlaceholder(t.Get("settings-tray-icon-placeholder"))
			r.name.SetText(o.name)
			r.icon.SetText(o.icon)
			r.name.OnChanged = func(string) { changed() }
			r.icon.OnChanged = func(string) { changed() }
			color := optionalColor(k, slot{
				get:   func() any { return o.color },
				set:   func(v any) error { o.color = v; changed(); return nil },
				unset: func() { o.color = nil; changed() },
			})
			r.Append(r.name, true)
			r.Append(r.icon, true)
			r.AppendAligned(color, false, widget.AlignCenter)
			return r
		},
		decode: func(v any) []any {
			list, _ := v.([]any)
			out := make([]any, len(list))
			for i, it := range list {
				m, _ := it.(map[string]any)
				name, _ := m["name"].(string)
				icon, _ := m["icon"].(string)
				out[i] = trayOverride{name: name, icon: icon, color: m["color"]}
			}
			return out
		},
		encode: func(items []any) any {
			out := []any{}
			for _, it := range items {
				o, _ := it.(trayOverride)
				if o.name == "" {
					continue
				}
				m := map[string]any{"name": o.name}
				if o.icon != "" {
					m["icon"] = o.icon
				}
				if o.color != nil {
					m["color"] = o.color
				}
				out = append(out, m)
			}
			return out
		},
	}
})

// mailAccountList is mail_account_list::mail_account_list: a card per
// account, its name in the header; a nameless account is not written,
// an empty icon is unset (the provider's default).
var mailAccountList = cardRow(cardSpec{
	identity: &cardField{key: "name", label: "settings-mail-account-name", editor: func(k *kit, s slot) control {
		return newLiveText(k, s, "name")
	}},
	fields: []cardField{
		{key: "query", label: "settings-mail-account-query", editor: func(k *kit, s slot) control {
			return newLiveText(k, s, "tag:unread and folder:…")
		}},
		{key: "provider", label: "settings-mail-account-provider", editor: func(k *kit, s slot) control {
			return newEnumSelect(k, s, config.MetaOf[config.MailProvider]())
		}},
		{key: "icon", label: "settings-mail-account-icon", editor: func(k *kit, s slot) control { return newIconEditor(k, s) }},
	},
	blank: func() map[string]any {
		return map[string]any{"name": "", "query": ""}
	},
	keep: func(item map[string]any) bool { name, _ := item["name"].(string); return name != "" },
	clean: func(item map[string]any) map[string]any {
		if icon, _ := item["icon"].(string); icon == "" {
			delete(item, "icon")
		}
		return item
	},
	listCls: "card-form-list", rowClass: "card-form-row", labelCls: "card-form-label",
})
