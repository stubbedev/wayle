package bar

import (
	"testing"

	"github.com/stubbedev/wayle/config"
)

func TestMailLabel(t *testing.T) {
	cfg := config.DefaultsMail()
	if got := mailLabel(cfg.Format, 3, cfg.HideWhenZero); got != "3" {
		t.Errorf("= %q, want 3", got)
	}
	// hide-when-zero blanks the label at zero.
	if got := mailLabel(cfg.Format, 0, cfg.HideWhenZero); got != "" {
		t.Errorf("zero = %q, want empty", got)
	}
	if got := mailLabel(cfg.Format, 0, false); got != "0" {
		t.Errorf("zero shown = %q, want 0", got)
	}
	if got := mailLabel("Unread: {{ count }}", 12, false); got != "Unread: 12" {
		t.Errorf("format = %q", got)
	}
}

func TestLoadFileAppliesMailAccounts(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	good := "[[modules.mail.accounts]]\nname = \"Work\"\nquery = \"folder:work and tag:unread\"\nprovider = \"gmail\"\n\n[modules.mail]\nformat = \"{{ count }} new\"\nhide-when-zero = false\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(c.Mail.Accounts) != 1 {
		t.Fatalf("accounts = %+v", c.Mail.Accounts)
	}
	account := c.Mail.Accounts[0]
	if account.Name != "Work" || account.Query != "folder:work and tag:unread" || account.Provider != "gmail" {
		t.Errorf("account = %+v", account)
	}
	if c.Mail.Format != "{{ count }} new" || c.Mail.HideWhenZero {
		t.Errorf("mail = %+v", c.Mail)
	}

	for _, bad := range []string{
		"[[modules.mail.accounts]]\nquery = \"tag:unread\"\n",
		"[[modules.mail.accounts]]\nname = \"Work\"\n",
	} {
		if err := osWrite(path, bad); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error", bad)
		}
	}
}
