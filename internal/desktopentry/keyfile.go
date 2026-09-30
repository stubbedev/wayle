// Package desktopentry reads freedesktop desktop entries the way gio
// does (GKeyFile + GDesktopAppInfo): the key-file grammar with its
// escapes and localized keys, the application directories and desktop
// ids, visibility (NoDisplay, Hidden, OnlyShowIn/NotShowIn, TryExec),
// Exec field-code expansion, and launching - pure Go, no gio.
package desktopentry

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

// DesktopGroup is the main group of a desktop entry.
const DesktopGroup = "Desktop Entry"

// KeyFile is a parsed key file: groups of key=value lines, with the
// localized variants (Name[de]) kept under their full key.
type KeyFile struct {
	groups map[string]map[string]string
	// languages is the locale fallback list LocaleString tries.
	languages []string
}

// ErrMalformed is a key file that does not parse.
var ErrMalformed = errors.New("desktopentry: malformed key file")

// ParseKeyFile parses a key file. Blank lines and # comments are
// skipped; a key before any [group] or a line without '=' is
// malformed, as GKeyFile refuses them.
func ParseKeyFile(data []byte) (*KeyFile, error) {
	kf := &KeyFile{groups: map[string]map[string]string{}, languages: Languages()}
	var group map[string]string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimLeft(sc.Text(), " \t")
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "[") {
			name, ok := strings.CutSuffix(strings.TrimRight(text, " \t"), "]")
			if !ok {
				return nil, fmt.Errorf("%w: line %d: unterminated group", ErrMalformed, line)
			}
			name = name[1:]
			if _, dup := kf.groups[name]; !dup {
				kf.groups[name] = map[string]string{}
			}
			group = kf.groups[name]
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok || group == nil {
			return nil, fmt.Errorf("%w: line %d", ErrMalformed, line)
		}
		key = strings.TrimRight(key, " \t")
		// A repeated key keeps the last value, as GKeyFile does.
		group[key] = strings.TrimLeft(value, " \t")
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return kf, nil
}

// LoadKeyFile reads and parses a key file from disk.
func LoadKeyFile(path string) (*KeyFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // a desktop entry path the caller resolved
	if err != nil {
		return nil, err
	}
	return ParseKeyFile(data)
}

// HasGroup reports whether the group exists.
func (kf *KeyFile) HasGroup(group string) bool {
	_, ok := kf.groups[group]
	return ok
}

// Raw returns a key's value with no unescaping.
func (kf *KeyFile) Raw(group, key string) (string, bool) {
	v, ok := kf.groups[group][key]
	return v, ok
}

// String returns a key's value with the string escapes (\s \n \t \r
// \\) decoded.
func (kf *KeyFile) String(group, key string) (string, bool) {
	v, ok := kf.Raw(group, key)
	if !ok {
		return "", false
	}
	return unescape(v), true
}

// LocaleString returns the best localized variant of a key
// (Name[de_DE], Name[de], ...), else the plain key.
func (kf *KeyFile) LocaleString(group, key string) (string, bool) {
	for _, lang := range kf.languages {
		if v, ok := kf.String(group, key+"["+lang+"]"); ok {
			return v, true
		}
	}
	return kf.String(group, key)
}

// Bool reads a boolean key: exactly "true" or "false" (GKeyFile also
// takes 1 and 0); absent or anything else is false.
func (kf *KeyFile) Bool(group, key string) bool {
	v, ok := kf.Raw(group, key)
	if !ok {
		return false
	}
	switch strings.TrimSpace(v) {
	case "true", "1":
		return true
	}
	return false
}

// List reads a ;-separated list, escapes decoded and \; kept literal.
func (kf *KeyFile) List(group, key string) []string {
	v, ok := kf.Raw(group, key)
	if !ok {
		return nil
	}
	return splitList(v)
}

// LocaleList is List over the best localized variant.
func (kf *KeyFile) LocaleList(group, key string) []string {
	for _, lang := range kf.languages {
		if v, ok := kf.Raw(group, key+"["+lang+"]"); ok {
			return splitList(v)
		}
	}
	return kf.List(group, key)
}

func splitList(v string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(v); i++ {
		switch {
		case v[i] == '\\' && i+1 < len(v) && v[i+1] == ';':
			cur.WriteByte(';')
			i++
		case v[i] == '\\' && i+1 < len(v):
			cur.WriteString(unescape(v[i : i+2]))
			i++
		case v[i] == ';':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(v[i])
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func unescape(v string) string {
	if !strings.Contains(v, `\`) {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' || i+1 >= len(v) {
			b.WriteByte(v[i])
			continue
		}
		i++
		switch v[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(v[i])
		}
	}
	return b.String()
}

// Languages is g_get_language_names for the message locale:
// LC_ALL, then LC_MESSAGES, then LANG, widened from lang_COUNTRY@MOD
// to lang_COUNTRY, lang@MOD, and lang. C and POSIX name no language.
func Languages() []string {
	locale := ""
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(key); v != "" {
			locale = v
			break
		}
	}
	return languagesFor(locale)
}

func languagesFor(locale string) []string {
	if locale == "" || locale == "C" || locale == "POSIX" {
		return nil
	}
	modifier := ""
	if i := strings.IndexByte(locale, '@'); i >= 0 {
		locale, modifier = locale[:i], locale[i:]
	}
	if i := strings.IndexByte(locale, '.'); i >= 0 {
		locale = locale[:i]
	}
	lang, country, hasCountry := strings.Cut(locale, "_")
	var out []string
	if hasCountry {
		if modifier != "" {
			out = append(out, lang+"_"+country+modifier)
		}
		out = append(out, lang+"_"+country)
	}
	if modifier != "" {
		out = append(out, lang+modifier)
	}
	return append(out, lang)
}
