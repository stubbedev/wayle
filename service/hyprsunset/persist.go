package hyprsunset

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// overrideFile is the record's name inside the wayle state dir.
const overrideFile = "hyprsunset-override"

// overrideMaxAge discards records old enough to span a whole day/night
// cycle (persist.rs MAX_AGE).
const overrideMaxAge = 12 * time.Hour

// Override is a manual toggle remembered across restarts: the filter
// state it chose and, when the auto-schedule ran, the solar phase it
// was made in (zero Phase for none).
type Override struct {
	Phase   Phase
	Enabled bool
}

// SaveOverride records a toggle made at now (persist.rs save); write
// failures are quiet, as the record is a convenience.
func SaveOverride(dir string, o Override, now time.Time) {
	enabled := "off"
	if o.Enabled {
		enabled = "on"
	}
	body := o.Phase.String() + " " + enabled + " " + now.UTC().Format(time.RFC3339) + "\n"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, overrideFile), []byte(body), 0o600)
}

// ClearOverride drops the record (a sunrise/sunset crossing ends it).
func ClearOverride(dir string) {
	_ = os.Remove(filepath.Join(dir, overrideFile))
}

// LoadOverride reads the record; false when absent, malformed, or
// stale.
func LoadOverride(dir string, now time.Time) (Override, bool) {
	body, err := os.ReadFile(filepath.Join(dir, overrideFile)) //nolint:gosec // wayle's own state dir
	if err != nil {
		return Override{}, false
	}
	return parseOverride(string(body), now)
}

// parseOverride is persist.rs parse: "<phase> <on|off> <rfc3339>".
func parseOverride(body string, now time.Time) (Override, bool) {
	fields := strings.Fields(body)
	if len(fields) < 3 {
		return Override{}, false
	}
	saved, err := time.Parse(time.RFC3339, fields[2])
	if err != nil {
		return Override{}, false
	}
	if now.Sub(saved) > overrideMaxAge {
		return Override{}, false
	}
	var phase Phase
	switch fields[0] {
	case "day":
		phase = PhaseDay
	case "night":
		phase = PhaseNight
	case "none":
	default:
		return Override{}, false
	}
	return Override{Phase: phase, Enabled: fields[1] == "on"}, true
}
