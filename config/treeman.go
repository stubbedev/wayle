package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// TreemanConfig is the treeman module configuration: the worktree
// health aggregate, with a per-bucket icon.
type TreemanConfig struct {
	Click       ClickConfig
	Format      string
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
		Format          *string     `toml:"format"`
		LabelShow       *bool       `toml:"label-show"`
		HideIfEmpty     *bool       `toml:"hide-if-empty"`
		IconShow        *bool       `toml:"icon-show"`
		IconName        *string     `toml:"icon-name"`
		IconColor       *ColorValue `toml:"icon-color"`
		IconFailed      *string     `toml:"icon-failed"`
		IconPreparing   *string     `toml:"icon-preparing"`
		IconTearingDown *string     `toml:"icon-tearing-down"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if doc.HideIfEmpty != nil {
		cfg.HideIfEmpty = *doc.HideIfEmpty
	}
	if doc.IconShow != nil {
		for name, icon := range cfg.Icons {
			icon.Show = *doc.IconShow
			cfg.Icons[name] = icon
		}
		cfg.Icon.Show = *doc.IconShow
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
		stable := cfg.Icons[TreemanBucketStable]
		stable.Name = *doc.IconName
		cfg.Icons[TreemanBucketStable] = stable
	}
	if doc.IconColor != nil {
		cfg.Icon.Color = *doc.IconColor
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
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
