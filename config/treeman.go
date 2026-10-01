package config

// TreemanConfig is ported from crates/wayle-config/src/schemas/modules/treeman/mod.rs.
//
// treeman worktree health across all registered repos, in a dropdown.
type TreemanConfig struct {
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ total }}` - Total active worktrees
	// - `{{ stable }}` - Worktrees in the ready bucket
	// - `{{ up }}` - Worktrees preparing
	// - `{{ down }}` - Worktrees tearing down
	// - `{{ failed }}` - Worktrees whose last finalize errored
	//
	// ## Examples
	//
	// - `"{{ total }}"` - "7"
	// - `"{{ total }} ({{ failed }}!)"` - "7 (1!)"
	Format string `cfg:"format"`
	// Module icon (shown when every worktree is resting-ready).
	IconName string `cfg:"icon-name"`
	// Icon shown while any worktree is being prepared.
	IconPreparing string `cfg:"icon-preparing"`
	// Icon shown while any worktree is being torn down.
	IconTearingDown string `cfg:"icon-tearing-down"`
	// Icon shown when any worktree's last finalize errored.
	IconFailed string `cfg:"icon-failed"`
	// Collapse the module entirely when there are no active worktrees.
	HideIfEmpty bool `cfg:"hide-if-empty"`
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
	// Display count label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click.
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

// DefaultsTreeman returns the schema defaults.
func DefaultsTreeman() TreemanConfig {
	return TreemanConfig{
		Format:          "{{ total }}",
		IconName:        "ld-layers-symbolic",
		IconPreparing:   "tb-loader-2-symbolic",
		IconTearingDown: "ld-trash-2-symbolic",
		IconFailed:      "tb-alert-triangle-symbolic",
		HideIfEmpty:     false,
		BorderShow:      false,
		BorderColor:     mustColor("border-accent"),
		IconShow:        true,
		IconColor:       mustColor("auto"),
		IconBgColor:     mustColor("accent"),
		LabelShow:       true,
		LabelColor:      mustColor("accent"),
		LabelMaxLength:  0,
		ButtonBgColor:   mustColor("bg-surface-elevated"),
		LeftClick:       ParseClickAction("dropdown:treeman"),
		RightClick:      ClickAction{},
		MiddleClick:     ClickAction{},
		ScrollUp:        ClickAction{},
		ScrollDown:      ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c TreemanConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
