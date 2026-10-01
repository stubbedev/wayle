package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// String enums are registered once with their variants in schema
// order; decoding, encoding, and the JSON Schema read the registry, so
// a variant exists in all three or in none. Variant descriptions come
// from the constants' doc comments (see schemadoc.go).

var enumRegistry sync.Map // reflect.Type -> []string

// registerEnum records T's variants. Call it from a package-level var
// initializer next to the constants:
//
//	var _ = registerEnum(LocationTop, LocationBottom, LocationLeft, LocationRight)
func registerEnum[T ~string](values ...T) struct{} {
	variants := make([]string, len(values))
	for i, v := range values {
		variants[i] = string(v)
	}
	if _, loaded := enumRegistry.LoadOrStore(reflect.TypeFor[T](), variants); loaded {
		panic(fmt.Sprintf("config: enum %s registered twice", reflect.TypeFor[T]()))
	}
	return struct{}{}
}

// enumVariants returns t's registered variants, or nil when t is not a
// registered enum.
func enumVariants(t reflect.Type) []string {
	if v, ok := enumRegistry.Load(t); ok {
		variants, _ := v.([]string)
		return variants
	}
	return nil
}

// decodeEnum validates s against the variants with serde's wording.
func decodeEnum(variants []string, v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", errors.New("invalid type: unit variant, expected string only")
	}
	if slices.Contains(variants, s) {
		return s, nil
	}
	return "", fmt.Errorf("unknown variant `%s`, %s", s, expectedOneOf(variants))
}

// typeOf is reflect.TypeFor, kept short for the schema tables.
func typeOf[T any]() reflect.Type { return reflect.TypeFor[T]() }

// valueOf returns the addressable value behind a pointer.
func valueOf[T any](p *T) reflect.Value { return reflect.ValueOf(p).Elem() }

// expectedOneOf renders serde's OneOf list.
func expectedOneOf(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	switch len(quoted) {
	case 0:
		return "there are no variants"
	case 1:
		return "expected " + quoted[0]
	case 2:
		return "expected " + quoted[0] + " or " + quoted[1]
	}
	return "expected one of " + strings.Join(quoted, ", ")
}
