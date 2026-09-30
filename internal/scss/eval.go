package scss

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// maxIncludeDepth bounds mixin recursion, which has no base case
// without @if.
const maxIncludeDepth = 100

// compiler evaluates parsed stylesheets into a CSS tree.
type compiler struct {
	loadPaths []string
	// modules are loaded @use modules by canonical path; loading marks
	// the ones mid-evaluation (a second @use of one is a module loop).
	modules map[string]*module
	loading map[string]bool
	// active is the stack of files being evaluated; @importing one is
	// an import cycle.
	active     []string
	cssImports []string
	root       *cssBlock
	depth      int
}

func newCompiler(loadPaths []string) *compiler {
	c := &compiler{
		modules: map[string]*module{},
		loading: map[string]bool{},
		root:    &cssBlock{},
	}
	for _, p := range loadPaths {
		if abs, err := filepath.Abs(p); err == nil {
			c.loadPaths = append(c.loadPaths, abs)
		}
	}
	return c
}

// module is one @use module's (or the entry stylesheet's) global
// scope, and the `with` configuration its !default variables consume.
type module struct {
	globals *scope
	config  map[string]*configEntry
}

type configEntry struct {
	v    value
	used bool
}

// scope is one lexical block's variables and mixins; a module's global
// scope has no parent.
type scope struct {
	vars   map[string]value
	mixins map[string]*mixin
	parent *scope
}

func newScope(parent *scope) *scope {
	return &scope{vars: map[string]value{}, mixins: map[string]*mixin{}, parent: parent}
}

func (s *scope) root() *scope {
	for s.parent != nil {
		s = s.parent
	}
	return s
}

// fileCtx is per source file: @use namespaces belong to the file that
// wrote them, even when it was @imported into another.
type fileCtx struct {
	src        *source
	mod        *module
	namespaces map[string]*module
	stars      []*module
}

// env is where an expression or statement evaluates: its scope, its
// file, and the @content block its mixin was included with.
type env struct {
	scope   *scope
	file    *fileCtx
	content *content
}

func (e env) child() env {
	e.scope = newScope(e.scope)
	return e
}

type mixin struct {
	rule    *mixinRule
	closure env
}

// content is an @include's content block with the env it closes over.
type content struct {
	body []stmt
	env  env
}

// out is where statements emit CSS.
type out struct {
	// container receives new blocks (the root, or an at-rule's body).
	container *cssBlock
	// rule receives declarations; nil creates a rule for selectors on
	// the first one.
	rule *cssBlock
	// selectors are the resolved enclosing selectors; nil outside any
	// style rule.
	selectors []string
	media     *mediaCtx
	keyframes bool
	// prefix is a nested property block's name prefix (`font-`).
	prefix string
}

// mediaCtx is the enclosing @media's queries and the container its
// block went into, where a nested @media's merged block goes too.
type mediaCtx struct {
	queries []string
	parent  *cssBlock
}

func (o *out) target(at pos) *cssBlock {
	if o.rule == nil {
		if o.selectors == nil {
			at.fail("declarations may only be used within style rules")
		}
		o.rule = o.container.add(&cssBlock{head: strings.Join(o.selectors, ", ")})
	}
	return o.rule
}

func (c *compiler) runEntry(src *source) {
	c.loading[src.path] = true
	mod := &module{globals: newScope(nil)}
	c.runFile(src, mod, &out{container: c.root}, nil)
	delete(c.loading, src.path)
}

// runFile evaluates a whole file into o. A nil e evaluates it at its
// module's top level; an @import passes the importing env.
func (c *compiler) runFile(src *source, mod *module, o *out, e *env) {
	stmts := parse(src)
	f := &fileCtx{src: src, mod: mod, namespaces: map[string]*module{}}
	fe := env{scope: mod.globals, file: f}
	if e != nil {
		fe.scope, fe.content = e.scope, e.content
	}
	c.active = append(c.active, src.path)
	c.exec(stmts, o, fe)
	c.active = c.active[:len(c.active)-1]
}

