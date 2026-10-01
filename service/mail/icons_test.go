package mail

import (
	"context"
	"slices"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/icons"
	"github.com/stubbedev/wayle/internal/icons/icontest"
)

func account(p config.MailProvider, icon *string) config.MailAccount {
	return config.MailAccount{Provider: p, Icon: icon}
}

func TestProviderIconSlugs(t *testing.T) {
	own, empty := "ld-star-symbolic", ""
	accounts := []config.MailAccount{
		account(config.MailProviderYahoo, nil),
		account(config.MailProviderGmail, nil),
		account(config.MailProviderGmail, nil),     // deduplicated
		account(config.MailProviderOutlook, &own),  // the user's own icon
		account(config.MailProviderProton, &empty), // an empty override is none
		account(config.MailProviderGeneric, nil),   // no brand icon
		account(config.MailProviderFastmail, nil),  // already installed
	}
	installed := func(name string) bool { return name == "si-fastmail-symbolic" }
	got := providerIconSlugs(accounts, installed)
	if want := []string{"gmail", "protonmail", "yahoo"}; !slices.Equal(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
}

// TestProviderIconsAreTheDefaultIcons pins the install to the name the
// account resolves: the icon a brand install writes is the provider's
// default icon.
func TestProviderIconsAreTheDefaultIcons(t *testing.T) {
	for _, p := range []config.MailProvider{
		config.MailProviderGmail, config.MailProviderOutlook, config.MailProviderIcloud,
		config.MailProviderProton, config.MailProviderFastmail, config.MailProviderYahoo,
	} {
		slug, ok := p.SimpleIconsSlug()
		if !ok || icons.SimpleIcons.InstalledName(slug) != p.DefaultIcon() {
			t.Errorf("%s: installs %q, resolves %q", p, icons.SimpleIcons.InstalledName(slug), p.DefaultIcon())
		}
	}
}

func TestInstallProviderIcons(t *testing.T) {
	t.Setenv("XDG_DATA_DIRS", "")
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24z"/></svg>`
	cdn := icontest.NewCDN(map[string]string{icons.SimpleIcons.CDNURL("gmail"): svg})
	m := icons.NewManagerWith(icons.RegistryAt(t.TempDir()), cdn.Client())
	accounts := []config.MailAccount{account(config.MailProviderGmail, nil), account(config.MailProviderYahoo, nil)}

	InstallProviderIcons(context.Background(), accounts, m)

	if !m.IsInstalled("si-gmail-symbolic") {
		t.Error("gmail's brand icon was not installed")
	}
	if m.IsInstalled("si-yahoo-symbolic") {
		t.Error("a failed fetch installed something")
	}
	first := len(cdn.Requested())

	// The next start fetches only what is still missing.
	InstallProviderIcons(context.Background(), accounts, m)
	if again := cdn.Requested()[first:]; !slices.Equal(again, []string{icons.SimpleIcons.CDNURL("yahoo")}) {
		t.Errorf("second run fetched %v, want only the missing yahoo icon", again)
	}
	before := len(cdn.Requested())
	InstallProviderIcons(context.Background(), []config.MailAccount{account(config.MailProviderGeneric, nil)}, m)
	if len(cdn.Requested()) != before {
		t.Error("a generic account fetched something")
	}
}
