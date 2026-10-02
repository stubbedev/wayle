package portal

import "github.com/stubbedev/wayle/internal/dbusx"

// lockdownIface is the read-only policy the frontend consults before
// allowing printing, saving, devices and so on. Wayle locks nothing
// down: every property is false (lockdown.rs).
func lockdownIface() dbusx.Interface {
	props := dbusx.Getters{"version": func() any { return uint32(1) }}
	for _, name := range []string{
		"disable-printing", "disable-save-to-disk", "disable-application-handlers",
		"disable-location", "disable-camera", "disable-microphone", "disable-sound-output",
	} {
		props[name] = func() any { return false }
	}
	return dbusx.Interface{Name: "org.freedesktop.impl.portal.Lockdown", Methods: struct{}{}, Properties: props}
}
