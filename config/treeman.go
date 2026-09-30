package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// TreemanConfig is the treeman module configuration: the worktree
// health aggregate, with a per-bucket icon.
type TreemanConfig struct {
	Click  ClickConfig
	Format string
	// Button is the bar-button key set; LabelShow, Icon.Show/Color, and
	// the Show of every Icons entry mirror its label-show, icon-show,
	// and icon-color.
	Button      ButtonConfig
	LabelShow   bool
	HideIfEmpty bool
	Icon        IconConfig
	Icons       map[string]IconConfig
	Colors      map[string]ColorValue
}

// Schema icon defaults (TreemanConfig's per-state icon keys).
const (
	TreemanBucketStable       = "stable"
	TreemanBucketUp           = "up"
	TreemanBucketDown         = "down"
	TreemanBucketFailed       = "failed"
	defaultTreemanFailedIcon  = "tb-alert-triangle-symbolic"
	defaultTreemanPreparing   = "tb-loader-2-symbolic"
	defaultTreemanTearingDown = "ld-trash-2-symbolic"
)

// DefaultsTreeman returns the schema defaults.
func DefaultsTreeman() TreemanConfig {
	return TreemanConfig{
		Format:      "{{ total }}",
		LabelShow:   true,
		Button:      DefaultsButton(buttonColors("auto", "accent", "accent", "bg-surface-elevated", "border-accent"), TokenAccent, true, 0),
		HideIfEmpty: false,
		Icon:        DefaultsIcon(true, "ld-layers-symbolic"),
		Icons: map[string]IconConfig{
			TreemanBucketFailed: DefaultsIcon(true, defaultTreemanFailedIcon),
			TreemanBucketUp:     DefaultsIcon(true, defaultTreemanPreparing),
			TreemanBucketDown:   DefaultsIcon(true, defaultTreemanTearingDown),
			TreemanBucketStable: DefaultsIcon(true, "ld-layers-symbolic"),
		},
		Colors: map[string]ColorValue{},
	}
}

// applyTreeman overlays [modules.treeman].
func applyTreeman(md toml.MetaData, prim toml.Primitive) (TreemanConfig, error) {
	cfg := DefaultsTreeman()
	var doc struct {
		Format          *string `toml:"format"`
		HideIfEmpty     *bool   `toml:"hide-if-empty"`
		IconName        *string `toml:"icon-name"`
		IconFailed      *string `toml:"icon-failed"`
		IconPreparing   *string `toml:"icon-preparing"`
		IconTearingDown *string `toml:"icon-tearing-down"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.HideIfEmpty != nil {
		cfg.HideIfEmpty = *doc.HideIfEmpty
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
		stable := cfg.Icons[TreemanBucketStable]
		stable.Name = *doc.IconName
		cfg.Icons[TreemanBucketStable] = stable
	}
	for name, key := range map[string]*string{
		TreemanBucketFailed: doc.IconFailed,
		TreemanBucketUp:     doc.IconPreparing,
		TreemanBucketDown:   doc.IconTearingDown,
	} {
		if key == nil {
			continue
		}
		icon := cfg.Icons[name]
		icon.Name = *key
		cfg.Icons[name] = icon
	}
	if doc.Format != nil && *doc.Format == "" {
		return cfg, errors.New("treeman: format is empty")
	}
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	button.mirrorIcon(&cfg.Icon)
	button.mirrorIconShow(cfg.Icons)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
