package config

import "fmt"

// CssToken names a design token a ColorValue can reference; the values
// are the config strings ("bg-surface", "border-accent", ...). The
// palette-side resolution lives in the styling package; the numeric mix
// percentages each token resolves through live in styling/tokens.go,
// ported from crates/wayle-styling/scss/tokens
// (crates/wayle-config/src/schemas/styling/types/color.rs CssToken).
type CssToken string

// The styling tokens, grouped as in the schema.
//
//nolint:gosec // these are CSS token names, not credentials
const (
	// `--bg-base` - Application background.
	TokenBgBase CssToken = "bg-base"
	// `--bg-surface` - Elevated surfaces.
	TokenBgSurface CssToken = "bg-surface"
	// `--bg-surface-elevated` - Subtle elevation from surface (buttons on surface).
	TokenBgSurfaceElevated CssToken = "bg-surface-elevated"
	// `--bg-elevated` - Higher elevation surfaces.
	TokenBgElevated CssToken = "bg-elevated"
	// `--bg-overlay` - Popovers, dialogs.
	TokenBgOverlay CssToken = "bg-overlay"
	// `--bg-hover` - Hover state background.
	TokenBgHover CssToken = "bg-hover"
	// `--bg-active` - Active/pressed state background.
	TokenBgActive CssToken = "bg-active"
	// `--bg-selected` - Selected item background.
	TokenBgSelected CssToken = "bg-selected"
	// `--fg-default` - Primary text color.
	TokenFgDefault CssToken = "fg-default"
	// `--fg-muted` - Secondary text color.
	TokenFgMuted CssToken = "fg-muted"
	// `--fg-subtle` - Tertiary/hint text color.
	TokenFgSubtle CssToken = "fg-subtle"
	// `--fg-on-accent` - Text color on accent backgrounds.
	TokenFgOnAccent CssToken = "fg-on-accent"
	// `--accent` - Primary accent color.
	TokenAccent CssToken = "accent"
	// `--accent-subtle` - Subtle accent background.
	TokenAccentSubtle CssToken = "accent-subtle"
	// `--accent-hover` - Accent hover state.
	TokenAccentHover CssToken = "accent-hover"
	// `--status-error` - Error state color.
	TokenStatusError CssToken = "status-error"
	// `--status-warning` - Warning state color.
	TokenStatusWarning CssToken = "status-warning"
	// `--status-success` - Success state color.
	TokenStatusSuccess CssToken = "status-success"
	// `--status-info` - Info state color.
	TokenStatusInfo CssToken = "status-info"
	// `--status-error-subtle` - Subtle error background.
	TokenStatusErrorSubtle CssToken = "status-error-subtle"
	// `--status-warning-subtle` - Subtle warning background.
	TokenStatusWarningSubtle CssToken = "status-warning-subtle"
	// `--status-success-subtle` - Subtle success background.
	TokenStatusSuccessSubtle CssToken = "status-success-subtle"
	// `--status-info-subtle` - Subtle info background.
	TokenStatusInfoSubtle CssToken = "status-info-subtle"
	// `--status-error-hover` - Error hover state.
	TokenStatusErrorHover CssToken = "status-error-hover"
	// `--red` - Red color for stylistic/decorative use.
	TokenRed CssToken = "red"
	// `--yellow` - Yellow color for stylistic/decorative use.
	TokenYellow CssToken = "yellow"
	// `--green` - Green color for stylistic/decorative use.
	TokenGreen CssToken = "green"
	// `--blue` - Blue color for stylistic/decorative use.
	TokenBlue CssToken = "blue"
	// `--border-subtle` - Subtle border color.
	TokenBorderSubtle CssToken = "border-subtle"
	// `--border-default` - Default border color.
	TokenBorderDefault CssToken = "border-default"
	// `--border-strong` - Strong border color.
	TokenBorderStrong CssToken = "border-strong"
	// `--border-accent` - Accent-colored border.
	TokenBorderAccent CssToken = "border-accent"
	// `--border-error` - Error state border.
	TokenBorderError CssToken = "border-error"
)

var _ = registerEnum(
	TokenBgBase, TokenBgSurface, TokenBgSurfaceElevated, TokenBgElevated,
	TokenBgOverlay, TokenBgHover, TokenBgActive, TokenBgSelected,
	TokenFgDefault, TokenFgMuted, TokenFgSubtle, TokenFgOnAccent,
	TokenAccent, TokenAccentSubtle, TokenAccentHover,
	TokenStatusError, TokenStatusWarning, TokenStatusSuccess, TokenStatusInfo,
	TokenStatusErrorSubtle, TokenStatusWarningSubtle, TokenStatusSuccessSubtle,
	TokenStatusInfoSubtle, TokenStatusErrorHover,
	TokenRed, TokenYellow, TokenGreen, TokenBlue,
	TokenBorderSubtle, TokenBorderDefault, TokenBorderStrong,
	TokenBorderAccent, TokenBorderError,
)

