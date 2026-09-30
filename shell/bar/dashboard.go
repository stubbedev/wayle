package bar

import (
	"os"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// dashboardFallbackIcon is the wayle glyph shown when no distro icon
// resolves (dashboard helpers.rs FALLBACK_ICON).
const dashboardFallbackIcon = "cm-wayle-symbolic"

// osReleasePaths are read in order; the first readable one wins.
var osReleasePaths = []string{"/etc/os-release", "/usr/lib/os-release"}

// distroInfo is the os-release subset the icon pick reads.
type distroInfo struct {
	id   string
	logo string
}

// parseOSRelease reads ID and LOGO from an os-release body; ok is false
// without an ID (detect_distro's `id?`).
func parseOSRelease(body string) (distroInfo, bool) {
	var info distroInfo
	hasID := false
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "ID="); ok {
			info.id, hasID = unquoteOSRelease(v), true
		} else if v, ok := strings.CutPrefix(line, "LOGO="); ok {
			info.logo = unquoteOSRelease(v)
		}
	}
	return info, hasID
}

// unquoteOSRelease strips surrounding double, then single quotes.
func unquoteOSRelease(s string) string {
	return strings.Trim(strings.Trim(s, `"`), "'")
}

// detectDistro reads the first os-release file present.
func detectDistro() (distroInfo, bool) {
	for _, path := range osReleasePaths {
		body, err := os.ReadFile(path) //nolint:gosec // fixed system paths
		if err == nil {
			return parseOSRelease(string(body))
		}
	}
	return distroInfo{}, false
}

// bundledDistroIcons maps os-release IDs onto the bundled icon set
// (helpers.rs bundled_icon_for_distro), verbatim.
var bundledDistroIcons = map[string]string{
	"alpine":              "si-alpinelinux-symbolic",
	"almalinux":           "si-almalinux-symbolic",
	"arch":                "si-archlinux-symbolic",
	"artix":               "si-artixlinux-symbolic",
	"asahi":               "si-asahilinux-symbolic",
	"cachyos":             "cm-cachyos-symbolic",
	"centos":              "si-centos-symbolic",
	"debian":              "si-debian-symbolic",
	"deepin":              "si-deepin-symbolic",
	"elementary":          "si-elementary-symbolic",
	"endeavouros":         "si-endeavouros-symbolic",
	"fedora":              "si-fedora-symbolic",
	"garuda":              "si-garudalinux-symbolic",
	"gentoo":              "si-gentoo-symbolic",
	"kali":                "si-kalilinux-symbolic",
	"kdeneon":             "si-kdeneon-symbolic",
	"kubuntu":             "si-kubuntu-symbolic",
	"linuxmint":           "si-linuxmint-symbolic",
	"lubuntu":             "si-lubuntu-symbolic",
	"manjaro":             "si-manjaro-symbolic",
	"mx":                  "si-mxlinux-symbolic",
	"nixos":               "si-nixos-symbolic",
	"nobara":              "cm-nobara-symbolic",
	"opensuse-leap":       "si-opensuse-symbolic",
	"opensuse-tumbleweed": "si-opensuse-symbolic",
	"opensuse":            "si-opensuse-symbolic",
	"pop":                 "si-popos-symbolic",
	"rhel":                "si-redhat-symbolic",
	"rocky":               "si-rockylinux-symbolic",
	"slackware":           "si-slackware-symbolic",
	"solus":               "si-solus-symbolic",
	"steamos":             "si-steam-symbolic",
	"ubuntu":              "si-ubuntu-symbolic",
	"ubuntumate":          "si-ubuntumate-symbolic",
	"void":                "si-voidlinux-symbolic",
	"xubuntu":             "si-xubuntu-symbolic",
	"zorin":               "si-zorin-symbolic",
}

// dashboardIcon is helpers.rs build_icon: the override when set;
// otherwise the bundled distro icon, the os-release LOGO, then
// distributor-logo-<id>, each only when the theme has it; the wayle
// glyph last.
func dashboardIcon(override string, distro func() (distroInfo, bool), exists func(string) bool) string {
	if override != "" {
		return override
	}
	info, ok := distro()
	if !ok {
		return dashboardFallbackIcon
	}
	if icon, ok := bundledDistroIcons[info.id]; ok && exists(icon) {
		return icon
	}
	if info.logo != "" && exists(info.logo) {
		return info.logo
	}
	if logo := "distributor-logo-" + info.id; exists(logo) {
		return logo
	}
	return dashboardFallbackIcon
}

// dashboardModule is the icon-only quick-access button; its default
// left-click opens the dashboard dropdown through the shared binding
// path.
type dashboardModule struct {
	icon *widget.Icon
}

func newDashboard(ctx ModuleContext) (Module, error) {
	cfg := ctx.Config.Dashboard
	name := dashboardIcon(cfg.IconOverride, detectDistro, widget.ThemeIconExists)
	// The icon always shows and the label never does (the schema's
	// hidden icon-show/label-show).
	icon := moduleIcon(ctx, config.IconConfig{Show: true, Name: name, Color: cfg.IconColor})
	return &dashboardModule{icon: icon}, nil
}

func (m *dashboardModule) Root() widget.Widget { return m.icon }