func (c *compiler) exec(stmts []stmt, o *out, e env) {
	for _, s := range stmts {
		if o.prefix != "" {
			switch s.(type) {
			case *decl, *varDecl:
			default:
				s.stmtPos().fail("only properties are allowed in a nested property block")
			}
		}
		switch x := s.(type) {
		case *styleRule:
			c.styleRule(x, o, e)
		case *decl:
			c.decl(x, o, e)
		case *varDecl:
			c.varDecl(x, e)
		case *atRule:
			c.atRule(x, o, e)
		case *mediaRule:
			c.media(x, o, e)
		case *keyframesRule:
			prelude := collapseSpace(c.interpText(x.prelude, e))
			b := o.container.add(&cssBlock{head: "@" + x.name + " " + prelude})
			c.exec(x.body, &out{container: b, keyframes: true}, e.child())
		case *importRule:
			c.importRule(x, o, e)
		case *useRule:
			c.useRule(x, e)
		case *mixinRule:
			e.scope.mixins[x.name] = &mixin{rule: x, closure: e}
		case *includeRule:
			c.include(x, o, e)
		case *contentRule:
			if e.content != nil {
				ce := e.content.env
				c.exec(e.content.body, o, env{scope: newScope(ce.scope), file: ce.file, content: ce.content})
			}
		}
	}
}

func (c *compiler) styleRule(x *styleRule, o *out, e env) {
	text := collapseSpace(c.interpText(x.sel, e))
	if o.keyframes {
		var sels []string
		for _, s := range splitList(text) {
			sels = append(sels, strings.TrimSpace(s))
		}
		sel := strings.Join(sels, ", ")
		b := o.container.add(&cssBlock{head: sel})
		c.exec(x.body, &out{container: o.container, rule: b, selectors: []string{sel}}, e.child())
		return
	}
	resolved := resolveSelectors(x.pos, o.selectors, text)
	b := o.container.add(&cssBlock{head: strings.Join(resolved, ", ")})
	c.exec(x.body, &out{container: o.container, rule: b, selectors: resolved, media: o.media}, e.child())
}

func (c *compiler) decl(x *decl, o *out, e env) {
	name := o.prefix + collapseSpace(c.interpText(x.name, e))
	if x.isRaw {
		o.target(x.pos).addDecl(name, collapseSpace(c.interpText(x.raw, e)))
		return
	}
	if x.value != nil {
		if v := css(c.eval(x.value, e)); v != "" {
			o.target(x.pos).addDecl(name, v)
		}
	}
	if x.body != nil {
		sub := *o
		sub.rule = o.target(x.pos)
		sub.prefix = name + "-"
		c.exec(x.body, &sub, e.child())
	}
}

func (c *compiler) varDecl(x *varDecl, e env) {
	if x.ns != "" {
		mod := e.namespace(x.pos, x.ns)
		checkPublic(x.pos, x.name)
		cur, ok := mod.globals.vars[x.name]
		if !ok {
			x.fail("undefined variable $%s.%s", x.ns, x.name)
		}
		if x.guarded && !isNull(cur) {
			return
		}
		mod.globals.vars[x.name] = stripSlash(c.eval(x.value, e))
		return
	}
	if x.guarded {
		if e.scope.parent == nil {
			if entry, ok := e.file.mod.config[x.name]; ok {
				entry.used = true
				e.scope.vars[x.name] = entry.v
				return
			}
		}
		lookup := e
		if x.global {
			lookup.scope = e.scope.root()
		}
		if cur, ok := lookup.findVar(x.name); ok && !isNull(cur) {
			return
		}
	}
	e.setVar(x.name, stripSlash(c.eval(x.value, e)), x.global)
}

func isNull(v value) bool {
	_, ok := v.(nullValue)
	return ok
}

// setVar assigns like Sass: !global and the top level write the module
// scope; otherwise the innermost local scope that already has the
// variable, or else the current one (shadowing a global).
func (e env) setVar(name string, v value, global bool) {
	if global || e.scope.parent == nil {
		e.scope.root().vars[name] = v
		return
	}
	for s := e.scope; s.parent != nil; s = s.parent {
		if _, ok := s.vars[name]; ok {
			s.vars[name] = v
			return
		}
	}
	e.scope.vars[name] = v
}

func (e env) findVar(name string) (value, bool) {
	for s := e.scope; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v, true
		}
	}
	if !isPrivate(name) {
		for _, m := range e.file.stars {
			if v, ok := m.globals.vars[name]; ok {
				return v, true
			}
		}
	}
	return nil, false
}

func (e env) lookupVar(x *varRef) value {
	if x.ns != "" {
		mod := e.namespace(x.pos, x.ns)
		checkPublic(x.pos, x.name)
		v, ok := mod.globals.vars[x.name]
		if !ok {
			x.fail("undefined variable $%s.%s", x.ns, x.name)
		}
		return v
	}
	v, ok := e.findVar(x.name)
	if !ok {
		x.fail("undefined variable $%s", x.name)
	}
	return v
}

