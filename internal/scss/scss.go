// Package scss compiles the SCSS users write for wayle style overrides
// into plain CSS. It stands in for the grass compiler the Rust shell
// runs over <config dir>/styles/index.scss
// (crates/wayle-styling/src/lib.rs, try_user_css): the Go port is pure
// Go, and no maintained pure-Go Sass compiler exists.
//
// The compiler implements a subset of SCSS with dart-sass/grass
// semantics:
//
//   - plain CSS, including at-rules (@media, @supports, @keyframes,
//     @font-face, @layer, and unknown ones such as GTK's @define-color);
//   - // and /* */ comments (both dropped);
//   - nested rules, the parent selector & (suffixes, pseudo-class
//     arguments, several & in one selector), leading combinators,
//     comma lists expanded as a cartesian product, nested properties;
//   - variables with !default and !global and Sass's block scoping, and
//     #{} interpolation in selectors, property names, values, and
//     at-rule preludes;
//   - SassScript values: numbers with units (arithmetic, unit
//     conversion, the slash-separator rules), strings, colors, lists,
//     booleans, null, comparisons, and/or/not; calc(), min(), max(),
//     clamp() simplified like Sass; rgb(), rgba(), hsl(), hsla();
//     every other function passed through as plain CSS;
//   - @import and @use (as, as *, with) with Sass's partial resolution
//     order relative to the importing file and then the load paths;
//     import cycles and module loops are errors;
//   - @mixin, @include with positional, keyword, and default arguments,
//     and @content.
//
// Everything else — control flow (@if, @each, @for, @while),
// @function, @extend and placeholder selectors, @at-root, @forward,
// maps, rest arguments, and Sass's built-in functions such as
// darken() — fails the compile with an *Error matching ErrUnsupported
// that names the construct, file, and line. That is the failure mode
// of a grass compile error: the caller logs it and drops the user's
// stylesheet, never silently emitting CSS that differs from what Sass
// would produce.
package scss

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrUnsupported is matched (errors.Is) by an *Error whose stylesheet
// uses Sass outside the supported subset.
var ErrUnsupported = errors.New("unsupported SCSS feature")

// Error is a compile failure at one place in one stylesheet.
type Error struct {
	// File is the path of the stylesheet the failure is in.
	File string
	// Line is the 1-based line of the offending construct.
	Line int
	// Msg says what is wrong.
	Msg string

	unsupported bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
}

// Unwrap makes an unsupported-feature error match ErrUnsupported.
func (e *Error) Unwrap() error {
	if e.unsupported {
		return ErrUnsupported
	}
	return nil
}

// Compile compiles the SCSS stylesheet at path to CSS. Imports resolve
// relative to the importing file first, then against each load path in
// order. A read failure of path itself is returned as the os error; a
// failure anywhere inside the stylesheet (syntax, evaluation, a missing
// or unreadable import, an unsupported feature) is an *Error.
func Compile(path string, loadPaths ...string) (css string, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	text, err := os.ReadFile(abs) //nolint:gosec // compiling the caller's stylesheet is the job
	if err != nil {
		return "", err
	}
	defer catch(&err)
	c := newCompiler(loadPaths)
	c.runEntry(newSource(abs, string(text)))
	return c.output(), nil
}

// catch turns an *Error panic (the parser's and evaluator's internal
// failure path) into a returned error; any other panic propagates.
func catch(err *error) {
	r := recover()
	if r == nil {
		return
	}
	if e, ok := r.(*Error); ok {
		*err = e
		return
	}
	panic(r)
}
