package settings

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// generalPage is pages/general.
func generalPage(*config.Config) pageSpec {
	return pageSpec{
		id: "general", navKey: "settings-nav-general", icon: "ld-settings-symbolic",
		header: "settings-page-general",
		sections: []sectionSpec{
			{title: "settings-section-fonts", rows: []rowSpec{field("general.font-sans", fontRow), field("general.font-mono", fontRow)}},
			{title: "settings-section-scale-rounding", rows: fields("styling.scale", "styling.rounding")},
			{title: "settings-section-display", rows: fields("general.tearing-mode")},
		},
	}
}

// lockPage is pages/lock.
func lockPage(*config.Config) pageSpec {
	return pageSpec{
		id: "lock", navKey: "settings-nav-lock-page", icon: "ld-lock-symbolic",
		header: "settings-page-lock",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("lock.enabled")},
			{title: "settings-section-background", rows: []rowSpec{
				field("lock.background-mode"), field("lock.background-image", filePath),
				field("lock.background-color"), field("lock.blur"),
			}},
			{title: "settings-section-clock", rows: fields("lock.show-clock", "lock.clock-format", "lock.date-format")},
			{title: "settings-section-security", rows: fields("lock.grace-period-ms", "lock.max-attempts",
				"lock.show-failed-attempts", "lock.blank-timeout-ms", "lock.pam-service")},
			{title: "settings-section-animation", rows: surfaceAnimationRows("animations.lock")},
		},
	}
}

// greeterPage is pages/greeter, with the apply footer.
func greeterPage(*config.Config) pageSpec {
	return pageSpec{
		id: "greeter", navKey: "settings-nav-greeter-page", icon: "ld-monitor-symbolic",
		header: "settings-page-greeter",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("greeter.show-user-list", "greeter.show-power-buttons")},
			{title: "settings-section-background", rows: []rowSpec{
				field("greeter.background-mode"), field("greeter.background-image", filePath), field("greeter.background-color"),
			}},
			{title: "settings-section-clock", rows: fields("greeter.show-clock", "greeter.clock-format", "greeter.date-format")},
			{title: "settings-section-cursor", rows: fields("greeter.cursor-theme", "greeter.cursor-size")},
		},
		footer: greeterApplyFooter,
	}
}

// Greeter apply texts (apply.rs: not localized in Rust either).
const (
	greeterApplyLabel  = "Apply to login screen"
	greeterApplyFailed = "Apply failed — see logs"
	greeterApplyHint   = "Writes the system login-screen config; asks for admin authentication."
)

// greeterApplyFooter is build_footer: the button pushing the greeter
// settings to the login screen, and its hint.
func greeterApplyFooter(k *kit) widget.Widget {
	box := widget.NewBox(widget.Column, 6, 0)
	box.AddClass("settings-section")
	label := k.label(greeterApplyLabel)
	box.AppendAligned(k.button(label, func() {
		if err := applyGreeter(k.store); err != nil {
			log.Printf("settings: greeter apply failed: %v", err)
			label.SetText(greeterApplyFailed)
			return
		}
		log.Printf("settings: greeter apply: launched pkexec wayle-greeter apply-config")
	}, "primary"), false, widget.AlignStart)
	hint := k.label(greeterApplyHint, "settings-section-title")
	hint.SetWrap(true)
	box.Append(hint, false)
	return box
}

// applyGreeter stages the [greeter] keys the login screen takes and
// hands them to `pkexec wayle-greeter apply-config` (dispatch).
func applyGreeter(s store) error {
	greeter := make(map[string]any, len(config.GreeterApplyKeys))
	for _, key := range config.GreeterApplyKeys {
		if v := s.value("greeter." + key); v != nil {
			greeter[key] = v
		}
	}
	body, err := config.TOMLDocument(map[string]any{"greeter": greeter})
	if err != nil {
		return err
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("wayle-greeter-apply-%d.toml", os.Getpid()))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("stage %s: %w", path, err)
	}
	return spawnGreeterApply(path)
}

// spawnGreeterApply starts pkexec without waiting: the polkit agent
// asks, and wayle-greeter reports on its own (a test seam).
var spawnGreeterApply = func(staged string) error {
	cmd := exec.Command("pkexec", "wayle-greeter", "apply-config", staged) //nolint:gosec // fixed program, staged path
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn pkexec: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
