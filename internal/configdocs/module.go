package configdocs

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/config"
)

const (
	pageH1Depth          = 1
	fieldSubsectionDepth = 3
)

// field is one property as the page renders it.
type field struct {
	name        string
	description string
	defaultCell string
	typeCell    string
	// refType is the named type the field points at, "" for none.
	refType string
}

// modulePage is generate_module_page.
func modulePage(sec config.DocSection, known map[string]bool) string {
	fields := collectFields(sec.Schema, known)
	buckets := bucketFields(fields, sec.Groups)
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %s\noutline: [2, 3]\n---\n\n# %s\n\n", sec.Name, sec.Name)
	b.WriteString("<div v-pre>\n\n")
	description := sec.Name
	if d, ok := sec.Schema["description"].(string); ok {
		description = d
	}
	b.WriteString(rehostRustdoc(strings.TrimSpace(description), pageH1Depth))
	b.WriteByte('\n')
	if id := sec.LayoutID; id != "" {
		fmt.Fprintf(&b, "Add it to your layout with `%s`:\n\n```toml\n[[bar.layout]]\nmonitor = \"*\"\nright = [\"%s\"]\n```\n\n", id, id)
	}
	for i, g := range sec.Groups {
		if len(buckets[i]) > 0 {
			renderGroup(&b, g, buckets[i])
		}
	}
	renderDefaults(&b, sec)
	b.WriteString("\n</div>\n")
	return b.String()
}

func collectFields(schema config.Schema, known map[string]bool) []field {
	props, _ := schema["properties"].(config.Schema)
	var out []field
	for _, name := range config.PropertyOrder(props) {
		prop, _ := props[name].(config.Schema)
		desc, _ := prop["description"].(string)
		def, has := prop["default"]
		out = append(out, field{
			name:        name,
			description: desc,
			defaultCell: defaultCell(def, has),
			typeCell:    typeCell(prop, known),
			refType:     refTypeName(prop),
		})
	}
	return out
}

// refTypeName is extract_ref_type_name: $ref, else allOf[0].$ref.
func refTypeName(prop config.Schema) string {
	if ref, ok := prop["$ref"].(string); ok {
		return refTail(ref)
	}
	if all, ok := prop["allOf"].([]any); ok && len(all) > 0 {
		if first, ok := all[0].(config.Schema); ok {
			if ref, ok := first["$ref"].(string); ok {
				return refTail(ref)
			}
		}
	}
	return ""
}

func refTail(ref string) string {
	if _, tail, ok := strings.CutLast(ref, "/"); ok {
		return tail
	}
	return ref
}

// defaultCell is render_default_cell.
func defaultCell(v any, has bool) string {
	if !has {
		return "required"
	}
	switch t := v.(type) {
	case string:
		return "`\"" + t + "\"`"
	case bool:
		return "`" + strconv.FormatBool(t) + "`"
	case int64:
		return "`" + strconv.FormatInt(t, 10) + "`"
	case uint64:
		return "`" + strconv.FormatUint(t, 10) + "`"
	case int:
		return "`" + strconv.Itoa(t) + "`"
	case float64:
		return "`" + formatNumber(t) + "`"
	case []any:
		if len(t) == 0 {
			return "`[]`"
		}
		return "`[...]`"
	case config.Schema:
		n := 0
		for k := range t {
			if !strings.HasPrefix(k, "\x00") {
				n++
			}
		}
		if n == 0 {
			return "`{}`"
		}
		return "`{...}`"
	case nil:
		return "`null`"
	}
	return "`" + config.JSONText(v) + "`"
}

