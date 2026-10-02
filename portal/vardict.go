package portal

import "github.com/godbus/dbus/v5"

// Vardict is a portal a{sv}: every interactive method's options and
// results (dbus_util.rs).
type Vardict = map[string]dbus.Variant

// Response codes of an interactive method's (u, a{sv}) reply
// (response.rs).
const (
	ResponseSuccess   uint32 = 0
	ResponseCancelled uint32 = 1
	ResponseOther     uint32 = 2
)

// optU32 reads a u32 option, also accepting a u64 that fits.
func optU32(options Vardict, key string) (uint32, bool) {
	switch v := options[key].Value().(type) {
	case uint32:
		return v, true
	case uint64:
		if v <= 1<<32-1 {
			return uint32(v), true
		}
	}
	return 0, false
}

// optBool reads a bool option.
func optBool(options Vardict, key string) (value, ok bool) {
	value, ok = options[key].Value().(bool)
	return value, ok
}

// optString reads a string option.
func optString(options Vardict, key string) (string, bool) {
	s, ok := options[key].Value().(string)
	return s, ok
}

// stringOr is optString with a fallback for a missing or mistyped key.
func stringOr(options Vardict, key, fallback string) string {
	if s, ok := optString(options, key); ok {
		return s
	}
	return fallback
}