func (e env) lookupMixin(at pos, ns, name string) *mixin {
	if ns != "" {
		mod := e.namespace(at, ns)
		checkPublic(at, name)
		if m, ok := mod.globals.mixins[name]; ok {
			return m
		}
		at.fail("undefined mixin %s.%s", ns, name)
	}
	for s := e.scope; s != nil; s = s.parent {
		if m, ok := s.mixins[name]; ok {
			return m
		}
	}
	if !isPrivate(name) {
		for _, mod := range e.file.stars {
			if m, ok := mod.globals.mixins[name]; ok {
				return m
			}
		}
	}
	at.fail("undefined mixin %s", name)
	return nil
}

func (e env) namespace(at pos, ns string) *module {
	mod, ok := e.file.namespaces[ns]
	if !ok {
		at.fail("there is no module with the namespace %q", ns)
	}
	return mod
}

// isPrivate reports a module-private member name (leading - or _).
func isPrivate(name string) bool { return strings.HasPrefix(name, "-") }

func checkPublic(at pos, name string) {
	if isPrivate(name) {
		at.fail("private members can't be accessed from outside their modules")
	}
}

func (c *compiler) atRule(x *atRule, o *out, e env) {
	head := "@" + x.name
	if prelude := collapseSpace(c.interpText(x.prelude, e)); prelude != "" {
		head += " " + prelude
	}
	if x.bodyless {
		o.container.add(&cssBlock{head: head, bodyless: true})
		return
	}
	b := o.container.add(&cssBlock{head: head})
	sub := &out{container: b, selectors: o.selectors}
	if o.selectors == nil && !x.supports {
		// @font-face, @page, and the like hold declarations directly.
		sub.rule = b
	}
	c.exec(x.body, sub, e.child())
}

func (c *compiler) media(x *mediaRule, o *out, e env) {
	query := collapseSpace(c.interpText(x.query, e))
	var queries []string
	for _, q := range splitList(query) {
		queries = append(queries, strings.TrimSpace(q))
	}
	var b *cssBlock
	var ctx *mediaCtx
	if o.media != nil {
		if merged, ok := mergeMedia(o.media.queries, queries); ok {
			b = o.media.parent.add(&cssBlock{head: "@media " + strings.Join(merged, ", ")})
			ctx = &mediaCtx{queries: merged, parent: o.media.parent}
		}
	}
	if b == nil {
		// Not mergeable (a media type inside a query): nest the block,
		// which CSS reads as the same conjunction.
		b = o.container.add(&cssBlock{head: "@media " + query})
		ctx = &mediaCtx{queries: queries, parent: o.container}
	}
	c.exec(x.body, &out{container: b, selectors: o.selectors, media: ctx}, e.child())
}

// mergeMedia conjoins nested media query lists like Sass, when every
// inner query is a feature list (`(min-width: 1px)`).
func mergeMedia(outer, inner []string) ([]string, bool) {
	var merged []string
	for _, o := range outer {
		for _, i := range inner {
			if !strings.HasPrefix(i, "(") {
				return nil, false
			}
			merged = append(merged, o+" and "+i)
		}
	}
	return merged, true
}

func (c *compiler) importRule(x *importRule, o *out, e env) {
	for _, t := range x.targets {
		if t.plain {
			if o.container != c.root || o.selectors != nil {
				t.unsupported("a plain CSS @import inside a block")
			}
			text := t.raw
			if x.modifiers != nil {
				text += " " + collapseSpace(c.interpText(x.modifiers, e))
			}
			c.cssImports = append(c.cssImports, "@import "+text+";")
			continue
		}
		p := c.resolve(t.pos, t.url, filepath.Dir(e.file.src.path))
		if slices.Contains(c.active, p) {
			t.fail("this file is already being loaded")
		}
		c.runFile(c.read(t.pos, p), e.file.mod, o, &e)
	}
}

