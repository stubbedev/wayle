package portal

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestInputCaptureCapturesNothing(t *testing.T) {
	r := newRig(t, func(b *Backend) {
		b.zones = func() []zone { return []zone{{2560, 1440, 0, 0}, {1920, 1080, 2560, -200}} }
	})
	if v, err := r.obj.GetProperty(InputCaptureIface + ".SupportedCapabilities"); err != nil || v.Value() != uint32(0) {
		t.Errorf("SupportedCapabilities = %v, %v", v, err)
	}
	code, res := r.interactive(t, InputCaptureIface+".CreateSession", handle, sessA, "org.app", "", Vardict{"capabilities": dbus.MakeVariant(uint32(7))})
	if code != ResponseSuccess || res["capabilities"].Value() != uint32(0) {
		t.Errorf("CreateSession = %d %v", code, res)
	}
	if v, err := r.client.Object(BusName, sessA).GetProperty(SessionIface + ".version"); err != nil || v.Value() != uint32(2) {
		t.Errorf("no Session at the handle: %v, %v", v, err)
	}
	var res2 map[string]dbus.Variant
	if err := r.obj.Call(InputCaptureIface+".CreateSession2", 0, sessB, "org.app", Vardict{}).Store(&res2); err != nil || res2["capabilities"].Value() != uint32(0) {
		t.Errorf("CreateSession2 = %v, %v", res2, err)
	}

	_, res = r.interactive(t, InputCaptureIface+".GetZones", handle, sessA, "org.app", Vardict{})
	var zones []zone
	_ = dbus.Store([]any{res["zones"].Value()}, &zones)
	if len(zones) != 2 || zones[1] != (zone{1920, 1080, 2560, -200}) || res["zone_set"].Value() != uint32(1) {
		t.Errorf("zones = %v %v", zones, res)
	}
	barriers := []Vardict{{"barrier_id": dbus.MakeVariant(uint32(3))}, {"barrier_id": dbus.MakeVariant(uint32(9))}, {"position": dbus.MakeVariant("x")}}
	_, res = r.interactive(t, InputCaptureIface+".SetPointerBarriers", handle, sessA, "org.app", Vardict{}, barriers, uint32(1))
	if got := res["failed_barriers"].Value().([]uint32); len(got) != 2 || got[0] != 3 || got[1] != 9 {
		t.Errorf("failed barriers = %v", got)
	}
	for _, m := range []string{"Enable", "Disable", "Release"} {
		if code, _ := r.interactive(t, InputCaptureIface+"."+m, sessA, "org.app", Vardict{}); code != ResponseSuccess {
			t.Errorf("%s = %d", m, code)
		}
	}
	if code, _ := r.interactive(t, InputCaptureIface+".Start", handle, sessA, "org.app", "", Vardict{}); code != ResponseSuccess {
		t.Errorf("Start = %d", code)
	}
}

func TestInputCaptureWithoutOutputs(t *testing.T) {
	r := newRig(t, func(b *Backend) { b.zones = func() []zone { return nil } })
	_, res := r.interactive(t, InputCaptureIface+".GetZones", handle, sessA, "org.app", Vardict{})
	if z, ok := res["zones"].Value().([][]any); !ok || len(z) != 0 {
		t.Errorf("zones = %#v, want an empty a(uuii)", res["zones"].Value())
	}
}
