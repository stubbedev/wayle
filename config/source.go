package config

import (
	"strings"
)

// ValueSource is the layer a field's effective value comes from: the
// built-in default, the config file, or a runtime override
// (wayle-config's ValueSource) - what the settings GUI badges.
type ValueSource int

// The layers, lowest first.
const (
	SourceDefault ValueSource = iota
	SourceConfig
	SourceRuntime
)

func (v ValueSource) String() string {
	return [...]string{"default", "config", "runtime"}[v]
}

// configLeaves is the leaves a config document sets, at canonical
// paths, as encoded values.
func configLeaves(tree any) *table {
	staged := newTable()
	if tree == nil {
		return staged
	}
	ca := &layerApply{kind: configLayer, sink: DiscardDiagnostics, staged: staged}
	_ = ca.applyContainer(valueOf(Defaults()), tree, "")
	return staged
}

// canonicalPath resolves a dot path (aliases allowed) to its canonical
// keys; false for a path naming no field.
func canonicalPath(path string) (string, bool) {
	t := typeOf[Config]()
	var keys []string
	for seg := range strings.SplitSeq(path, ".") {
		_, f, ok := fieldAtPath(t, seg)
		if !ok {
			return "", false
		}
		keys = append(keys, f.key)
		t = f.typ
	}
	return strings.Join(keys, "."), len(keys) > 0
}

// Source is the layer the value at a dot path comes from. A container
// path takes the highest layer any leaf under it uses.
func (s *Service) Source(path string) ValueSource {
	canonical, ok := canonicalPath(path)
	if !ok {
		return SourceDefault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case stagedAt(s.staged, canonical):
		return SourceRuntime
	case stagedAt(s.configSet, canonical):
		return SourceConfig
	}
	return SourceDefault
}

// ConfigValue is the value the config file sets at a dot path, false
// when it sets none (the property's config() slot).
func (s *Service) ConfigValue(path string) (any, bool) {
	canonical, ok := canonicalPath(path)
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return stagedValue(s.configSet, canonical)
}

// RuntimeValue is the runtime override at a dot path, false for none.
func (s *Service) RuntimeValue(path string) (any, bool) {
	canonical, ok := canonicalPath(path)
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return stagedValue(s.staged, canonical)
}

// DefaultValue is the built-in default at a dot path, encoded as the
// config serializes it.
func DefaultValue(path string) (any, error) {
	v := widenFloats(encode(Defaults()))
	for seg := range strings.SplitSeq(path, ".") {
		next, ok := lookup(v, seg)
		if !ok {
			return nil, &InvalidFieldError{Field: seg, Component: path, Reason: FieldNotFound}
		}
		v = next
	}
	return v, nil
}

// stagedValue is the leaf staged at a canonical path, plain.
func stagedValue(root *table, path string) (any, bool) {
	var current any = root
	for seg := range strings.SplitSeq(path, ".") {
		t, ok := current.(*table)
		if !ok {
			return nil, false
		}
		if current, ok = t.get(seg); !ok {
			return nil, false
		}
	}
	return widenFloats(toPlain(current)), true
}
