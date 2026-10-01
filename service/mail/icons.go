package mail

import (
	"context"
	"log"
	"slices"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/icons"
)

// providerIconSlugs is install_provider_icons' selection: the Simple
// Icons slug of every account left on its provider's brand icon whose
// glyph is not installed, sorted and deduplicated. An account with its
// own icon is the user's choice and installs nothing.
//
// Rust asks is_installed for "si-<slug>" and so never sees the
// "si-<slug>-symbolic" file its install writes, refetching on every
// start; this asks for the file the install writes.
func providerIconSlugs(accounts []config.MailAccount, installed func(name string) bool) []string {
	var slugs []string
	for _, a := range accounts {
		if a.Icon != nil && *a.Icon != "" {
			continue
		}
		slug, ok := a.Provider.SimpleIconsSlug()
		if ok && !installed(icons.SimpleIcons.InstalledName(slug)) && !slices.Contains(slugs, slug) {
			slugs = append(slugs, slug)
		}
	}
	slices.Sort(slugs)
	return slugs
}

// InstallProviderIcons is install_provider_icons: a best-effort install
// of the configured providers' brand icons. Failures (offline, an
// unknown slug) are logged and leave the account on the generic icon.
func InstallProviderIcons(ctx context.Context, accounts []config.MailAccount, m *icons.Manager) {
	slugs := providerIconSlugs(accounts, m.IsInstalled)
	if len(slugs) == 0 {
		return
	}
	result, err := m.Install(ctx, icons.SimpleIcons, slugs)
	if err != nil {
		log.Printf("mail: provider icon install failed: %v", err)
		return
	}
	if len(result.Installed) > 0 {
		log.Printf("mail: installed provider icons %v", result.Installed)
	}
	for _, f := range result.Failed {
		log.Printf("mail: provider icon %s install failed: %s", f.Slug, f.Error)
	}
}
