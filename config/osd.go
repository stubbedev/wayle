package config

import (
	"errors"
	"slices"

	"github.com/BurntSushi/toml"
)

// OSD screen positions (the OsdPosition schema set).
const (
	OsdTopLeft     = "top-left"
	OsdTop         = "top"
	OsdTopRight    = "top-right"
	OsdRight       = "right"
	OsdBottomRight = "bottom-right"
	OsdBottom      = "bottom"
	OsdBottomLeft  = "bottom-left"
	OsdLeft        = "left"
)

// OsdPositions is the schema's position set.
var OsdPositions = []string{OsdTopLeft, OsdTop, OsdTopRight, OsdRight, OsdBottomRight, OsdBottom, OsdBottomLeft, OsdLeft}

// OsdConfig is the on-screen display configuration.
type OsdConfig struct {
	Enabled    bool
	DurationMS int
	Position   string
	Margin     float64
	Monitor    string
	TextAlign  string
}

// DefaultsOsd returns the schema defaults.
func DefaultsOsd() OsdConfig {
	return OsdConfig{
		Enabled:    true,
		DurationMS: 2500,
		Position:   OsdBottom,
		Margin:     1.0,
		Monitor:    "primary",
		TextAlign:  "center",
	}
}

// ValidOsdPosition reports whether the position is in the schema set.
func ValidOsdPosition(p string) bool {
	return slices.Contains(OsdPositions, p)
}

// applyOsd overlays [osd].
func applyOsd(md toml.MetaData, prim toml.Primitive) (OsdConfig, error) {
	cfg := DefaultsOsd()
	var doc struct {
		Enabled    *bool    `toml:"enabled"`
		DurationMS *int     `toml:"duration"`
		Position   *string  `toml:"position"`
		Margin     *float64 `toml:"margin"`
		Monitor    *string  `toml:"monitor"`
		TextAlign  *string  `toml:"text-align"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Enabled != nil {
		cfg.Enabled = *doc.Enabled
	}
	if doc.DurationMS != nil {
		cfg.DurationMS = *doc.DurationMS
	}
	if doc.Position != nil {
		if !ValidOsdPosition(*doc.Position) {
			return cfg, errors.New("osd: unknown position " + *doc.Position)
		}
		cfg.Position = *doc.Position
	}
	if doc.Margin != nil {
		cfg.Margin = *doc.Margin
	}
	if doc.Monitor != nil {
		cfg.Monitor = *doc.Monitor
	}
	if doc.TextAlign != nil {
		cfg.TextAlign = *doc.TextAlign
	}
	return cfg, nil
}
