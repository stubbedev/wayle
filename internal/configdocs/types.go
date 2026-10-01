package configdocs

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/stubbedev/wayle/config"
)

const typeSectionDepth = 2

// typeSlug is type_slug: CamelCase as kebab-case anchors.
func typeSlug(name string) string {
	var b strings.Builder
	for i, c := range name {
		if c < unicode.MaxASCII && unicode.IsUpper(c) && i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(strings.ToLower(string(c)))
	}
	return b.String()
}

// collectTypeDefs is collect_type_defs: the union of every section's
// $defs, the first section (by name) to define a name winning, the
// synthetic wrappers and primitives left out.
func collectTypeDefs(sections []config.DocSection) map[string]config.Schema {
	defs := map[string]config.Schema{}
	for _, sec := range sections {
		own, _ := sec.Schema["$defs"].(config.Schema)
		names := make([]string, 0, len(own))
		for n := range own {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if syntheticWrapper(n) {
				continue
			}
			if _, seen := defs[n]; !seen {
				defs[n], _ = own[n].(config.Schema)
			}
		}
	}
	return defs
}

func syntheticWrapper(name string) bool {
	if strings.HasPrefix(name, "Array_of_") || strings.HasPrefix(name, "Nullable_") || strings.HasPrefix(name, "Map_of_") {
		return true
	}
	switch name {
	case "boolean", "integer", "number", "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "float", "double":
		return true
	}
	return false
}

// typesPage is render_types_page.
func typesPage(defs map[string]config.Schema) string {
	var b strings.Builder
	b.WriteString("---\ntitle: Types\noutline: [2, 3]\n---\n\n<div v-pre>\n\n# Types\n\n")
	b.WriteString("Named types referenced across the config. Every field in [`/config/`](/config/) that shows a type like `Color` or `ClickAction` links here.\n\n")
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b.WriteString(typeSection(n, defs[n]))
	}
	b.WriteString("\n</div>\n")
	return b.String()
}

func typeSection(name string, def config.Schema) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s {#%s}\n\n", name, typeSlug(name))
	desc, _ := def["description"].(string)
	if trimmed := strings.TrimSpace(desc); trimmed != "" {
		b.WriteString(rehostRustdoc(trimmed, typeSectionDepth))
		b.WriteByte('\n')
	}
	if body, ok := typeBody(def); ok {
		b.WriteString(body)
		return b.String()
	}
	if structuredDescription(desc) {
		return b.String()
	}
	b.WriteString("See the schema for valid values.\n\n")
	return b.String()
}

// typeBody is render_type_body: the first renderer that applies.
func typeBody(def config.Schema) (string, bool) {
	if s, ok := anyOfBranches(def); ok {
		return s, true
	}
	return branchBody(def)
}

// branchBody tries every renderer but anyOf.
func branchBody(def config.Schema) (string, bool) {
	for _, render := range []func(config.Schema) (string, bool){oneOfVariants, enumValues, numericRange, stringPattern, objectShape} {
		if s, ok := render(def); ok {
			return s, true
		}
	}
	return "", false
}

func structuredDescription(desc string) bool {
	for _, l := range lines(desc) {
		if isFenceLine(l) {
			return true
		}
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "| ") {
			return true
		}
	}
	return false
}

func anyOfBranches(def config.Schema) (string, bool) {
	branches, _ := def["anyOf"].([]any)
	if len(branches) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, br := range branches {
		if s, ok := br.(config.Schema); ok {
			if text, ok := branchBody(s); ok {
				b.WriteString(text)
			}
		}
	}
	return b.String(), b.Len() > 0
}

func oneOfVariants(def config.Schema) (string, bool) {
	variants, _ := def["oneOf"].([]any)
	if len(variants) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("| Value | Meaning |\n|---|---|\n")
	rows := false
	for _, v := range variants {
		s, _ := v.(config.Schema)
		value, ok := s["const"]
		if !ok {
			continue
		}
		desc, _ := s["description"].(string)
		fmt.Fprintf(&b, "| `%s` | %s |\n", jsonLiteral(value), desc)
		rows = true
	}
	if !rows {
		return "", false
	}
	b.WriteByte('\n')
	return b.String(), true
}

func enumValues(def config.Schema) (string, bool) {
	values, _ := def["enum"].([]any)
	if len(values) == 0 {
		return "", false
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = "`" + jsonLiteral(v) + "`"
	}
	return "One of: " + strings.Join(parts, ", ") + ".\n\n", true
}

func numericRange(def config.Schema) (string, bool) {
	t, _ := def["type"].(string)
	if t != "integer" && t != "number" {
		return "", false
	}
	lo, hasLo := def["minimum"]
	hi, hasHi := def["maximum"]
	var block string
	switch {
	case hasLo && hasHi:
		block = fmt.Sprintf("Number in `[%s, %s]`.\n\n", jsonLiteral(lo), jsonLiteral(hi))
	case hasLo:
		block = fmt.Sprintf("Number `>= %s`.\n\n", jsonLiteral(lo))
	case hasHi:
		block = fmt.Sprintf("Number `<= %s`.\n\n", jsonLiteral(hi))
	default:
		block = "Any number.\n\n"
	}
	if f, ok := def["format"].(string); ok {
		block += fmt.Sprintf("Serialises as `%s`.\n\n", f)
	}
	return block, true
}

func stringPattern(def config.Schema) (string, bool) {
	if t, _ := def["type"].(string); t != "string" {
		return "", false
	}
	block := "String"
	if p, ok := def["pattern"].(string); ok {
		block += " matching `" + p + "`"
	}
	return block + ".\n\n", true
}

func objectShape(def config.Schema) (string, bool) {
	if t, _ := def["type"].(string); t != "object" {
		return "", false
	}
	props, _ := def["properties"].(config.Schema)
	order := config.PropertyOrder(props)
	if len(order) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("| Field | Description |\n|---|---|\n")
	for _, name := range order {
		prop, _ := props[name].(config.Schema)
		desc, _ := prop["description"].(string)
		first := ""
		if ls := lines(desc); len(ls) > 0 {
			first = strings.TrimSpace(ls[0])
		}
		fmt.Fprintf(&b, "| `%s` | %s |\n", name, first)
	}
	b.WriteByte('\n')
	return b.String(), true
}

// jsonLiteral is render_json_literal: a string quoted but unescaped,
// the rest as serde_json writes it.
func jsonLiteral(v any) string {
	if s, ok := v.(string); ok {
		return "\"" + s + "\""
	}
	return config.JSONText(v)
}
