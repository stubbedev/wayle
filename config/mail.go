package config

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// MailProvider is an account's mail host; it picks the account's
// default icon (schemas/modules/mail/account.rs MailProvider).
type MailProvider string

// Mail providers.
const (
	MailProviderGeneric  MailProvider = "generic"
	MailProviderGmail    MailProvider = "gmail"
	MailProviderOutlook  MailProvider = "outlook"
	MailProviderIcloud   MailProvider = "icloud"
	MailProviderProton   MailProvider = "proton"
	MailProviderFastmail MailProvider = "fastmail"
	MailProviderYahoo    MailProvider = "yahoo"
)

var mailProviderIcons = map[MailProvider]string{
	MailProviderGeneric:  "ld-mail-symbolic",
	MailProviderGmail:    "si-gmail-symbolic",
	MailProviderOutlook:  "si-microsoftoutlook-symbolic",
	MailProviderIcloud:   "si-icloud-symbolic",
	MailProviderProton:   "si-protonmail-symbolic",
	MailProviderFastmail: "si-fastmail-symbolic",
	MailProviderYahoo:    "si-yahoo-symbolic",
}

// DefaultIcon is the provider's brand icon (default_icon).
func (p MailProvider) DefaultIcon() string { return mailProviderIcons[p] }

// MailAccountConfig is one [[modules.mail.accounts]] entry: a notmuch
// query whose match count is the account's unread count.
type MailAccountConfig struct {
	Name     string
	Query    string
	Provider MailProvider
	Icon     string
}

// ResolvedIcon is the account's icon: its own when set, else the
// provider's (MailAccount::resolved_icon).
func (a MailAccountConfig) ResolvedIcon() string {
	if a.Icon != "" {
		return a.Icon
	}
	return a.Provider.DefaultIcon()
}

// MailConfig is the mail module configuration.
type MailConfig struct {
	Click    ClickConfig
	Accounts []MailAccountConfig
	Query    string
	Format   string
	// Button is the bar-button key set; LabelShow mirrors its
	// label-show.
	Button       ButtonConfig
	LabelShow    bool
	HideWhenZero bool
	// Notify fires a desktop notification per newly-arrived message.
	Notify bool
	// NotifySummary and NotifyBody template that notification with
	// {{ sender }}, {{ subject }}, {{ count }} and {{ new }}.
	NotifySummary string
	NotifyBody    string
	// IconName is the module icon, and the notification icon when no
	// accounts are configured.
	IconName string
}

// DefaultsMail returns the schema defaults.
func DefaultsMail() MailConfig {
	return MailConfig{
		Query:         "tag:unread",
		Format:        "{{ count }}",
		LabelShow:     true,
		HideWhenZero:  true,
		NotifySummary: "{{ sender }}",
		NotifyBody:    "{{ subject }}",
		IconName:      "ld-mail-symbolic",
		Button:        DefaultsButton(buttonColors("auto", "auto", "bg-surface-elevated", "bg-surface-elevated", "blue"), TokenBlue, true, 0),
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
		Accounts      *[]mailDoc `toml:"accounts"`
		Query         *string    `toml:"query"`
		Format        *string    `toml:"format"`
		HideWhenZero  *bool      `toml:"hide-when-zero"`
		Notify        *bool      `toml:"notify"`
		NotifySummary *string    `toml:"notify-summary"`
		NotifyBody    *string    `toml:"notify-body"`
		IconName      *string    `toml:"icon-name"`
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
	if doc.HideWhenZero != nil {
		cfg.HideWhenZero = *doc.HideWhenZero
	}
	if doc.Notify != nil {
		cfg.Notify = *doc.Notify
	}
	if doc.NotifySummary != nil {
		cfg.NotifySummary = *doc.NotifySummary
	}
	if doc.NotifyBody != nil {
		cfg.NotifyBody = *doc.NotifyBody
	}
	if doc.IconName != nil {
		cfg.IconName = *doc.IconName
	}
	if doc.Accounts != nil {
		cfg.Accounts = make([]MailAccountConfig, 0, len(*doc.Accounts))
		for _, account := range *doc.Accounts {
			out, err := mailAccount(account)
			if err != nil {
				return cfg, err
			}
			cfg.Accounts = append(cfg.Accounts, out)
		}
	}
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}

// mailAccount validates one accounts entry: name and query are
// required, and the provider must be one the schema knows.
func mailAccount(doc mailDoc) (MailAccountConfig, error) {
	out := MailAccountConfig{Provider: MailProviderGeneric}
	if doc.Name != nil {
		out.Name = *doc.Name
	}
	if doc.Query != nil {
		out.Query = *doc.Query
	}
	if doc.Icon != nil {
		out.Icon = *doc.Icon
	}
	if out.Name == "" {
		return out, errors.New("mail: account name is required")
	}
	if out.Query == "" {
		return out, errors.New("mail: account " + out.Name + " query is required")
	}
	if doc.Provider != nil {
		provider := MailProvider(*doc.Provider)
		if _, ok := mailProviderIcons[provider]; !ok {
			return out, fmt.Errorf("mail: account %s: invalid provider %q (want generic|gmail|outlook|icloud|proton|fastmail|yahoo)", out.Name, *doc.Provider)
		}
		out.Provider = provider
	}
	return out, nil
}
