package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Matches holds one command's parsed values (clap's ArgMatches).
type Matches struct {
	values  map[string][]any
	present map[string]bool // set on the command line (not a default)
	order   []*arg          // the present args in command-line order
	sub     *Matches
	subName string
	cmd     *cmd
	stdout  io.Writer
	stderr  io.Writer
}

// Stdout is where the handler prints its output.
func (m *Matches) Stdout() io.Writer {
	if m.stdout == nil {
		return os.Stdout
	}
	return m.stdout
}

// Stderr is where the handler prints diagnostics it reports itself
// (a returned error is printed by Run as "Error: ...").
func (m *Matches) Stderr() io.Writer {
	if m.stderr == nil {
		return os.Stderr
	}
	return m.stderr
}

func newMatches(c *cmd) *Matches {
	return &Matches{values: map[string][]any{}, present: map[string]bool{}, cmd: c}
}

func (m *Matches) markPresent(a *arg) {
	if !m.present[a.ID] {
		m.present[a.ID] = true
		m.order = append(m.order, a)
	}
}

// Flag reports a boolean flag.
func (m *Matches) Flag(id string) bool {
	v, ok := Value[bool](m, id)
	return ok && v
}

// Subcommand names the matched subcommand, "" when none was given.
func (m *Matches) Subcommand() string { return m.subName }

// Value returns an arg's single parsed value (its default when absent
// and it has one). It panics when T is not the arg's parsed type: that
// is a declaration bug, caught by the tree tests.
func Value[T any](m *Matches, id string) (T, bool) {
	var zero T
	vals := m.values[id]
	if len(vals) == 0 {
		return zero, false
	}
	v, ok := vals[0].(T)
	if !ok {
		panic(fmt.Sprintf("cli: arg %q holds %T, not %T", id, vals[0], zero))
	}
	return v, true
}

// Values returns every parsed value of a multi-valued arg.
func Values[T any](m *Matches, id string) []T {
	vals := m.values[id]
	out := make([]T, len(vals))
	for i, v := range vals {
		t, ok := v.(T)
		if !ok {
			panic(fmt.Sprintf("cli: arg %q holds %T, not %T", id, v, t))
		}
		out[i] = t
	}
	return out
}

// identifier is how an arg was named on the command line.
type identifier int

const (
	identLong identifier = iota
	identShort
	identIndex
)

type pendingArg struct {
	arg   *arg
	ident identifier
	raws  []string
}

type parseState int

const (
	stateDone parseState = iota
	stateOpt
	statePos
)

// parser is clap's Parser for one command level.
type parser struct {
	c       *cmd
	m       *Matches
	pending *pendingArg
}

