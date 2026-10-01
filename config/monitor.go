package config

import "strings"

// monitorTarget is "primary" (the zero value) or a connector name; the
// OSD and the notification popups share it (osd/types.rs OsdMonitor,
// notification/types.rs PopupMonitor). "primary" matches
// case-insensitively and serializes back as "primary".
type monitorTarget struct {
	connector string
}

func onConnector(name string) monitorTarget {
	if strings.EqualFold(name, "primary") {
		return monitorTarget{}
	}
	return monitorTarget{connector: name}
}

// IsPrimary reports whether the surface follows the primary monitor.
func (m monitorTarget) IsPrimary() bool { return m.connector == "" }

// Connector is the configured connector name ("" for primary).
func (m monitorTarget) Connector() string { return m.connector }

// Matches reports whether the target selects the named output, given
// the name of the primary (first) output.
func (m monitorTarget) Matches(output, primary string) bool {
	if m.IsPrimary() {
		return output == primary
	}
	return output == m.connector
}

// UnmarshalConfig implements Unmarshaler.
func (m *monitorTarget) UnmarshalConfig(v any) error {
	s, ok := v.(string)
	if !ok {
		return invalidType(v, `"primary" or a connector name like "DP-1"`)
	}
	*m = onConnector(s)
	return nil
}

// MarshalConfig implements Marshaler.
func (m monitorTarget) MarshalConfig() any {
	if m.IsPrimary() {
		return "primary"
	}
	return m.connector
}

func (monitorTarget) configSchema(*schemaGen) Schema {
	return Schema{
		"type":        "string",
		"description": `"primary" or a monitor connector name (e.g. "DP-1")`,
		"default":     "primary",
	}
}

// OsdMonitor is the OSD's target monitor.
type OsdMonitor struct{ monitorTarget }

// OsdOnConnector targets one connector ("primary" stays the primary).
func OsdOnConnector(name string) OsdMonitor { return OsdMonitor{onConnector(name)} }

// PopupMonitor is the notification popups' target monitor.
type PopupMonitor struct{ monitorTarget }

// PopupOnConnector targets one connector ("primary" stays the primary).
func PopupOnConnector(name string) PopupMonitor { return PopupMonitor{onConnector(name)} }
