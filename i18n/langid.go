package i18n

import (
	"errors"
	"slices"
	"strings"
)

// LangID is a Unicode language identifier as unic-langid 0.9 parses
// it: a lowercase language ("" is und), an optional titlecase script,
// an optional uppercase region, and sorted, deduplicated variants.
type LangID struct {
	language string
	script   string
	region   string
	variants []string
}

var errLangID = errors.New("i18n: invalid language identifier")

// ParseLangID ports unic-langid's parse_language_identifier: subtags
// split on '-' or '_', case-normalized; anything past the variants
// (an extension, an empty subtag) is an error.
func ParseLangID(s string) (LangID, error) {
	tags := splitSubtags(s)
	var id LangID
	lang, ok := parseLanguage(tags[0])
	if !ok {
		return LangID{}, errLangID
	}
	id.language = lang
	position := 1
	for _, tag := range tags[1:] {
		script, isScript := parseScript(tag)
		region, isRegion := parseRegion(tag)
		variant, isVariant := parseVariant(tag)
		switch {
		case position == 1 && isScript:
			id.script, position = script, 2
		case position <= 2 && isRegion:
			id.region, position = region, 3
		case isVariant:
			id.variants, position = append(id.variants, variant), 3
		default:
			return LangID{}, errLangID
		}
	}
	if len(id.variants) > 0 {
		slices.Sort(id.variants)
		id.variants = slices.Compact(id.variants)
	}
	return id, nil
}

// MustLangID parses a compile-time identifier.
func MustLangID(s string) LangID {
	id, err := ParseLangID(s)
	if err != nil {
		panic(err)
	}
	return id
}

// splitSubtags splits on '-' and '_', keeping empty subtags the way
// Rust's slice::split does.
func splitSubtags(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "_", "-"), "-")
}

func allASCII(s string, pred func(byte) bool) bool {
	for i := range len(s) {
		if !pred(s[i]) {
			return false
		}
	}
	return true
}

func isAlnum(b byte) bool { return isAlpha(b) || isDigit(b) }

func parseLanguage(s string) (string, bool) {
	if len(s) < 2 || len(s) > 8 || len(s) == 4 || !allASCII(s, isAlpha) {
		return "", false
	}
	lower := strings.ToLower(s)
	if lower == "und" {
		return "", true
	}
	return lower, true
}

func parseScript(s string) (string, bool) {
	if len(s) != 4 || !allASCII(s, isAlpha) {
		return "", false
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:]), true
}

func parseRegion(s string) (string, bool) {
	switch {
	case len(s) == 2 && allASCII(s, isAlpha):
		return strings.ToUpper(s), true
	case len(s) == 3 && allASCII(s, isDigit):
		return s, true
	}
	return "", false
}

// parseVariant keeps unic-langid's exact 4-byte test: it rejects only a
// subtag that neither starts with a digit nor is alphanumeric after
// the first byte.
func parseVariant(s string) (string, bool) {
	if len(s) < 4 || len(s) > 8 || !allASCII(s, func(b byte) bool { return b < 0x80 }) {
		return "", false
	}
	if len(s) >= 5 && !allASCII(s, isAlnum) {
		return "", false
	}
	if len(s) == 4 && !isDigit(s[0]) && !allASCII(s[1:], isAlnum) {
		return "", false
	}
	return strings.ToLower(s), true
}

// String renders the identifier canonically ("und" for no language).
func (id LangID) String() string {
	parts := []string{id.Language()}
	if id.script != "" {
		parts = append(parts, id.script)
	}
	if id.region != "" {
		parts = append(parts, id.region)
	}
	parts = append(parts, id.variants...)
	return strings.Join(parts, "-")
}

// Language is the language subtag, "und" when unset.
func (id LangID) Language() string {
	if id.language == "" {
		return "und"
	}
	return id.language
}

// Equal reports whether both identifiers carry the same subtags.
func (id LangID) Equal(other LangID) bool {
	return id.language == other.language && id.script == other.script &&
		id.region == other.region && slices.Equal(id.variants, other.variants)
}

// matches ports LanguageIdentifier::matches: each subtag matches when
// equal, or when the side treated as a range leaves it unset.
func (id LangID) matches(other LangID, selfRange, otherRange bool) bool {
	sub := func(a, b string) bool {
		return (selfRange && a == "") || (otherRange && b == "") || a == b
	}
	variants := (selfRange && len(id.variants) == 0) ||
		(otherRange && len(other.variants) == 0) ||
		slices.Equal(id.variants, other.variants)
	return sub(id.language, other.language) && sub(id.script, other.script) &&
		sub(id.region, other.region) && variants
}

// regionMatchingKeys is fluent-langneg's mock likely-subtags table of
// languages whose region is their own code.
var regionMatchingKeys = []string{
	"az", "bg", "cs", "de", "es", "fi", "fr", "hu", "it", "lt", "lv", "nl", "pl", "ro", "ru",
}

// maximize is fluent-langneg 0.13's MockLikelySubtags::maximize (the
// crate is built without its cldr feature).
func (id *LangID) maximize() bool {
	extended := map[string]string{
		"en":    "en-Latn-US",
		"fr":    "fr-Latn-FR",
		"sr":    "sr-Cyrl-SR",
		"sr-RU": "sr-Latn-SR",
		"az-IR": "az-Arab-IR",
		"zh-GB": "zh-Hant-GB",
		"zh-US": "zh-Hant-US",
	}[id.String()]
	if extended == "" {
		if slices.Contains(regionMatchingKeys, id.language) {
			id.region = strings.ToUpper(id.language)
			return true
		}
		return false
	}
	full := MustLangID(extended)
	id.language, id.script, id.region = full.language, full.script, full.region
	return true
}
