// Package i18n is wayle's Fluent localization: a port of the runtime
// the Rust crates get from i18n-embed 0.16 (FluentLanguageLoader),
// fluent-bundle 0.16, fluent-syntax 0.12, and fluent-langneg 0.13.
//
// Three domains exist, as in Rust:
//
//   - the shell domain (crates/wayle-shell-core/src/i18n.rs, the t!/td!
//     macros): bar labels, dropdowns, OSD, notification popups. T and
//     Attr read it.
//   - the settings domain (crates/wayle-i18n/src/lib.rs, t/t_attr): the
//     settings GUI and config field labels. Settings returns it.
//   - the greeter domain (crates/wayle-greeter/src/i18n.rs, t!): the
//     login screen's labels. Greeter returns it.
//
// Each domain negotiates the desktop locale (LANGUAGE, LC_ALL,
// LC_MESSAGES, LANG) against its embedded locales once, on first use,
// with en-US as the always-loaded fallback. Lookups fall through the
// negotiated languages in order; a missing message yields i18n-embed's
// "No localization for id" marker rather than an empty string.
//
// Interpolated placeables are wrapped in Unicode isolation marks
// (U+2068 ... U+2069) exactly as fluent-bundle does by default, which
// i18n-embed never turns off.
package i18n

import (
	"os"
	"sync"

	greeterlocales "github.com/stubbedev/wayle/crates/wayle-greeter/locales"
	settingslocales "github.com/stubbedev/wayle/crates/wayle-i18n/locales"
	shelllocales "github.com/stubbedev/wayle/crates/wayle-shell-core/locales"
)

// domain lazily builds one loader from the desktop locale, like the
// Rust OnceLock<FluentLanguageLoader>.
type domain struct {
	assets Assets
	once   sync.Once
	loader *Loader
}

func (d *domain) get() *Loader {
	d.once.Do(func() {
		l, err := Select(d.assets, requestedLanguages(os.Getenv))
		if err != nil {
			// The embedded fallback is validated by tests; failing here
			// is a build defect, as the Rust expect() treats it.
			panic(err)
		}
		d.loader = l
	})
	return d.loader
}

var (
	shellDomain    = &domain{assets: NewAssets(shelllocales.FS)}
	settingsDomain = &domain{assets: NewAssets(settingslocales.FS)}
	greeterDomain  = &domain{assets: NewFileAssets(greeterlocales.FS, "wayle-greeter.ftl")}
)

// Shell returns the shell-domain loader (wayle-shell-core's loader()).
func Shell() *Loader { return shellDomain.get() }

// Settings returns the settings-domain loader (wayle-i18n's loader()).
func Settings() *Loader { return settingsDomain.get() }

// Greeter returns the greeter-domain loader (wayle-greeter's loader()).
func Greeter() *Loader { return greeterDomain.get() }

// T formats a shell message: wayle-shell-core's t!/td!.
func T(id string, args ...Arg) string { return Shell().Get(id, args...) }

// Attr formats a shell message attribute.
func Attr(id, attr string, args ...Arg) string { return Shell().Attr(id, attr, args...) }
