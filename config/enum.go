package config

import (
	"fmt"
	"slices"
	"strings"
)

// parseEnum decodes one config enum string: the value must be one of
// values (the serde spellings), anything else is a load error naming
// the accepted set. what names the enum in the error.
func parseEnum[T ~string](text []byte, what string, values ...T) (T, error) {
	v := T(text)
	if slices.Contains(values, v) {
		return v, nil
	}
	names := make([]string, len(values))
	for i, value := range values {
		names[i] = string(value)
	}
	return "", fmt.Errorf("config: invalid %s %q (want %s)", what, text, strings.Join(names, "|"))
}
