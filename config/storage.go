package config

// StorageConfig is ported from crates/wayle-config/src/schemas/modules/storage/mod.rs.
//
// Disk usage for a mount point.
type StorageConfig struct {
	// Polling interval in milliseconds.
	//
	// Faster polling increases CPU usage.
	PollIntervalMs uint64 `cfg:"poll-interval-ms"`
	// Mount point(s) to monitor (e.g., `"/"` or `["/", "/mnt/drive1"]`).
	MountPoint StorageMountPoint `cfg:"mount-point"`
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ percent }}` - Disk usage as integer (0-100)
	// - `{{ used_tib }}` - Used space in TiB
	// - `{{ used_gib }}` - Used space in GiB
	// - `{{ used_mib }}` - Used space in MiB
	// - `{{ used_auto }}` - Used space with auto unit (e.g., "128.5 GiB")
	// - `{{ total_tib }}` - Total space in TiB
	// - `{{ total_gib }}` - Total space in GiB
	// - `{{ total_mib }}` - Total space in MiB
	// - `{{ total_auto }}` - Total space with auto unit
	// - `{{ free_tib }}` - Free space in TiB
	// - `{{ free_gib }}` - Free space in GiB
	// - `{{ free_mib }}` - Free space in MiB
	// - `{{ free_auto }}` - Free space with auto unit
	// - `{{ filesystem }}` - Filesystem type (e.g., "ext4", "btrfs")
	//
	// ## Examples
	//
	// - `"{{ percent }}%"` - "45%"
	// - `"{{ used_auto }}/{{ total_auto }}"` - "128.5 GiB/512.0 GiB"
	// - `"{{ free_gib }} GiB free"` - "383.5 GiB free"
	Format string `cfg:"format"`
	// Icon name.
	IconName string `cfg:"icon-name"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Display label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
	// Dynamic color thresholds based on disk usage percentage.
	//
	// Entries are checked in order; the last matching entry wins for each
	// color slot. Use `above` for high-value warnings (e.g., disk nearly full).
	//
	// ## Example
	//
	// ```toml
	// [[modules.storage.thresholds]]
	// above = 70
	// icon-color = "status-warning"
	// label-color = "status-warning"
	//
	// [[modules.storage.thresholds]]
	// above = 90
	// icon-color = "status-error"
	// label-color = "status-error"
	// ```
	Thresholds []ThresholdEntry `cfg:"thresholds"`
}

// DefaultsStorage returns the schema defaults.
func DefaultsStorage() StorageConfig {
	return StorageConfig{
		PollIntervalMs: 30000,
		MountPoint:     StorageMountPoint{Paths: []string{"/"}},
		Format:         "{{ percent }}%",
		IconName:       "ld-hard-drive-symbolic",
		BorderShow:     false,
		BorderColor:    mustColor("yellow"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("yellow"),
		LabelShow:      true,
		LabelColor:     mustColor("yellow"),
		LabelMaxLength: 0,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ClickAction{},
		RightClick:     ClickAction{},
		MiddleClick:    ClickAction{},
		ScrollUp:       ClickAction{},
		ScrollDown:     ClickAction{},
		Thresholds:     []ThresholdEntry{},
	}
}

// Clicks returns the five input bindings.
func (c StorageConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

// StorageMountPoint is the mount-point value: one path, or a list
// (the untagged Single | Multiple enum; the form round-trips).
//
// Storage mount targets accepted by `mount-point`.
//
// Supports a single string for backwards compatibility or an array of paths.
type StorageMountPoint struct {
	// Paths are the mounts to monitor.
	Paths []string
	// Multiple records the list form.
	Multiple bool
}

// UnmarshalConfig implements Unmarshaler: a string, then an array of
// strings; anything else is serde's untagged error.
func (m *StorageMountPoint) UnmarshalConfig(v any) error {
	if s, ok := v.(string); ok {
		*m = StorageMountPoint{Paths: []string{s}}
		return nil
	}
	var paths []string
	if decodeInto(valueOf(&paths), v) == nil {
		*m = StorageMountPoint{Paths: paths, Multiple: true}
		return nil
	}
	return errUntagged("StorageMountPoint")
}

// MarshalConfig implements Marshaler.
func (m StorageMountPoint) MarshalConfig() any {
	if !m.Multiple && len(m.Paths) == 1 {
		return m.Paths[0]
	}
	out := make([]any, len(m.Paths))
	for i, p := range m.Paths {
		out[i] = p
	}
	return out
}

func (StorageMountPoint) configSchema(*schemaGen) Schema {
	return Schema{
		"description": typeDoc(typeOf[StorageMountPoint]()),
		"anyOf": []any{
			Schema{"type": "string", "description": "Monitor a single mount path."},
			Schema{"type": "array", "items": Schema{"type": "string"}, "description": "Monitor multiple mount paths."},
		},
	}
}