// ColorKind distinguishes the ColorValue variants.
type ColorKind uint8

// ColorValue kinds, mirroring the Rust enum: the context-aware Auto
// that defers to its consumer (resolved as the accent), a token
// reference that follows the theme, a fixed hex color, or fully
// transparent. Auto is the zero kind, so a zero ColorValue defers
// rather than naming an empty token.
const (
	ColorAuto ColorKind = iota
	ColorToken
	ColorCustom
	ColorTransparent
)

// ColorValue is the config color union: "bg-surface" | "#414868" |
// "transparent" | "auto".
type ColorValue struct {
	Kind  ColorKind
	Token CssToken
	Hex   string // #rgb, #rgba, #rrggbb, or #rrggbbaa; valid only for ColorCustom
}

// IsAuto reports whether the value defers to its consumer.
func (c ColorValue) IsAuto() bool { return c.Kind == ColorAuto }

// String is the config form.
func (c ColorValue) String() string {
	switch c.Kind {
	case ColorCustom:
		return c.Hex
	case ColorTransparent:
		return "transparent"
	case ColorAuto:
		return "auto"
	}
	return string(c.Token)
}

// ParseColorValue decodes one config color string. Unknown token names
// and malformed hex are errors, with the Rust messages
// (InvalidCssToken, InvalidHexColor).
func ParseColorValue(s string) (ColorValue, error) {
	switch s {
	case "transparent":
		return ColorValue{Kind: ColorTransparent}, nil
	case "auto":
		return ColorValue{Kind: ColorAuto}, nil
	}
	if len(s) > 0 && s[0] == '#' {
		hex, err := ParseHexColor(s)
		if err != nil {
			return ColorValue{}, err
		}
		return ColorValue{Kind: ColorCustom, Hex: hex.String()}, nil
	}
	if _, err := decodeEnum(enumVariants(cssTokenType), s); err != nil {
		return ColorValue{}, fmt.Errorf("unknown CSS token: '%s' (see documentation for valid values)", s)
	}
	return ColorValue{Kind: ColorToken, Token: CssToken(s)}, nil
}

// UnmarshalConfig implements Unmarshaler.
func (c *ColorValue) UnmarshalConfig(v any) error {
	s, ok := v.(string)
	if !ok {
		return invalidType(v, "a string")
	}
	parsed, err := ParseColorValue(s)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

// MarshalConfig implements Marshaler.
func (c ColorValue) MarshalConfig() any { return c.String() }

var cssTokenType = typeOf[CssToken]()

func (ColorValue) configSchema(*schemaGen) Schema {
	variants := enumVariants(cssTokenType)
	tokens := make([]any, len(variants))
	for i, v := range variants {
		entry := Schema{"const": v, "type": "string"}
		if doc := schemaDocs["CssToken="+v]; doc != "" {
			entry["description"] = doc
		}
		tokens[i] = entry
	}
	return Schema{
		"description": "CSS token, hex color (#rgb, #rgba, #rrggbb, or #rrggbbaa), 'transparent', or 'auto'",
		"anyOf": []any{
			Schema{"oneOf": tokens},
			Schema{"enum": []any{"transparent", "auto"}},
			Schema{"type": "string", "pattern": "^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$"},
		},
	}
}

// mustColor panics only on a typo in literal defaults; user config
// never flows through it.
func mustColor(s string) ColorValue {
	cv, err := ParseColorValue(s)
	if err != nil {
		panic(err)
	}
	return cv
}

// CSSVar is the token's CSS variable reference, "var(--accent)": the
// Rust CssToken::css_var.
func (t CssToken) CSSVar() string { return "var(--" + string(t) + ")" }

// ToCSS is the value for an inline declaration: a token is its var(),
// a custom color its hex, transparent the keyword, and Auto the accent
// var — consumers resolve Auto before calling it. This is the Rust
// ColorValue::to_css.
func (c ColorValue) ToCSS() string {
	switch c.Kind {
	case ColorToken:
		return c.Token.CSSVar()
	case ColorCustom:
		return c.Hex
	case ColorTransparent:
		return "transparent"
	default: // ColorAuto
		return TokenAccent.CSSVar()
	}
}
