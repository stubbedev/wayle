// Package cli is wayle's declarative command tree: a port of the clap
// subset the Rust CLI (wayle/src/cli) uses, so the Go binary keeps its
// help text, flags, error messages, exit codes, and shell completions
// byte-for-byte.
//
// A tree is plain data: Commands holding Args and child Commands, each
// leaf naming a Run handler. Parsing, help, usage, errors, and the
// completion scripts all derive from that one declaration; nothing is
// restated per subcommand. The rendering ports clap_builder 4.5
// (output/help_template.rs, output/usage.rs, error/format.rs,
// parser/parser.rs) and clap_complete 4.6 (aot/shells).
package cli

import "strings"

// Command is one node of the tree (a clap Command). The zero value of
// every optional field is clap's default.
type Command struct {
	// Name is the subcommand token; for the root it is the binary name.
	Name string
	// About is the one-line description shown in parent listings and
	// in short (-h) help.
	About string
	// LongAbout replaces About in --help when set (a multi-paragraph
	// doc comment in clap derive).
	LongAbout string
	// AfterLongHelp is appended to --help only.
	AfterLongHelp string
	// Version, on the root, adds -V/--version printing "<name> <version>".
	Version string
	// Args are the options, flags, and positionals in declaration order.
	Args []*Arg
	// Subcommands are the children in declaration order.
	Subcommands []*Command
	// SubcommandOptional lets a command with subcommands run without
	// one (clap derive's Option<Subcommand>). Otherwise a subcommand is
	// required and a bare invocation prints help to stderr, exit 2.
	SubcommandOptional bool
	// AllowHyphenValues lets every value-taking arg accept values that
	// start with '-' (clap's Command::allow_hyphen_values).
	AllowHyphenValues bool
	// DisableHelpFlag drops the generated -h/--help.
	DisableHelpFlag bool
	// Run executes the command. Commands with required subcommands have
	// none; the matched leaf's Run is called.
	Run func(*Matches) error
}

// Arg is one argument (a clap Arg). An arg with neither Short nor
// Long is positional. A named arg without a Value parser is a boolean
// flag (ArgAction::SetTrue); with one it takes a single value.
type Arg struct {
	// ID keys the parsed value in Matches; its upper-case form is the
	// default value name.
	ID    string
	Short rune
	Long  string
	// ValueName overrides the <PLACEHOLDER>.
	ValueName string
	// Help is the short help; LongHelp replaces it in --help.
	Help     string
	LongHelp string
	// Value parses (and so validates) the raw value. A positional
	// without one takes a String.
	Value ValueParser
	// Required makes the arg mandatory.
	Required bool
	// Multiple makes a positional collect every remaining value
	// (a clap derive Vec<T>).
	Multiple bool
	// Defaults are the values used when the arg is absent; a nil slice
	// means no default (an empty-string default is []string{""}).
	Defaults []string
	// TrailingVarArg makes the positional swallow everything after it,
	// flags included (clap's trailing_var_arg + allow_hyphen_values).
	TrailingVarArg bool
	// AllowHyphenValues lets this arg's values start with '-'.
	AllowHyphenValues bool
}

func (a *Arg) positional() bool { return a.Short == 0 && a.Long == "" }

func (a *Arg) valueName() string {
	if a.ValueName != "" {
		return a.ValueName
	}
	return strings.ToUpper(a.ID)
}

// action is clap's ArgAction for the kinds the tree uses.
type action int

const (
	actionSet     action = iota // one value (an option or a positional)
	actionAppend                // a multi-valued positional
	actionSetTrue               // a boolean flag
	actionHelp
	actionVersion
)

// arg is an Arg as built into a command: its action, its positional
// index, and the hyphen settings the command propagates.
type arg struct {
	*Arg
	action      action
	index       int // 1-based position, 0 for named args
	allowHyphen bool
	parser      ValueParser
}

func (a *arg) takesValue() bool { return a.action == actionSet || a.action == actionAppend }

func (a *arg) multiple() bool { return a.action == actionAppend }

// cmd is a Command as built (clap's _build_self): the generated help
// flag, version flag, and help subcommand added, positionals indexed.
type cmd struct {
	decl           *Command
	name           string
	about          string
	longAbout      string
	afterLongHelp  string
	version        string
	bin            string // the usage name, e.g. "wayle audio"
	args           []*arg
	subs           []*cmd
	parent         *cmd
	subRequired    bool
	elseHelp       bool // arg_required_else_help
	helpFlagOff    bool
	helpSubOff     bool
	longHelpExists bool
}

const helpAbout = "Print this message or the help of the given subcommand(s)"

