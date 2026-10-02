package portal

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/shell/portaldialogs"
)

const (
	accessDialog = "org.freedesktop.impl.portal.Access.AccessDialog"
	userInfoCall = "org.freedesktop.impl.portal.Account.GetUserInformation"
	chooseApp    = "org.freedesktop.impl.portal.AppChooser.ChooseApplication"
	prepInstall  = "org.freedesktop.impl.portal.DynamicLauncher.PrepareInstall"
)

// eachDialog calls every dialog-backed method once.
func (r *rig) eachDialog(t *testing.T) map[string]uint32 {
	t.Helper()
	codes := map[string]uint32{}
	codes[accessDialog], _ = r.interactive(t, accessDialog, handle, "org.app", "", "T", "S", "B", Vardict{})
	codes[userInfoCall], _ = r.interactive(t, userInfoCall, handle, "org.app", "", Vardict{})
	codes[chooseApp], _ = r.interactive(t, chooseApp, handle, "org.app", "", []string{"a.desktop"}, Vardict{})
	codes[prepInstall], _ = r.interactive(t, prepInstall, handle, "org.app", "", "App", dbus.MakeVariant("icon"), Vardict{})
	return codes
}

func TestDialogsAnswerFromTheShell(t *testing.T) {
	r := newRig(t, nil)
	// No shell: every dialog fails as other, never a silent grant.
	for method, code := range r.eachDialog(t) {
		if code != ResponseOther {
			t.Errorf("%s without a shell = %d", method, code)
		}
	}
	d := serveDialogs(t, r, false)
	for method, code := range r.eachDialog(t) {
		if code != ResponseCancelled {
			t.Errorf("%s declined = %d", method, code)
		}
	}
	d.yes.Store(true)
	for method, code := range r.eachDialog(t) {
		if code != ResponseSuccess {
			t.Errorf("%s accepted = %d", method, code)
		}
	}
}

func TestAccessDialogLabels(t *testing.T) {
	r := newRig(t, nil)
	d := serveDialogs(t, r, true)
	r.interactive(t, accessDialog, handle, "org.app", "", "T", "S", "B", Vardict{})
	if got := d.asked.Load()[0]; got != (portaldialogs.AccessRequest{Title: "T", Subtitle: "S", Body: "B", GrantLabel: "Allow", DenyLabel: "Deny"}) {
		t.Errorf("defaults = %+v", got)
	}
	r.interactive(t, accessDialog, handle, "org.app", "", "T", "S", "B", Vardict{
		"grant_label": dbus.MakeVariant("Share"), "deny_label": dbus.MakeVariant("Keep"), "icon": dbus.MakeVariant("camera-web"),
	})
	if got := d.asked.Load()[0].(portaldialogs.AccessRequest); got.GrantLabel != "Share" || got.DenyLabel != "Keep" || got.Icon != "camera-web" {
		t.Errorf("labels = %+v", got)
	}
}

func TestAccountSharesTheUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "ada")
	r := newRig(t, nil)
	d := serveDialogs(t, r, true)
	_, res := r.interactive(t, userInfoCall, handle, "org.app", "", Vardict{"reason": dbus.MakeVariant("to greet you")})
	if d.asked.Load()[0] != "to greet you" {
		t.Errorf("reason = %v", d.asked.Load())
	}
	if res["id"].Value() != "ada" || res["name"].Value() == "" {
		t.Errorf("results = %v", res)
	}
	if _, ok := res["image"]; ok {
		t.Error("an image without ~/.face")
	}
	face := filepath.Join(home, ".face")
	if err := os.WriteFile(face, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, res = r.interactive(t, userInfoCall, handle, "org.app", "", Vardict{}); res["image"].Value() != "file://"+face {
		t.Errorf("image = %v", res["image"])
	}
}

func TestAppChooserAndLauncherResults(t *testing.T) {
	r := newRig(t, nil)
	d := serveDialogs(t, r, true)
	_, res := r.interactive(t, chooseApp, handle, "org.app", "", []string{"a.desktop", "b.desktop"}, Vardict{
		"content_type": dbus.MakeVariant("image/png"), "uri": dbus.MakeVariant("file:///x.png"),
	})
	asked := d.asked.Load()
	if res["choice"].Value() != "org.chosen.desktop" || !slices.Equal(asked[0].([]string), []string{"a.desktop", "b.desktop"}) || asked[1] != "image/png" || asked[2] != "file:///x.png" {
		t.Errorf("choice = %v, asked %v", res, asked)
	}
	if err := r.obj.Call("org.freedesktop.impl.portal.AppChooser.UpdateChoices", 0, handle, []string{"c"}).Err; err != nil {
		t.Error(err)
	}

	icon := dbus.MakeVariant(gicon{"bytes", dbus.MakeVariant([]byte{1, 2})})
	_, res = r.interactive(t, prepInstall, handle, "org.app", "", "My App", icon, Vardict{})
	// The icon goes back as it came, a (sv) GIcon: the call surviving at
	// all is the wire check (a struct re-encoded as an array is a
	// malformed reply the bus disconnects the backend for).
	if got, ok := res["icon"].Value().([]any); res["name"].Value() != "My App" || !ok || len(got) != 2 || got[0] != "bytes" || !slices.Equal(got[1].(dbus.Variant).Value().([]byte), []byte{1, 2}) {
		t.Errorf("launcher results = %v", res)
	}
	if got := d.asked.Load(); got[0] != "My App" || got[1] != "" {
		t.Errorf("ConfirmInstall asked %v", got)
	}
	var code uint32
	if err := r.obj.Call("org.freedesktop.impl.portal.DynamicLauncher.RequestInstallToken", 0, "org.app", Vardict{}).Store(&code); err != nil || code != ResponseSuccess {
		t.Errorf("RequestInstallToken = %d, %v", code, err)
	}
	v, err := r.obj.GetProperty("org.freedesktop.impl.portal.DynamicLauncher.SupportedLauncherTypes")
	if err != nil || v.Value() != uint32(3) {
		t.Errorf("SupportedLauncherTypes = %v, %v", v, err)
	}
}
