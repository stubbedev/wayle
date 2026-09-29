package bar

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/sni"
)

func TestSystrayIconName(t *testing.T) {
	cfg := config.DefaultsSystray()
	base := sni.Item{ID: "nm-applet", IconName: "nm-device-wifi"}
	if got := systrayIconName(cfg, base); got != "nm-device-wifi" {
		t.Errorf("plain = %q", got)
	}
	// NeedsAttention promotes the attention glyph.
	attention := base
	attention.Status = sni.StatusNeedsAttention
	attention.AttentionName = "nm-alert"
	if got := systrayIconName(cfg, attention); got != "nm-alert" {
		t.Errorf("attention = %q", got)
	}
	// The config override wins over both.
	cfg.Overrides["nm-applet"] = "ld-wifi-symbolic"
	if got := systrayIconName(cfg, attention); got != "ld-wifi-symbolic" {
		t.Errorf("override = %q", got)
	}
	// A pixmap-only item falls back to the placeholder.
	pixmapOnly := sni.Item{ID: "gpaste", IconPixmap: []sni.Pixmap{{Width: 16, Height: 16}}}
	if got := systrayIconName(config.DefaultsSystray(), pixmapOnly); got != "ld-package-symbolic" {
		t.Errorf("pixmap-only = %q", got)
	}
}

func TestSystrayBlacklist(t *testing.T) {
	patterns := []string{"*Indicator*", "sn-" + "*"}
	if !systrayBlacklisted(patterns, "MyIndicator", "") {
		t.Error("id match missed")
	}
	if !systrayBlacklisted(patterns, "", "sn-notifier") {
		t.Error("title match missed")
	}
	if systrayBlacklisted(patterns, "nm-applet", "Network") {
		t.Error("clean item flagged")
	}
}

func TestLoadFileAppliesSystray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.systray]\nicon-size = 20\nicon-scale = 1.5\nitem-gap = 0.4\nblacklist = [\"*Indicator*\"]\n\n[modules.systray.overrides]\n\"nm-applet\" = \"ld-wifi-symbolic\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Systray.IconSize != 20 || c.Systray.IconScale != 1.5 || c.Systray.ItemGap != 0.4 {
		t.Errorf("config = %+v", c.Systray)
	}
	if len(c.Systray.Blacklist) != 1 {
		t.Errorf("blacklist = %v", c.Systray.Blacklist)
	}
	if got := c.Systray.Overrides["nm-applet"]; got != "ld-wifi-symbolic" {
		t.Errorf("override = %q", got)
	}
}

func TestSystrayModuleReconciles(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	store := sni.NewStore()
	ctx.SNI = store

	module, err := Create("systray", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm := module.(*systrayModule)
	count := func() int { return len(tm.root.Children()) }
	if n := count(); n != 0 {
		t.Fatalf("empty tray rendered %d icons", n)
	}
	// An item lands in the row (headless: refresh by hand).
	store.Put(&sni.Item{Bus: ":1.1", Path: "/StatusNotifierItem", ID: "nm-applet", IconName: "nm-device-wifi"})
	<-store.Changes()
	tm.refresh()
	if n := count(); n != 1 {
		t.Fatalf("after put = %d icons", n)
	}
	// Blacklisting drops the icon on the next reconcile.
	cfg.Systray.Blacklist = []string{"nm-*"}
	tm.refresh()
	if n := count(); n != 0 {
		t.Fatalf("after blacklist = %d icons", n)
	}
	// Removal clears the row.
	cfg.Systray.Blacklist = nil
	store.Remove(":1.1", "/StatusNotifierItem")
	<-store.Changes()
	tm.refresh()
	if n := count(); n != 0 {
		t.Fatalf("after remove = %d icons", n)
	}

	// A missing store is a construction error.
	bad := newTestContext(t, cfg)
	bad.SNI = nil
	if _, err := Create("systray", bad); err == nil {
		t.Fatal("nil SNI store: want an error, got a module")
	}
}
