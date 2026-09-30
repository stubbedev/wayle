package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadMail(t *testing.T, body string) (MailConfig, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(path)
	if err != nil {
		return MailConfig{}, err
	}
	return c.Mail, nil
}

func TestMailDefaultsMatchTheSchema(t *testing.T) {
	cfg := DefaultsMail()
	if cfg.Query != "tag:unread" {
		t.Errorf("query = %q, want tag:unread", cfg.Query)
	}
	if cfg.Notify {
		t.Error("notify defaults on, want off")
	}
	if cfg.NotifySummary != "{{ sender }}" || cfg.NotifyBody != "{{ subject }}" {
		t.Errorf("notify templates = %q / %q", cfg.NotifySummary, cfg.NotifyBody)
	}
	if cfg.IconName != "ld-mail-symbolic" {
		t.Errorf("icon-name = %q", cfg.IconName)
	}
}

func TestMailNotifyKeysAndProviders(t *testing.T) {
	cfg, err := loadMail(t, `[modules.mail]
notify = true
notify-summary = "{{ new }} from {{ sender }}"
notify-body = "{{ count }} unread"
icon-name = "my-mail"

[[modules.mail.accounts]]
name = "Work"
query = "tag:unread and folder:work"
provider = "fastmail"

[[modules.mail.accounts]]
name = "Home"
query = "tag:unread and folder:home"
icon = "custom-icon"
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Notify || cfg.NotifySummary != "{{ new }} from {{ sender }}" || cfg.NotifyBody != "{{ count }} unread" || cfg.IconName != "my-mail" {
		t.Errorf("mail = %+v", cfg)
	}
	if got := cfg.Accounts[0].ResolvedIcon(); got != "si-fastmail-symbolic" {
		t.Errorf("provider icon = %q", got)
	}
	if cfg.Accounts[1].Provider != MailProviderGeneric {
		t.Errorf("default provider = %q, want generic", cfg.Accounts[1].Provider)
	}
	if got := cfg.Accounts[1].ResolvedIcon(); got != "custom-icon" {
		t.Errorf("own icon = %q, want it over the provider's", got)
	}
}

func TestMailUnknownProviderIsALoadError(t *testing.T) {
	_, err := loadMail(t, "[[modules.mail.accounts]]\nname = \"Work\"\nquery = \"tag:unread\"\nprovider = \"hotmail\"\n")
	if err == nil || !strings.Contains(err.Error(), "hotmail") {
		t.Fatalf("err = %v, want an invalid-provider error naming it", err)
	}
}
