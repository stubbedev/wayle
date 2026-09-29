package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// NetstatConfig is the netstat module configuration.
type NetstatConfig struct {
	Click     ClickConfig
	Format    string
	Interface string
	PollMs    int
	LabelShow bool
}

// DefaultsNetstat returns the schema defaults.
func DefaultsNetstat() NetstatConfig {
	return NetstatConfig{
		Format:    "{{ down_auto }} {{ up_auto }}",
		Interface: "auto",
		PollMs:    2000,
		LabelShow: true,
	}
}

// applyNetstat overlays [modules.netstat].
func applyNetstat(md toml.MetaData, prim toml.Primitive) (NetstatConfig, error) {
	cfg := DefaultsNetstat()
	var doc struct {
		Format    *string `toml:"format"`
		Interface *string `toml:"interface"`
		PollMs    *int    `toml:"poll-interval-ms"`
		LabelShow *bool   `toml:"label-show"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.Interface != nil {
		cfg.Interface = *doc.Interface
	}
	if doc.PollMs != nil {
		cfg.PollMs = *doc.PollMs
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if cfg.PollMs < 0 {
		return cfg, errors.New("netstat: poll-interval-ms is negative")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
