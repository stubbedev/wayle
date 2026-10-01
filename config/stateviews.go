package config

// State views for the modules whose icon (and, for power profiles,
// color) follows a service state: the schema declares one key per
// state, the bar looks them up by state name.

// Recorder states.
const (
	RecorderIdle      = "idle"
	RecorderRecording = "recording"
	RecorderPaused    = "paused"
)

// Icons maps each recorder state to its icon.
func (c RecorderConfig) Icons() map[string]IconConfig {
	return map[string]IconConfig{
		RecorderIdle:      IconWith(c.IconShow, c.IconIdle, c.IconColor),
		RecorderRecording: IconWith(c.IconShow, c.IconRecording, c.IconColor),
		RecorderPaused:    IconWith(c.IconShow, c.IconPaused, c.IconColor),
	}
}

// Idle-inhibit states.
const (
	IdleInhibitActive   = "active"
	IdleInhibitInactive = "inactive"
)

// Icons maps each idle-inhibit state to its icon.
func (c IdleInhibitConfig) Icons() map[string]IconConfig {
	return map[string]IconConfig{
		IdleInhibitActive:   IconWith(c.IconShow, c.IconActive, c.IconColor),
		IdleInhibitInactive: IconWith(c.IconShow, c.IconInactive, c.IconColor),
	}
}

// Power profiles, as power-profiles-daemon names them.
const (
	ProfilePowerSaver  = "power-saver"
	ProfileBalanced    = "balanced"
	ProfilePerformance = "performance"
)

// Icons maps each power profile to its icon.
func (c PowerProfilesConfig) Icons() map[string]IconConfig {
	return map[string]IconConfig{
		ProfilePowerSaver:  IconWith(c.IconShow, c.IconPowerSaver, c.IconColor),
		ProfileBalanced:    IconWith(c.IconShow, c.IconBalanced, c.IconColor),
		ProfilePerformance: IconWith(c.IconShow, c.IconPerformance, c.IconColor),
	}
}

// Colors maps each power profile to its icon color.
func (c PowerProfilesConfig) Colors() map[string]ColorValue {
	return map[string]ColorValue{
		ProfilePowerSaver:  c.ColorPowerSaver,
		ProfileBalanced:    c.ColorBalanced,
		ProfilePerformance: c.ColorPerformance,
	}
}

// Treeman health buckets.
const (
	TreemanBucketStable = "stable"
	TreemanBucketUp     = "up"
	TreemanBucketDown   = "down"
	TreemanBucketFailed = "failed"
)

// Icons maps each treeman bucket to its icon.
func (c TreemanConfig) Icons() map[string]IconConfig {
	return map[string]IconConfig{
		TreemanBucketStable: IconWith(c.IconShow, c.IconName, c.IconColor),
		TreemanBucketUp:     IconWith(c.IconShow, c.IconPreparing, c.IconColor),
		TreemanBucketDown:   IconWith(c.IconShow, c.IconTearingDown, c.IconColor),
		TreemanBucketFailed: IconWith(c.IconShow, c.IconFailed, c.IconColor),
	}
}

// IconOnView is the hyprsunset icon while the filter runs.
func (c HyprsunsetConfig) IconOnView() IconConfig { return IconWith(c.IconShow, c.IconOn, c.IconColor) }

// IconOffView is the hyprsunset icon while the filter is off.
func (c HyprsunsetConfig) IconOffView() IconConfig {
	return IconWith(c.IconShow, c.IconOff, c.IconColor)
}