// build turns a declaration into a cmd. expandHelp builds the help
// subcommand as a copy of the tree (the completion generators' view)
// instead of the [COMMAND]... positional the parser uses.
func build(decl *Command, parent *cmd, expandHelp bool) *cmd {
	c := &cmd{
		decl:          decl,
		name:          decl.Name,
		about:         decl.About,
		longAbout:     decl.LongAbout,
		afterLongHelp: decl.AfterLongHelp,
		parent:        parent,
		helpFlagOff:   decl.DisableHelpFlag,
	}
	c.bin = decl.Name
	if parent != nil {
		c.bin = parent.bin + " " + decl.Name
	} else {
		c.version = decl.Version
	}
	for _, a := range decl.Args {
		c.args = append(c.args, buildArg(a, decl.AllowHyphenValues))
	}
	c.longHelpExists = c.computeLongHelpExists()
	if !c.helpFlagOff {
		help := &Arg{ID: "help", Short: 'h', Long: "help", Help: "Print help"}
		if c.longHelpExists {
			help.Help = "Print help (see more with '--help')"
			help.LongHelp = "Print help (see a summary with '-h')"
		}
		c.args = append(c.args, &arg{Arg: help, action: actionHelp})
	}
	if c.version != "" {
		version := &Arg{ID: "version", Short: 'V', Long: "version", Help: "Print version"}
		c.args = append(c.args, &arg{Arg: version, action: actionVersion})
	}
	index := 1
	for _, a := range c.args {
		if a.positional() {
			a.index = index
			index++
		}
	}
	if len(decl.Subcommands) > 0 {
		c.subRequired = !decl.SubcommandOptional
		c.elseHelp = !decl.SubcommandOptional
		for _, sub := range decl.Subcommands {
			c.subs = append(c.subs, build(sub, c, expandHelp))
		}
		c.subs = append(c.subs, c.helpSubcommand(expandHelp))
	}
	return c
}

func buildArg(a *Arg, cmdHyphen bool) *arg {
	built := &arg{Arg: a, parser: a.Value}
	switch {
	case a.positional() && a.Multiple:
		built.action = actionAppend
	case a.positional() || a.Value != nil:
		built.action = actionSet
	default:
		built.action = actionSetTrue
	}
	if built.parser == nil && built.takesValue() {
		built.parser = String
	}
	built.allowHyphen = built.takesValue() && (cmdHyphen || a.AllowHyphenValues || a.TrailingVarArg)
	return built
}

// helpSubcommand is clap's generated `help` subcommand.
func (c *cmd) helpSubcommand(expandHelp bool) *cmd {
	h := &cmd{
		name:        "help",
		about:       helpAbout,
		bin:         c.bin + " help",
		parent:      c,
		helpFlagOff: true,
		helpSubOff:  true,
	}
	if !expandHelp {
		h.args = []*arg{{
			Arg: &Arg{
				ID: "subcommand", ValueName: "COMMAND", Multiple: true,
				Help: "Print help for the subcommand(s)",
			},
			action: actionAppend,
			index:  1,
			parser: String,
		}}
		return h
	}
	for _, sub := range c.subs {
		h.subs = append(h.subs, copyForHelp(sub, h))
	}
	h.subs = append(h.subs, &cmd{
		name: "help", about: helpAbout, bin: h.bin + " help", parent: h,
		helpFlagOff: true, helpSubOff: true,
	})
	return h
}

// copyForHelp is clap's _copy_subtree_for_help: names and abouts only.
func copyForHelp(src, parent *cmd) *cmd {
	c := &cmd{
		name: src.name, about: src.about, bin: parent.bin + " " + src.name,
		parent: parent, helpFlagOff: true, helpSubOff: true,
	}
	for _, sub := range src.subs {
		if sub.name == "help" && sub.decl == nil {
			continue
		}
		c.subs = append(c.subs, copyForHelp(sub, c))
	}
	return c
}

// computeLongHelpExists is clap's long_help_exists_: --help differs
// from -h when there is a long about, an after-long-help, an arg long
// help, or possible values with help text.
func (c *cmd) computeLongHelpExists() bool {
	if c.longAbout != "" || c.afterLongHelp != "" {
		return true
	}
	for _, a := range c.args {
		if a.LongHelp != "" || pvsHaveHelp(a.possibleValues()) {
			return true
		}
	}
	return false
}

func (a *arg) possibleValues() []PossibleValue {
	if a.parser == nil {
		return nil
	}
	return a.parser.possibleValues()
}

func pvsHaveHelp(pvs []PossibleValue) bool {
	for _, pv := range pvs {
		if pv.Help != "" {
			return true
		}
	}
	return false
}

func (c *cmd) findSub(name string) *cmd {
	for _, s := range c.subs {
		if s.name == name {
			return s
		}
	}
	return nil
}

func (c *cmd) findLong(long string) *arg {
	for _, a := range c.args {
		if a.Long == long {
			return a
		}
	}
	return nil
}

func (c *cmd) findShort(short rune) *arg {
	for _, a := range c.args {
		if a.Short == short {
			return a
		}
	}
	return nil
}

func (c *cmd) positionalAt(index int) *arg {
	for _, a := range c.args {
		if a.index == index {
			return a
		}
	}
	return nil
}

func (c *cmd) hasPositionals() bool {
	for _, a := range c.args {
		if a.index > 0 {
			return true
		}
	}
	return false
}

func (c *cmd) subNames() []string {
	names := make([]string, len(c.subs))
	for i, s := range c.subs {
		names[i] = s.name
	}
	return names
}

func (c *cmd) longs() []string {
	var longs []string
	for _, a := range c.args {
		if a.Long != "" {
			longs = append(longs, a.Long)
		}
	}
	return longs
}