// parse is Parser::get_matches_with: parse the level (recursing into
// a subcommand), resolve the pending value, add defaults, validate.
func parse(c *cmd, args []string) (*Matches, *Error) {
	p := &parser{c: c, m: newMatches(c)}
	if err := p.parseArgs(args); err != nil {
		return nil, err
	}
	if err := p.resolvePending(); err != nil {
		return nil, err
	}
	if err := p.addDefaults(); err != nil {
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p.m, nil
}

func (p *parser) parseArgs(args []string) *Error {
	state, stateArg := stateDone, (*arg)(nil)
	posCounter := 1
	trailing := false
	for i, raw := range args {
		if !trailing {
			if state != stateOpt && state != statePos {
				if sc := p.c.findSub(raw); sc != nil {
					if sc.name == "help" && !p.c.helpSubOff && sc.decl == nil {
						return p.parseHelpSubcommand(args[i+1:])
					}
					if err := p.resolvePending(); err != nil {
						return err
					}
					sub, err := parse(sc, args[i+1:])
					if err != nil {
						return err
					}
					p.m.sub, p.m.subName = sub, sc.name
					return nil
				}
			}
			switch {
			case raw == "--":
				if (state == stateOpt || state == statePos) && stateArg.allowHyphen {
					break // a hyphen value
				}
				trailing = true
				continue
			case strings.HasPrefix(raw, "--"):
				res, err := p.parseLong(raw[2:], state, stateArg, posCounter)
				if err != nil {
					return err
				}
				switch res.kind {
				case resultDone:
					state = stateDone
					continue
				case resultOpt:
					state, stateArg = stateOpt, res.arg
					continue
				case resultNoMatch:
					return p.didYouMeanError(res.name, trailing)
				}
			case strings.HasPrefix(raw, "-") && raw != "-":
				res, err := p.parseShort(raw[1:], state, stateArg, posCounter)
				if err != nil {
					return err
				}
				switch res.kind {
				case resultDone:
					state = stateDone
					continue
				case resultOpt:
					state, stateArg = stateOpt, res.arg
					continue
				case resultNoMatch:
					_ = p.resolvePending()
					return unknownArgumentError(p.c, res.name, "", !trailing && p.c.hasPositionals(), usageWithTitle(p.c, nil))
				}
			}
			if state == stateOpt {
				p.pending.raws = append(p.pending.raws, raw)
				state = stateDone // every option takes exactly one value
				continue
			}
		}

		pos := p.c.positionalAt(posCounter)
		if pos == nil {
			_ = p.resolvePending()
			return p.matchArgError(raw, trailing)
		}
		if pos.TrailingVarArg {
			trailing = true
		}
		if p.pending == nil || p.pending.arg != pos || !pos.multiple() {
			if err := p.resolvePending(); err != nil {
				return err
			}
		}
		if p.pending == nil {
			p.pending = &pendingArg{arg: pos, ident: identIndex}
		}
		p.pending.raws = append(p.pending.raws, raw)
		if pos.multiple() {
			state, stateArg = statePos, pos
		} else {
			posCounter++
			state = stateDone
		}
	}
	return nil
}

type resultKind int

const (
	resultDone resultKind = iota
	resultOpt
	resultNoMatch
	resultHyphenValue
)

type parseResult struct {
	kind resultKind
	arg  *arg
	name string // the unmatched flag, for NoMatchingArg
}

func (p *parser) hyphenPending(state parseState, stateArg *arg) bool {
	return (state == stateOpt || state == statePos) && stateArg.allowHyphen
}

func (p *parser) parseLong(body string, state parseState, stateArg *arg, posCounter int) (parseResult, *Error) {
	if p.hyphenPending(state, stateArg) {
		return parseResult{kind: resultHyphenValue}, nil
	}
	name, value, hasEq := strings.Cut(body, "=")
	a := p.c.findLong(name)
	if a == nil {
		if pos := p.c.positionalAt(posCounter); pos != nil && pos.allowHyphen {
			return parseResult{kind: resultHyphenValue}, nil
		}
		return parseResult{kind: resultNoMatch, name: name}, nil
	}
	if a.takesValue() {
		return p.parseOptValue(identLong, a, value, hasEq)
	}
	if hasEq {
		_ = p.resolvePending()
		used := []*arg{}
		for _, other := range p.m.order {
			if !other.Required {
				used = append(used, other)
			}
		}
		used = append(used, a)
		return parseResult{}, tooManyValuesError(p.c, value, a.String(), usageWithTitle(p.c, used))
	}
	return parseResult{kind: resultDone}, p.react(identLong, a, nil)
}

func (p *parser) parseShort(body string, state parseState, stateArg *arg, posCounter int) (parseResult, *Error) {
	if p.hyphenPending(state, stateArg) {
		return parseResult{kind: resultHyphenValue}, nil
	}
	if pos := p.c.positionalAt(posCounter); pos != nil && pos.allowHyphen {
		for _, r := range body {
			if p.c.findShort(r) == nil {
				return parseResult{kind: resultHyphenValue}, nil
			}
		}
	}
	ret := parseResult{kind: resultDone}
	for i, r := range body {
		a := p.c.findShort(r)
		if a == nil {
			return parseResult{kind: resultNoMatch, name: "-" + string(r)}, nil
		}
		if !a.takesValue() {
			if err := p.react(identShort, a, nil); err != nil {
				return parseResult{}, err
			}
			continue
		}
		rest := body[i+len(string(r)):]
		value, hasEq := rest, false
		if after, ok := strings.CutPrefix(rest, "="); ok {
			value, hasEq = after, true
		}
		return p.parseOptValue(identShort, a, value, hasEq || rest != "")
	}
	return ret, nil
}

// parseOptValue is Parser::parse_opt_value: an attached value
// completes the option; otherwise the next argument is its value.
func (p *parser) parseOptValue(ident identifier, a *arg, value string, attached bool) (parseResult, *Error) {
	if attached {
		return parseResult{kind: resultDone}, p.react(ident, a, []string{value})
	}
	if err := p.resolvePending(); err != nil {
		return parseResult{}, err
	}
	p.pending = &pendingArg{arg: a, ident: ident}
	return parseResult{kind: resultOpt, arg: a}, nil
}

func (p *parser) resolvePending() *Error {
	pending := p.pending
	if pending == nil {
		return nil
	}
	p.pending = nil
	return p.react(pending.ident, pending.arg, pending.raws)
}

// react is Parser::react for the actions the tree uses.
func (p *parser) react(ident identifier, a *arg, raws []string) *Error {
	if err := p.resolvePending(); err != nil {
		return err
	}
	switch a.action {
	case actionHelp:
		return displayError(kindDisplayHelp, renderHelp(p.c, ident != identShort))
	case actionVersion:
		return displayError(kindDisplayVersion, p.c.bin+" "+p.c.version+"\n")
	case actionSetTrue:
		if p.m.present[a.ID] {
			return conflictSelfError(p.c, a.String(), usageWithTitle(p.c, nil))
		}
		p.m.markPresent(a)
		p.m.values[a.ID] = []any{true}
		return nil
	}
	if a.takesValue() && len(raws) == 0 {
		return invalidValueError(p.c, "", pvNames(a.possibleValues()), a.String())
	}
	if a.action == actionSet && p.m.present[a.ID] {
		return conflictSelfError(p.c, a.String(), usageWithTitle(p.c, nil))
	}
	p.m.markPresent(a)
	for _, raw := range raws {
		v, err := a.parser.parse(p.c, a, raw)
		if err != nil {
			if err.helpFlag == "" {
				err.helpFlag = helpFlagFor(p.c)
			}
			return err
		}
		p.m.values[a.ID] = append(p.m.values[a.ID], v)
	}
	return nil
}

func pvNames(pvs []PossibleValue) []string {
	names := make([]string, len(pvs))
	for i, pv := range pvs {
		names[i] = pv.Name
	}
	return names
}

func (p *parser) addDefaults() *Error {
	for _, a := range p.c.args {
		if _, ok := p.m.values[a.ID]; ok || len(a.Defaults) == 0 {
			continue
		}
		for _, raw := range a.Defaults {
			v, err := a.parser.parse(p.c, a, raw)
			if err != nil {
				return err
			}
			p.m.values[a.ID] = append(p.m.values[a.ID], v)
		}
	}
	return nil
}

// validate is Validator::validate: a bare invocation of a command that
// needs a subcommand prints its help to stderr; a missing required arg
// is an error naming every missing one.
func (p *parser) validate() *Error {
	if p.m.sub == nil && p.c.elseHelp && len(p.m.order) == 0 {
		return displayError(kindDisplayHelpOnMissing, renderHelp(p.c, false))
	}
	if p.m.sub == nil && p.c.subRequired {
		return missingSubcommandError(p.c, usageWithTitle(p.c, nil))
	}
	var missing []*arg
	for _, a := range p.c.args {
		if a.Required && !p.m.present[a.ID] {
			missing = append(missing, a)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	yes := true
	names := make([]string, len(missing))
	for i, a := range missing {
		names[i] = strip(a.stylized(&yes))
	}
	used := append(append([]*arg{}, p.m.order...), missing...)
	return missingRequiredError(p.c, names, usageWithTitle(p.c, used))
}

// matchArgError is Parser::match_arg_error for a stray argument.
func (p *parser) matchArgError(raw string, trailing bool) *Error {
	flagLike := strings.HasPrefix(raw, "-") && raw != "-"
	trailingTip := !trailing && p.c.hasPositionals() && flagLike
	if len(p.c.subs) > 0 {
		if candidates := didYouMean(raw, p.c.subNames()); len(candidates) > 0 {
			return invalidSubcommandError(p.c, raw, candidates, usageWithTitle(p.c, nil))
		}
		if !p.c.hasPositionals() {
			return invalidSubcommandError(p.c, raw, nil, usageWithTitle(p.c, nil))
		}
	}
	return unknownArgumentError(p.c, raw, "", trailingTip, usageWithTitle(p.c, nil))
}

// didYouMeanError is Parser::did_you_mean_error for an unknown --long.
func (p *parser) didYouMeanError(name string, trailing bool) *Error {
	_ = p.resolvePending()
	suggestion := ""
	var suggested *arg
	if s := didYouMean(name, p.c.longs()); len(s) > 0 {
		suggestion = "--" + s[len(s)-1]
		suggested = p.c.findLong(s[len(s)-1])
	}
	used := append([]*arg{}, p.m.order...)
	if suggested != nil && !p.m.present[suggested.ID] {
		used = append(used, suggested)
	}
	trailingTip := false
	if !trailing && p.c.hasPositionals() {
		trailingTip = suggested == nil
		for _, a := range p.c.args {
			if a.TrailingVarArg {
				trailingTip = true
			}
		}
	}
	return unknownArgumentError(p.c, "--"+name, suggestion, trailingTip, usageWithTitle(p.c, used))
}

// parseHelpSubcommand is Parser::parse_help_subcommand: walk the named
// path and print that command's long help.
func (p *parser) parseHelpSubcommand(path []string) *Error {
	target := p.c
	for _, name := range path {
		next := target.findSub(name)
		if next == nil {
			return invalidSubcommandError(target, name, nil, usageWithTitle(target, nil))
		}
		target = next
	}
	return displayError(kindDisplayHelp, renderHelp(target, true))
}
