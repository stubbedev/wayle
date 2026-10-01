package config

// GeneralConfig is the [general] section
// (crates/wayle-config/src/schemas/general/mod.rs).
//
// Shell-wide settings that don't belong to any specific module.
type GeneralConfig struct {
	// Sans-serif font family for UI text and labels.
	FontSans string `cfg:"font-sans"`
	// Monospace font family for code and technical content.
	FontMono string `cfg:"font-mono"`
	// Demote overlay surfaces to allow compositor screen tearing.
	//
	// When enabled, surfaces that would normally use the `overlay` layer
	// are demoted to `top`, allowing fullscreen games to use direct scanout.
	TearingMode bool `cfg:"tearing-mode"`
}

// DefaultsGeneral returns the schema defaults.
func DefaultsGeneral() GeneralConfig {
	return GeneralConfig{
		FontSans: "Inter",
		FontMono: "JetBrains Mono",
	}
}

// EffectiveLayer applies tearing mode to a surface's configured layer:
// overlay is demoted to top so fullscreen clients keep direct scanout.
func (g GeneralConfig) EffectiveLayer(l Layer) Layer {
	if g.TearingMode && l == LayerOverlay {
		return LayerTop
	}
	return l
}