func (c *compiler) useRule(x *useRule, e env) {
	if strings.HasPrefix(x.url, "sass:") {
		x.unsupported("the built-in module " + x.url)
	}
	p := c.resolve(x.pos, x.url, filepath.Dir(e.file.src.path))
	mod := c.modules[p]
	if mod != nil && len(x.config) > 0 {
		x.fail(`this module was already loaded, so it can't be configured using "with"`)
	}
	if mod == nil {
		if c.loading[p] {
			x.fail("module loop: this module is already being loaded")
		}
		mod = &module{globals: newScope(nil), config: map[string]*configEntry{}}
		for _, cv := range x.config {
			mod.config[cv.name] = &configEntry{v: stripSlash(c.eval(cv.value, e))}
		}
		c.loading[p] = true
		c.runFile(c.read(x.pos, p), mod, &out{container: c.root}, nil)
		delete(c.loading, p)
		for _, cv := range x.config {
			if !mod.config[cv.name].used {
				cv.fail("$%s was not declared with !default in the @used module", cv.name)
			}
		}
		mod.config = nil
		c.modules[p] = mod
	}
	ns := x.ns
	if ns == "" {
		ns = defaultNamespace(x.url)
	}
	if ns == "*" {
		e.file.stars = append(e.file.stars, mod)
		return
	}
	if _, dup := e.file.namespaces[ns]; dup {
		x.fail("there's already a module with namespace %q", ns)
	}
	e.file.namespaces[ns] = mod
}

// defaultNamespace is a @use URL's last component without extension or
// partial underscore.
func defaultNamespace(url string) string {
	base := path.Base(url)
	for _, ext := range []string{".scss", ".sass", ".css"} {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimPrefix(base, "_")
}

func (c *compiler) include(x *includeRule, o *out, e env) {
	m := e.lookupMixin(x.pos, x.ns, x.name)
	if c.depth >= maxIncludeDepth {
		x.fail("@include nesting deeper than %d (recursive mixin?)", maxIncludeDepth)
	}
	c.depth++
	defer func() { c.depth-- }()

	sc := newScope(m.closure.scope)
	var positional []value
	named := map[string]value{}
	var order []string
	for _, a := range x.args {
		v := c.eval(a.x, e)
		if a.name == "" {
			if len(named) > 0 {
				x.fail("positional arguments must come before keyword arguments")
			}
			positional = append(positional, v)
			continue
		}
		if _, dup := named[a.name]; dup {
			x.fail("duplicate argument $%s", a.name)
		}
		named[a.name] = v
		order = append(order, a.name)
	}
	params := m.rule.params
	if len(positional) > len(params) {
		x.fail("only %d argument(s) allowed, but %d were passed", len(params), len(positional))
	}
	for i, p := range params {
		v, byName := named[p.name]
		switch {
		case i < len(positional):
			if byName {
				x.fail("argument $%s was passed both by position and by name", p.name)
			}
			v = positional[i]
		case byName:
		case p.def != nil:
			v = stripSlash(c.eval(p.def, env{scope: sc, file: m.closure.file}))
		default:
			x.fail("missing argument $%s", p.name)
		}
		delete(named, p.name)
		sc.vars[p.name] = v
	}
	for _, name := range order {
		if _, left := named[name]; left {
			x.fail("no argument named $%s", name)
		}
	}
	var cont *content
	if x.hasContent {
		cont = &content{body: x.content, env: e}
	}
	c.exec(m.rule.body, o, env{scope: sc, file: m.closure.file, content: cont})
}

func (c *compiler) read(at pos, p string) *source {
	text, err := os.ReadFile(p) //nolint:gosec // loading the user's own stylesheets is the job
	if err != nil {
		at.fail("cannot read %s: %v", p, err)
	}
	return newSource(p, string(text))
}

// resolve finds an import URL the way grass does: relative to the
// importing file, then each load path; in each, name.scss, _name.scss,
// name.css, _name.css, then the same for name/index.
func (c *compiler) resolve(at pos, url, fromDir string) string {
	for _, dir := range append([]string{fromDir}, c.loadPaths...) {
		for _, cand := range candidates(filepath.Join(dir, filepath.FromSlash(url))) {
			info, err := os.Stat(cand)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if strings.HasSuffix(cand, ".sass") {
				at.unsupported("the indented syntax (" + filepath.Base(cand) + ")")
			}
			return filepath.Clean(cand)
		}
	}
	at.fail("can't find stylesheet to import: %q", url)
	return ""
}

func candidates(base string) []string {
	partial := func(p string) string { return filepath.Join(filepath.Dir(p), "_"+filepath.Base(p)) }
	switch filepath.Ext(base) {
	case ".scss", ".sass", ".css":
		return []string{base, partial(base)}
	}
	var out []string
	for _, b := range []string{base, filepath.Join(base, "index")} {
		for _, ext := range []string{".scss", ".sass", ".css"} {
			out = append(out, b+ext, partial(b)+ext)
		}
	}
	return out
}
