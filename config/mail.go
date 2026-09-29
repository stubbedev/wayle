package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// MailAccountConfig is one [[modules.mail.accounts]] entry: a notmuch
// query whose match count is the account's unread count.
type MailAccountConfig struct {
	Name     string
	Query    string
	Provider string
	Icon     string
}

// MailConfig is the mail module configuration.
type MailConfig struct {
	Click        ClickConfig
	Accounts     []MailAccountConfig
	Query        string
	Format       string
	LabelShow    bool
	HideWhenZero bool
}

// DefaultsMail returns the schema defaults.
func DefaultsMail() MailConfig {
	return MailConfig{
		Format:       "{{ count }}",
		LabelShow:    true,
		HideWhenZero: true,
	}
}

type mailDoc struct {
	Name     *string `toml:"name"`
	Query    *string `toml:"query"`
	Provider *string `toml:"provider"`
	Icon     *string `toml:"icon"`
}

// applyMail overlays [modules.mail], including the accounts array.
func applyMail(md toml.MetaData, prim toml.Primitive) (MailConfig, error) {
	cfg := DefaultsMail()
	var doc struct {
		Accounts     *[]mailDoc `toml:"accounts"`
		Query        *string    `toml:"query"`
		Format       *string    `toml:"format"`
		LabelShow    *bool      `toml:"label-show"`
		HideWhenZero *bool      `toml:"hide-when-zero"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Query != nil {
		cfg.Query = *doc.Query
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if doc.HideWhenZero != nil {
		cfg.HideWhenZero = *doc.HideWhenZero
	}
	if doc.Accounts != nil {
		cfg.Accounts = make([]MailAccountConfig, 0, len(*doc.Accounts))
		for _, account := range *doc.Accounts {
			out := MailAccountConfig{Provider: "generic"}
			if account.Name != nil {
				out.Name = *account.Name
			}
			if account.Query != nil {
				out.Query = *account.Query
			}
			if account.Provider != nil {
				out.Provider = *account.Provider
			}
			if account.Icon != nil {
				out.Icon = *account.Icon
			}
			if out.Name == "" {
				return cfg, errors.New("mail: account name is required")
			}
			if out.Query == "" {
				return cfg, errors.New("mail: account " + out.Name + " query is required")
			}
			cfg.Accounts = append(cfg.Accounts, out)
		}
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
