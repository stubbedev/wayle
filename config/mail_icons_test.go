package config

import "testing"

func TestMailAccountResolvedIcon(t *testing.T) {
	custom, empty := "my-icon", ""
	for _, tc := range []struct {
		account MailAccount
		want    string
	}{
		{MailAccount{Provider: MailProviderGmail}, "si-gmail-symbolic"},
		{MailAccount{Provider: MailProviderGeneric}, "ld-mail-symbolic"},
		{MailAccount{Provider: MailProviderGmail, Icon: &custom}, "my-icon"},
		// An empty override is no override.
		{MailAccount{Provider: MailProviderProton, Icon: &empty}, "si-protonmail-symbolic"},
	} {
		if got := tc.account.ResolvedIcon(); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.account, got, tc.want)
		}
	}
	if _, ok := MailProviderGeneric.SimpleIconsSlug(); ok {
		t.Error("the generic provider has no brand icon to install")
	}
	if slug, ok := MailProviderOutlook.SimpleIconsSlug(); !ok || slug != "microsoftoutlook" {
		t.Errorf("outlook slug = %q %v", slug, ok)
	}
}

// TestMailProviderDefaultIcons pins MailProvider::default_icon for
// every provider: the brand glyph its slug installs, the generic one
// for the rest.
func TestMailProviderDefaultIcons(t *testing.T) {
	for p, want := range map[MailProvider]string{
		MailProviderGmail:    "si-gmail-symbolic",
		MailProviderOutlook:  "si-microsoftoutlook-symbolic",
		MailProviderIcloud:   "si-icloud-symbolic",
		MailProviderProton:   "si-protonmail-symbolic",
		MailProviderFastmail: "si-fastmail-symbolic",
		MailProviderYahoo:    "si-yahoo-symbolic",
		MailProviderGeneric:  "ld-mail-symbolic",
	} {
		if got := p.DefaultIcon(); got != want {
			t.Errorf("%v: %q, want %q", p, got, want)
		}
	}
}
