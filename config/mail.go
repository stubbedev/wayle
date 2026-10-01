package config

// MailProvider is ported from crates/wayle-config/src/schemas/modules/mail/account.rs.
//
// Mail provider, used to pick a default brand icon for an account.
type MailProvider string

// MailProvider values.
const (
	// Generic mailbox; uses the plain mail glyph.
	MailProviderGeneric MailProvider = "generic"
	// Gmail.
	MailProviderGmail MailProvider = "gmail"
	// Microsoft Outlook.
	MailProviderOutlook MailProvider = "outlook"
	// Apple iCloud Mail.
	MailProviderIcloud MailProvider = "icloud"
	// Proton Mail.
	MailProviderProton MailProvider = "proton"
	// Fastmail.
	MailProviderFastmail MailProvider = "fastmail"
	// Yahoo Mail.
	MailProviderYahoo MailProvider = "yahoo"
)

var _ = registerEnum(MailProviderGeneric, MailProviderGmail, MailProviderOutlook, MailProviderIcloud, MailProviderProton, MailProviderFastmail, MailProviderYahoo)

// MailAccount is ported from crates/wayle-config/src/schemas/modules/mail/account.rs.
//
// One mail account in the `[modules.mail]` per-account breakdown.
//
// Each account has its own notmuch query; the dropdown shows the per-account
// unread counts and the bar shows their sum.
//
// ## Example
//
// ```toml
// [[modules.mail.accounts]]
// name = "Work"
// query = "folder:work/INBOX and tag:unread"
// provider = "gmail"
// ```
type MailAccount struct {
	// Display name shown in the dropdown.
	Name string `cfg:"name,required"`
	// notmuch query whose match count is this account's unread total.
	Query string `cfg:"query,required"`
	// Provider, selecting the default brand icon.
	Provider MailProvider `cfg:"provider,default"`
	// Optional icon override. Empty uses the provider's default icon.
	Icon *string `cfg:"icon,default"`
}

// DefaultsMailAccount returns the schema defaults.
func DefaultsMailAccount() MailAccount {
	return MailAccount{
		Provider: MailProviderGeneric,
	}
}

// MailConfig is ported from crates/wayle-config/src/schemas/modules/mail/mod.rs.
//
// Unread mail count, backed by a notmuch query.
//
// Runs `notmuch count <query>` and re-queries whenever the maildir changes
// (event-driven via an inotify watch on the notmuch database path). Hidden
// while the count is zero when `hide-when-zero` is set.
type MailConfig struct {
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ count }}` - Number of messages matching the query
	//
	// ## Examples
	//
	// - `"{{ count }}"` - "3"
	Format string `cfg:"format"`
	// notmuch search query whose match count is shown.
	//
	// Any query `notmuch count` accepts, e.g. `tag:unread`,
	// `tag:unread and tag:inbox`, `folder:work and tag:unread`.
	//
	// Ignored when `accounts` is non-empty — the bar count is then the sum of
	// the per-account queries.
	Query string `cfg:"query"`
	// Per-account unread breakdown shown in the mail dropdown. Each account
	// has its own notmuch query and a provider (for its icon). When set, the
	// bar count/label is the sum across accounts and `query` is ignored.
	Accounts []MailAccount `cfg:"accounts"`
	// Hide the module entirely while the count is zero.
	HideWhenZero bool `cfg:"hide-when-zero"`
	// Fire a desktop notification when the unread count
	// rises — i.e. new mail arrives. One notification per newly-arrived
	// message (capped per burst), showing its sender and subject. With
	// `accounts` configured, each notification uses that account's provider
	// icon; otherwise the module icon is used.
	Notify bool `cfg:"notify"`
	// Notification summary when new mail arrives.
	//
	// ## Placeholders
	//
	// - `{{ sender }}` - Message sender (name or address)
	// - `{{ subject }}` - Message subject
	// - `{{ count }}` - Total messages matching the query
	// - `{{ new }}` - How many arrived since the last count
	NotifySummary string `cfg:"notify-summary"`
	// Notification body when new mail arrives. Same placeholders as
	// `notify-summary`.
	NotifyBody string `cfg:"notify-body"`
	// Module icon.
	IconName string `cfg:"icon-name"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color. Auto selects based on variant for contrast.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Display label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click. Defaults to opening the per-account dropdown; set
	// to empty for no action, or a shell command (e.g. your mail client).
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
}

// DefaultsMail returns the schema defaults.
func DefaultsMail() MailConfig {
	return MailConfig{
		Format:         "{{ count }}",
		Query:          "tag:unread",
		Accounts:       []MailAccount{},
		HideWhenZero:   true,
		Notify:         false,
		NotifySummary:  "{{ sender }}",
		NotifyBody:     "{{ subject }}",
		IconName:       "ld-mail-symbolic",
		BorderShow:     false,
		BorderColor:    mustColor("blue"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("bg-surface-elevated"),
		LabelShow:      true,
		LabelColor:     mustColor("auto"),
		LabelMaxLength: 0,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ParseClickAction("dropdown:mail"),
		RightClick:     ClickAction{},
		MiddleClick:    ClickAction{},
		ScrollUp:       ClickAction{},
		ScrollDown:     ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c MailConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

func (a *MailAccount) setDefaults() { *a = DefaultsMailAccount() }

func (MailAccount) noStructDefault() {}

// DefaultIcon is the provider's brand icon (MailProvider::default_icon):
// its Simple Icons glyph, or the generic mail icon.
func (p MailProvider) DefaultIcon() string {
	if slug, ok := p.SimpleIconsSlug(); ok {
		return "si-" + slug + "-symbolic"
	}
	return "ld-mail-symbolic"
}

// SimpleIconsSlug is the Simple Icons slug that installs the provider's
// brand icon; false for the generic provider (simple_icons_slug).
func (p MailProvider) SimpleIconsSlug() (string, bool) {
	switch p {
	case MailProviderGmail:
		return "gmail", true
	case MailProviderOutlook:
		return "microsoftoutlook", true
	case MailProviderIcloud:
		return "icloud", true
	case MailProviderProton:
		return "protonmail", true
	case MailProviderFastmail:
		return "fastmail", true
	case MailProviderYahoo:
		return "yahoo", true
	}
	return "", false
}

// ResolvedIcon is the account's icon: its own when set and non-empty,
// else the provider's (MailAccount::resolved_icon).
func (a MailAccount) ResolvedIcon() string {
	if a.Icon != nil && *a.Icon != "" {
		return *a.Icon
	}
	return a.Provider.DefaultIcon()
}
