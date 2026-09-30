package config

import (
	"fmt"
	"strings"
)

// CssToken names a design token a ColorValue can reference; the values
// are the config strings ("bg-surface", "border-accent", ...). The
// palette-side resolution lives in the styling package.
type CssToken string

// The styling tokens, grouped as in the schema. The numeric mix
// percentages each token resolves through live in styling/tokens.go,
// ported from crates/wayle-styling/scss/tokens.
//
//nolint:gosec // these are CSS token names, not credentials
const (
	TokenBgBase            CssToken = "bg-base"
	TokenBgSurface         CssToken = "bg-surface"
	TokenBgSurfaceElevated CssToken = "bg-surface-elevated"
	TokenBgElevated        CssToken = "bg-elevated"
	TokenBgOverlay         CssToken = "bg-overlay"
	TokenBgHover           CssToken = "bg-hover"
	TokenBgActive          CssToken = "bg-active"
	TokenBgSelected        CssToken = "bg-selected"

	TokenFgDefault  CssToken = "fg-default"
	TokenFgMuted    CssToken = "fg-muted"
	TokenFgSubtle   CssToken = "fg-subtle"
	TokenFgOnAccent CssToken = "fg-on-accent"

	TokenAccent       CssToken = "accent"
	TokenAccentSubtle CssToken = "accent-subtle"
	TokenAccentHover  CssToken = "accent-hover"

	TokenStatusError         CssToken = "status-error"
	TokenStatusWarning       CssToken = "status-warning"
	TokenStatusSuccess       CssToken = "status-success"
	TokenStatusInfo          CssToken = "status-info"
	TokenStatusErrorSubtle   CssToken = "status-error-subtle"
	TokenStatusWarningSubtle CssToken = "status-warning-subtle"
	TokenStatusSuccessSubtle CssToken = "status-success-subtle"
	TokenStatusInfoSubtle    CssToken = "status-info-subtle"
	TokenStatusErrorHover    CssToken = "status-error-hover"

	TokenRed    CssToken = "red"
	TokenYellow CssToken = "yellow"
	TokenGreen  CssToken = "green"
	TokenBlue   CssToken = "blue"

	TokenBorderSubtle  CssToken = "border-subtle"
	TokenBorderDefault CssToken = "border-default"
	TokenBorderStrong  CssToken = "border-strong"
	TokenBorderAccent  CssToken = "border-accent"
	TokenBorderError   CssToken = "border-error"
)

var validTokens = map[CssToken]bool{}

func init() {
	for _, t := range []CssToken{
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
	} {
		validTokens[t] = true
	}
}

// ColorKind distinguishes the ColorValue variants.
type ColorKind uint8

// ColorValue kinds, mirroring the Rust enum: a token reference that
// follows the theme, a fixed hex color, fully transparent, or the
// context-aware Auto that defers to its consumer (resolved as the
// accent).
const (
	ColorToken ColorKind = iota
	ColorCustom
	ColorTransparent
	ColorAuto
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

// ParseColorValue decodes one config color string. Unknown token names
// and malformed hex are errors — a mistyped color fails at load instead
// of silently rendering the wrong thing.
func ParseColorValue(s string) (ColorValue, error) {
	switch s {
	case "transparent":
		return ColorValue{Kind: ColorTransparent}, nil
	case "auto":
		return ColorValue{Kind: ColorAuto}, nil
	}
	if strings.HasPrefix(s, "#") {
		hex, err := ParseHexColor(s)
		if err != nil {
			return ColorValue{}, err
		}
		return ColorValue{Kind: ColorCustom, Hex: hex.String()}, nil
	}
	token := CssToken(s)
	if !validTokens[token] {
		return ColorValue{}, fmt.Errorf("config: unknown color %q (want a token name, #hex, \"transparent\", or \"auto\")", s)
	}
	return ColorValue{Kind: ColorToken, Token: token}, nil
}

// UnmarshalText decodes a TOML color string through ParseColorValue, so
// module tables can decode `icon-color = "red"` straight into a
// ColorValue field and a bad color is a load error.
func (c *ColorValue) UnmarshalText(text []byte) error {
	cv, err := ParseColorValue(string(text))
	if err != nil {
		return err
	}
	*c = cv
	return nil
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