// formatNumber is format_number for a float: a value that was an f32
// before schemars widened it prints as that f32 (0.35, not
// 0.3499999940395355), the rest at f64 precision, both in Rust's
// Display form (no exponent, no ".0").
func formatNumber(f float64) string {
	narrowed := float32(f)
	if math.Abs(float64(narrowed)-f) < 2.220446049250313e-16*64 {
		return strconv.FormatFloat(float64(narrowed), 'f', -1, 32)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// typeCell is render_type_cell.
func typeCell(prop config.Schema, known map[string]bool) string {
	if name := refTypeName(prop); name != "" {
		return typeLink(name, known)
	}
	schemaType, ok := prop["type"].(string)
	if !ok {
		schemaType = "unknown"
	}
	format, _ := prop["format"].(string)
	if p, ok := primitiveName(schemaType, format); ok {
		return p
	}
	return schemaType
}

// typeLink is render_type_link.
func typeLink(name string, known map[string]bool) string {
	if p, ok := primitiveAlias(name); ok {
		return p
	}
	if inner, ok := strings.CutPrefix(name, "Array_of_"); ok {
		return "array of " + typeLink(inner, known)
	}
	if inner, ok := strings.CutPrefix(name, "Nullable_"); ok {
		return typeLink(inner, known) + " or null"
	}
	if inner, ok := strings.CutPrefix(name, "Map_of_"); ok {
		return "map of " + typeLink(inner, known)
	}
	if known[name] {
		return fmt.Sprintf("[`%s`](/config/types#%s)", name, typeSlug(name))
	}
	return "`" + name + "`"
}

// primitiveName is primitive_name.
func primitiveName(schemaType, format string) (string, bool) {
	switch schemaType {
	case "boolean":
		return "bool", true
	case "string":
		return "string", true
	case "integer":
		switch format {
		case "uint32":
			return "u32", true
		case "int32":
			return "i32", true
		case "uint64":
			return "u64", true
		case "int64":
			return "i64", true
		case "uint":
			return "usize", true
		case "int":
			return "isize", true
		}
		return "integer", true
	case "number":
		switch format {
		case "float":
			return "f32", true
		case "double":
			return "f64", true
		}
		return "number", true
	case "array":
		return "array", true
	case "object":
		return "object", true
	}
	return "", false
}

// primitiveAlias is primitive_name_from_alias.
func primitiveAlias(name string) (string, bool) {
	p, ok := map[string]string{
		"boolean": "bool", "string": "string",
		"uint8": "u8", "uint16": "u16", "uint32": "u32", "uint64": "u64",
		"int8": "i8", "int16": "i16", "int32": "i32", "int64": "i64",
		"uint": "usize", "int": "isize", "float": "f32", "double": "f64",
	}[name]
	return p, ok
}

// bucketFields is bucket_fields: each field to the first non-catch-all
// group whose rule matches, else the catch-all, else nowhere.
func bucketFields(fields []field, groups []config.DocGroup) [][]field {
	catchAll := -1
	for i, g := range groups {
		if g.Rule == config.DocCatchAll {
			catchAll = i
			break
		}
	}
	buckets := make([][]field, len(groups))
	for _, f := range fields {
		target := catchAll
		for i, g := range groups {
			if i != catchAll && ruleMatches(g, f) {
				target = i
				break
			}
		}
		if target >= 0 {
			buckets[target] = append(buckets[target], f)
		}
	}
	return buckets
}

func ruleMatches(g config.DocGroup, f field) bool {
	switch g.Rule {
	case config.DocPrefix:
		return strings.HasPrefix(f.name, g.Arg)
	case config.DocStandalone:
		return f.name == g.Arg
	case config.DocByType:
		return f.refType == g.Arg
	}
	return false
}

func renderGroup(b *strings.Builder, g config.DocGroup, fields []field) {
	fmt.Fprintf(b, "## %s\n\n| Field | Type | Default | Description |\n|---|---|---|---|\n", g.Title)
	for _, f := range fields {
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", f.name, f.typeCell, f.defaultCell,
			strings.ReplaceAll(summaryLine(f.description), "|", "\\|"))
	}
	b.WriteByte('\n')
	for _, f := range fields {
		if !hasRichBody(f.description) {
			continue
		}
		body := bodyAfterSummary(f.description)
		if strings.TrimSpace(body) == "" {
			continue
		}
		fmt.Fprintf(b, "::: details More about `%s`\n\n", f.name)
		b.WriteString(rehostRustdoc(strings.TrimSpace(body), fieldSubsectionDepth))
		b.WriteString("\n:::\n\n")
	}
}

// renderDefaults is render_default_toml.
func renderDefaults(b *strings.Builder, sec config.DocSection) {
	props, _ := sec.Schema["properties"].(config.Schema)
	if len(config.PropertyOrder(props)) == 0 {
		return
	}
	toml := sec.DefaultsTOML()
	if toml == "" && len(sec.Required) == 0 {
		return
	}
	b.WriteString("## Default configuration\n\n")
	if len(sec.Required) > 0 {
		names := make([]string, len(sec.Required))
		for i, r := range sec.Required {
			names[i] = "`" + r + "`"
		}
		fmt.Fprintf(b, "Required fields (must be set in your config): %s.\n\n", strings.Join(names, ", "))
	}
	if toml == "" {
		return
	}
	b.WriteString("```toml\n")
	b.WriteString(toml)
	if !strings.HasSuffix(toml, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n\n")
}

// summaryLine is summary_line: the first paragraph joined on one line.
func summaryLine(description string) string {
	var parts []string
	started := false
	for _, l := range lines(description) {
		blank := strings.TrimSpace(l) == ""
		if !started {
			if blank {
				continue
			}
			started = true
		}
		if blank {
			break
		}
		parts = append(parts, strings.TrimSpace(l))
	}
	return strings.Join(parts, " ")
}

// paragraphsAfterFirst are the lines past the first paragraph (the
// leading blank lines skipped, then the first paragraph).
func paragraphsAfterFirst(description string) []string {
	ls := lines(description)
	i := 0
	for i < len(ls) && strings.TrimSpace(ls[i]) == "" {
		i++
	}
	for i < len(ls) && strings.TrimSpace(ls[i]) != "" {
		i++
	}
	return ls[i:]
}

// bodyAfterSummary is body_after_summary.
func bodyAfterSummary(description string) string {
	return strings.Join(paragraphsAfterFirst(description), "\n")
}

// hasRichBody is has_rich_body.
func hasRichBody(description string) bool {
	for _, l := range paragraphsAfterFirst(description) {
		if strings.TrimSpace(l) != "" {
			return true
		}
	}
	return false
}
