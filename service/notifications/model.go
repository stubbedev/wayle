package notifications

import "github.com/godbus/dbus/v5"

// Urgency is the spec's urgency hint (types/notification.rs's Urgency).
type Urgency uint8

// Urgency levels.
const (
	UrgencyLow Urgency = iota
	UrgencyNormal
	UrgencyCritical
)

// UrgencyFromByte is From<u8>: 0 low, 2 critical, anything else normal.
func UrgencyFromByte(v byte) Urgency {
	switch v {
	case 0:
		return UrgencyLow
	case 2:
		return UrgencyCritical
	}
	return UrgencyNormal
}

// Action is one notification action: the id ActionInvoked reports and
// its label (core/types.rs's Action).
type Action struct {
	ID    string
	Label string
}

// DefaultActionID is the spec's body-click action.
const DefaultActionID = "default"

// ParseActions is parse_dbus_actions: the flat id, label, ... list as
// pairs; a trailing id without a label labels itself.
func ParseActions(raw []string) []Action {
	actions := make([]Action, 0, (len(raw)+1)/2)
	for i := 0; i < len(raw); i += 2 {
		id, label := raw[i], raw[i]
		if i+1 < len(raw) {
			label = raw[i+1]
		}
		actions = append(actions, Action{ID: id, Label: label})
	}
	return actions
}

// hints are the decoded Notify hints the shell reads (from_props).
type hints struct {
	urgency      Urgency
	imagePath    string
	desktopEntry string
	transient    bool
	resident     bool
}

// decodeHints reads the spec hints; a missing or mistyped hint keeps
// its default. Inline image data (image-data, then the deprecated
// image_data and icon_data) is cached as a PNG whose path replaces
// image-path, as normalize_hints does; cache turns pixels into a path.
func decodeHints(raw map[string]dbus.Variant, cache func(imageData) (string, bool)) hints {
	h := hints{urgency: UrgencyNormal}
	if v, ok := raw["urgency"].Value().(byte); ok {
		h.urgency = UrgencyFromByte(v)
	}
	h.imagePath, _ = raw["image-path"].Value().(string)
	h.desktopEntry, _ = raw["desktop-entry"].Value().(string)
	h.transient, _ = raw["transient"].Value().(bool)
	h.resident, _ = raw["resident"].Value().(bool)
	for _, key := range []string{"image-data", "image_data", "icon_data"} {
		v, ok := raw[key]
		if !ok {
			continue
		}
		img, ok := parseImageData(v)
		if !ok {
			// A malformed preferred key does not fall through to the
			// deprecated ones: IncomingHints fails that key's
			// deserialization and keeps none.
			break
		}
		if path, ok := cache(img); ok {
			h.imagePath = path
		}
		break
	}
	return h
}

// imageData is the spec's (iiibiiay) image struct.
type imageData struct {
	width, height, rowstride int32
	hasAlpha                 bool
	bitsPerSample, channels  int32
	data                     []byte
}

// parseImageData decodes an (iiibiiay) variant.
func parseImageData(v dbus.Variant) (imageData, bool) {
	fields, ok := v.Value().([]any)
	if !ok || len(fields) != 7 {
		return imageData{}, false
	}
	var img imageData
	ints := []*int32{&img.width, &img.height, &img.rowstride, nil, &img.bitsPerSample, &img.channels}
	for i, dst := range ints {
		if dst == nil {
			if img.hasAlpha, ok = fields[i].(bool); !ok {
				return imageData{}, false
			}
			continue
		}
		if *dst, ok = fields[i].(int32); !ok {
			return imageData{}, false
		}
	}
	if img.data, ok = fields[6].([]byte); !ok {
		return imageData{}, false
	}
	return img, true
}
