package config

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
)

// Layer application: a parsed tree applied onto a typed config, field
// by field, the way the derived ApplyConfigLayer/ApplyRuntimeLayer
// walk a #[wayle_config] tree (crates/wayle-derive/src/derives.rs,
// crates/wayle-config/src/property/config.rs):
//
//   - keys are matched canonical first, then aliases, then deprecated
//     aliases (which log a deprecation warning);
//   - unknown keys are ignored, and a section given a non-table value
//     is ignored;
//   - a leaf that fails to decode is a diagnostic and keeps its lower
//     layer's value. On the config layer the rest still applies; on the
//     runtime layer the first failure stops the walk and is returned,
//     like the derive's `?`.
//
// Effective values follow runtime > config > default because layers
// apply in that order and each leaf replaces its value wholesale.

type layerKind int

const (
	configLayer layerKind = iota
	runtimeLayer
)

type layerApply struct {
	kind layerKind
	sink DiagnosticSink
	// staged collects the runtime leaves that applied, at their
	// canonical paths, as encoded values — the ConfigProperty runtime
	// slots ExtractRuntimeValues persists.
	staged *table
	// overridden reports whether a runtime override is active at a
	// path; the config layer warns that its value is shadowed.
	overridden func(path string) bool
}

// RuntimeValueError is a runtime-layer value that failed to decode:
// "invalid value for '<path>': <error>".
type RuntimeValueError struct {
	Path string
	Err  error
}

func (e *RuntimeValueError) Error() string {
	return fmt.Sprintf("invalid value for '%s': %s", e.Path, e.Err)
}

func (e *RuntimeValueError) Unwrap() error { return e.Err }

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// applyContainer applies tree v onto the container struct dst.
func (a *layerApply) applyContainer(dst reflect.Value, v any, path string) error {
	if !isTable(v) {
		return nil
	}
	for _, f := range fieldsOf(dst.Type()) {
		if f.nolayer {
			continue
		}
		for _, key := range f.lookupKeys() {
			val, ok := lookup(v, key)
			if !ok {
				continue
			}
			child := joinPath(path, f.key)
			if f.isDeprecated(key) {
				log.Printf("config: deprecated config key %s; replace with canonical %s", joinPath(path, key), child)
			}
			field := dst.FieldByIndex(f.index)
			var err error
			if isContainer(f.typ) {
				err = a.applyContainer(field, val, child)
			} else {
				err = a.applyLeaf(field, val, child)
			}
			if err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func (a *layerApply) applyLeaf(dst reflect.Value, v any, path string) error {
	decoded, err := decodeLeaf(dst.Type(), v)
	if err != nil {
		title := "invalid config value"
		if a.kind == runtimeLayer {
			title = "invalid runtime value"
		}
		a.sink(Diagnostic{Kind: DiagnosticError, Title: title}.
			field("Field", path).
			field("Error", strings.TrimSpace(err.Error())).
			field("Value", formatDiagnosticValue(v)))
		if a.kind == runtimeLayer {
			return &RuntimeValueError{Path: path, Err: err}
		}
		return nil
	}
	dst.Set(decoded)
	switch a.kind {
	case runtimeLayer:
		if a.staged != nil {
			setStaged(a.staged, path, encodeValue(dst))
		}
	case configLayer:
		if a.overridden != nil && a.overridden(path) {
			d := Diagnostic{Kind: DiagnosticWarning, Title: "config.toml change ignored"}.
				field("Field", path).
				field("Reason", "runtime override active")
			d.Hint = "wayle config reset " + path
			a.sink(d)
		}
	}
	return nil
}

// formatDiagnosticValue is property/config.rs format_toml_value: the
// pretty TOML of a table, else the value's Debug form.
func formatDiagnosticValue(v any) string {
	if s, err := tomlPretty(v); err == nil {
		return strings.TrimSpace(s)
	}
	return debugValue(v)
}

// setStaged stores an encoded leaf at a dot path, creating tables.
func setStaged(root *table, path string, v any) {
	segments := strings.Split(path, ".")
	current := root
	for _, seg := range segments[:len(segments)-1] {
		next, ok := current.vals[seg].(*table)
		if !ok {
			next = newTable()
			current.set(seg, next)
		}
		current = next
	}
	current.set(segments[len(segments)-1], v)
}

// stagedAt reports whether a leaf is staged at path.
func stagedAt(root *table, path string) bool {
	var current any = root
	for seg := range strings.SplitSeq(path, ".") {
		t, ok := current.(*table)
		if !ok {
			return false
		}
		if current, ok = t.get(seg); !ok {
			return false
		}
	}
	return true
}

// deleteStaged removes the leaf at path and prunes emptied tables.
func deleteStaged(root *table, segments []string) {
	if len(segments) == 1 {
		root.delete(segments[0])
		return
	}
	child, ok := root.vals[segments[0]].(*table)
	if !ok {
		return
	}
	deleteStaged(child, segments[1:])
	if child.len() == 0 {
		root.delete(segments[0])
	}
}

// cloneTable deep-copies an encoded table.
func cloneTable(t *table) *table {
	out := newTable()
	for _, k := range t.keys {
		v := t.vals[k]
		if child, ok := v.(*table); ok {
			v = cloneTable(child)
		}
		out.set(k, v)
	}
	return out
}

// clearRuntimeByPath resolves a dot path through the schema to one
// leaf (accepting aliases) and returns its canonical path, with the
// ClearRuntimeByPath errors: an empty segment, an unknown field, or a
// path that continues past a leaf.
func clearRuntimePath(t reflect.Type, path string) (string, error) {
	segment, rest, _ := strings.Cut(path, ".")
	if segment == "" {
		return "", errors.New("empty path")
	}
	for _, f := range fieldsOf(t) {
		if f.nolayer {
			continue
		}
		for _, key := range f.lookupKeys() {
			if key != segment {
				continue
			}
			if isContainer(f.typ) {
				sub, err := clearRuntimePath(f.typ, rest)
				if err != nil {
					return "", err
				}
				return f.key + "." + sub, nil
			}
			if rest != "" {
				return "", fmt.Errorf("no nested field at '%s'", rest)
			}
			return f.key, nil
		}
	}
	return "", fmt.Errorf("unknown field '%s'", segment)
}
