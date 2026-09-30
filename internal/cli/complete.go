package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// CompletionShells are clap_complete's Shell values, the accepted
// arguments of a completions command.
var CompletionShells = []PossibleValue{
	{Name: "bash"}, {Name: "elvish"}, {Name: "fish"}, {Name: "powershell"}, {Name: "zsh"},
}

// WriteCompletions writes clap_complete's script for shell, generated
// from root (clap_complete::generate with the root name as bin name).
func WriteCompletions(w io.Writer, root *Command, shell string) error {
	c := build(root, nil, true)
	var script string
	switch shell {
	case "bash":
		script = bashScript(c)
	case "elvish":
		script = elvishScript(c)
	case "fish":
		script = fishScript(c)
	case "powershell":
		script = powershellScript(c)
	case "zsh":
		script = zshScript(c)
	default:
		return fmt.Errorf("unknown shell %q", shell)
	}
	_, err := io.WriteString(w, script)
	return err
}

// valueHint is the subset of clap's ValueHint the tree produces: a
// PathBuf arg completes files (AnyPath), everything else is Unknown.
type valueHint int

const (
	hintUnknown valueHint = iota
	hintAnyPath
)

func (a *arg) hint() valueHint {
	if a.takesValue() && a.parser == Path {
		return hintAnyPath
	}
	return hintUnknown
}

// opts are the named args taking a value (Command::get_opts).
func (c *cmd) opts() []*arg {
	var out []*arg
	for _, a := range c.args {
		if !a.positional() && a.takesValue() {
			out = append(out, a)
		}
	}
	return out
}

// flags are the named args taking no value (utils::flags).
func (c *cmd) flags() []*arg {
	var out []*arg
	for _, a := range c.args {
		if !a.positional() && !a.takesValue() {
			out = append(out, a)
		}
	}
	return out
}

func (c *cmd) positionals() []*arg {
	var out []*arg
	for _, a := range c.args {
		if a.positional() {
			out = append(out, a)
		}
	}
	return out
}

// allSubcommands is utils::all_subcommands: the children, then every
// descendant, as (name, bin name) pairs.
func allSubcommands(c *cmd) []*cmd {
	out := append([]*cmd{}, c.subs...)
	for _, s := range c.subs {
		out = append(out, allSubcommands(s)...)
	}
	return out
}

// findByBin is zsh.rs's parser_of.
func findByBin(c *cmd, bin string) *cmd {
	if c.bin == bin {
		return c
	}
	for _, s := range c.subs {
		if found := findByBin(s, bin); found != nil {
			return found
		}
	}
	return nil
}

func sortedUnique(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// ---- bash (aot/shells/bash.rs) ----

func bashScript(c *cmd) string {
	name := c.bin
	fn := strings.ReplaceAll(name, "-", "__")
	var b strings.Builder
	b.WriteString("_" + name + `() {
    local i cur prev opts cmd
    COMPREPLY=()
    if [[ "${BASH_VERSINFO[0]}" -ge 4 ]]; then
        cur="$2"
    else
        cur="${COMP_WORDS[COMP_CWORD]}"
    fi
    prev="$3"
    cmd=""
    opts=""

    for i in "${COMP_WORDS[@]:0:COMP_CWORD}"
    do
        case "${cmd},${i}" in
            ",$1")
                cmd="` + fn + `"
                ;;` + bashSubcommandCases(c, fn) + `
            *)
                ;;
        esac
    done

    case "${cmd}" in
        ` + fn + `)
            opts="` + bashOptions(c) + `"
            if [[ ${cur} == -* || ${COMP_CWORD} -eq 1 ]] ; then
                COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
                return 0
            fi
            case "${prev}" in` + bashOptionDetails(c) + `
                *)
                    COMPREPLY=()
                    ;;
            esac
            COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
            return 0
            ;;` + bashSubcommandDetails(c) + `
    esac
}

if [[ "${BASH_VERSINFO[0]}" -eq 4 && "${BASH_VERSINFO[1]}" -ge 4 || "${BASH_VERSINFO[0]}" -gt 4 ]]; then
    complete -F _` + name + ` -o nosort -o bashdefault -o default ` + name + `
else
    complete -F _` + name + ` -o bashdefault -o default ` + name + `
fi
`)
	return b.String()
}

func bashSubcommandCases(c *cmd, parentFn string) string {
	type entry struct{ parent, name, fn string }
	var entries []entry
	var add func(parent string, s *cmd)
	add = func(parent string, s *cmd) {
		fn := parent + "__" + strings.ReplaceAll(s.name, "-", "__")
		entries = append(entries, entry{parent, s.name, fn})
		for _, sub := range s.subs {
			add(fn, sub)
		}
	}
	for _, s := range c.subs {
		add(parentFn, s)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.parent != b.parent {
			return a.parent < b.parent
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.fn < b.fn
	})
	cases := []string{""}
	for _, e := range entries {
		cases = append(cases, e.parent+","+e.name+")\n                cmd=\""+e.fn+"\"\n                ;;")
	}
	return strings.Join(cases, "\n            ")
}

func bashSubcommandDetails(root *cmd) string {
	var paths []string
	for _, s := range allSubcommands(root) {
		paths = append(paths, strings.ReplaceAll(s.bin, " ", "__"))
	}
	paths = sortedUnique(paths)
	dets := []string{""}
	for _, path := range paths {
		sub := bashFind(root, path)
		level := len(strings.Split(path, "__"))
		dets = append(dets, strings.ReplaceAll(path, "-", "__")+`)
            opts="`+bashOptions(sub)+`"
            if [[ ${cur} == -* || ${COMP_CWORD} -eq `+strconv.Itoa(level)+` ]] ; then
                COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
                return 0
            fi
            case "${prev}" in`+bashOptionDetails(sub)+`
                *)
                    COMPREPLY=()
                    ;;
            esac
            COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
            return 0
            ;;`)
	}
	return strings.Join(dets, "\n        ")
}

// bashFind is utils::find_subcommand_with_path over a "__" path whose
// first element is the binary.
func bashFind(root *cmd, path string) *cmd {
	c := root
	for _, name := range strings.Split(path, "__")[1:] {
		c = c.findSub(name)
	}
	return c
}

func bashOptions(c *cmd) string {
	var opts []string
	for _, a := range c.args {
		if !a.positional() && a.Short != 0 {
			opts = append(opts, "-"+string(a.Short))
		}
	}
	for _, a := range c.args {
		if !a.positional() && a.Long != "" {
			opts = append(opts, "--"+a.Long)
		}
	}
	for _, a := range c.positionals() {
		if pvs := a.possibleValues(); len(pvs) > 0 {
			opts = append(opts, pvNames(pvs)...)
		} else {
			opts = append(opts, a.String())
		}
	}
	opts = append(opts, c.subNames()...)
	return strings.Join(opts, " ")
}

func bashOptionDetails(c *cmd) string {
	opts := []string{""}
	for _, o := range c.opts() {
		reply := "COMPREPLY=(" + bashValues(o) + ")"
		entry := func(flag string) string {
			return strings.Join([]string{flag + ")", reply, "return 0", ";;"}, "\n                    ")
		}
		if o.Long != "" {
			opts = append(opts, entry("--"+o.Long))
		}
		if o.Short != 0 {
			opts = append(opts, entry("-"+string(o.Short)))
		}
	}
	return strings.Join(opts, "\n                ")
}

func bashValues(o *arg) string {
	if pvs := o.possibleValues(); len(pvs) > 0 {
		return `$(compgen -W "` + strings.Join(pvNames(pvs), " ") + `" -- "${cur}")`
	}
	return `$(compgen -f "${cur}")`
}

// ---- fish (aot/shells/fish.rs) ----

func fishEscape(s string, comma bool) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	if comma {
		s = strings.ReplaceAll(s, ",", `\,`)
	}
	return s
}

func fishHelp(s string) string { return fishEscape(strings.ReplaceAll(strip(s), "\n", " "), false) }

func fishScript(c *cmd) string {
	name := strings.ReplaceAll(c.bin, "-", "_")
	needs := "__fish_" + name + "_needs_command"
	using := "__fish_" + name + "_using_subcommand"
	var b strings.Builder
	if len(c.subs) > 0 {
		fishHelpers(&b, name, c, needs, using)
	} else {
		needs, using = "__fish_use_subcommand", "__fish_seen_subcommand_from"
	}
	fishInner(&b, c.bin, nil, c, needs, using)
	return b.String()
}

func fishHelpers(b *strings.Builder, name string, c *cmd, needs, using string) {
	var specs strings.Builder
	for _, a := range c.args {
		if a.positional() {
			continue
		}
		specs.WriteString(" ")
		if a.Short != 0 {
			specs.WriteRune(a.Short)
		}
		if a.Long != "" {
			if a.Short != 0 {
				specs.WriteString("/")
			}
			specs.WriteString(fishEscape(a.Long, false))
		}
		if a.takesValue() {
			specs.WriteString("=")
		}
	}
	optspecs := "__fish_" + name + "_global_optspecs"
	b.WriteString("# Print an optspec for argparse to handle cmd's options that are independent of any subcommand.\n" +
		"function " + optspecs + "\n" +
		"\tstring join \\n" + specs.String() + "\n" +
		"end\n\n" +
		"function " + needs + "\n" +
		"\t# Figure out if the current invocation already has a command.\n" +
		"\tset -l cmd (commandline -opc)\n" +
		"\tset -e cmd[1]\n" +
		"\targparse -s (" + optspecs + ") -- $cmd 2>/dev/null\n" +
		"\tor return\n" +
		"\tif set -q argv[1]\n" +
		"\t\t# Also print the command, so this can be used to figure out what it is.\n" +
		"\t\techo $argv[1]\n" +
		"\t\treturn 1\n" +
		"\tend\n" +
		"\treturn 0\n" +
		"end\n\n" +
		"function " + using + "\n" +
		"\tset -l cmd (" + needs + ")\n" +
		"\ttest -z \"$cmd\"\n" +
		"\tand return 1\n" +
		"\tcontains -- $cmd[1] $argv\n" +
		"end\n\n")
}

func fishInner(b *strings.Builder, root string, parents []string, c *cmd, needs, using string) {
	template := "complete -c " + root
	switch len(parents) {
	case 0:
		if len(c.subs) > 0 {
			template += " -n \"" + needs + "\""
		}
	case 1:
		out := using + " " + parents[0]
		if len(c.subs) > 0 {
			out += "; and not __fish_seen_subcommand_from " + strings.Join(c.subNames(), " ")
		}
		template += " -n \"" + out + "\""
	case 2:
		template += " -n \"" + using + " " + parents[0] + "; and __fish_seen_subcommand_from " + parents[1] + "\""
	default:
		return
	}
	line := func(a *arg, extra string) {
		t := template
		if a.Short != 0 {
			t += " -s " + string(a.Short)
		}
		if a.Long != "" {
			t += " -l " + fishEscape(a.Long, false)
		}
		if a.Help != "" {
			t += " -d '" + fishHelp(a.Help) + "'"
		}
		b.WriteString(t + extra + "\n")
	}
	for _, o := range c.opts() {
		line(o, fishValueCompletion(o))
	}
	for _, f := range c.flags() {
		line(f, "")
	}
	if len(c.positionals()) == 0 {
		template += " -f"
	}
	for _, s := range c.subs {
		t := template + " -a \"" + s.name + "\""
		if s.about != "" {
			t += " -d '" + fishHelp(s.about) + "'"
		}
		b.WriteString(t + "\n")
	}
	for _, s := range c.subs {
		fishInner(b, root, append(append([]string{}, parents...), s.name), s, needs, using)
	}
}

func fishValueCompletion(o *arg) string {
	if pvs := o.possibleValues(); len(pvs) > 0 {
		vals := make([]string, len(pvs))
		for i, pv := range pvs {
			vals[i] = fishEscape(pv.Name, true) + `\t'` + fishHelp(pv.Help) + "'"
		}
		return ` -r -f -a "` + strings.Join(vals, "\n") + `"`
	}
	if o.hint() == hintAnyPath {
		return " -r -F"
	}
	return " -r"
}

// ---- zsh (aot/shells/zsh.rs) ----

func zshScript(c *cmd) string {
	name := c.bin
	return `#compdef ` + name + `

autoload -U is-at-least

_` + name + `() {
    typeset -A opt_args
    typeset -a _arguments_options
    local ret=1

    if is-at-least 5.2; then
        _arguments_options=(-s -S -C)
    else
        _arguments_options=(-s -C)
    fi

    local context curcontext="$curcontext" state line
    ` + zshArgs(c) + zshSubcommands(c) + `
}

` + zshSubcommandDetails(c) + `

if [ "$funcstack[1]" = "_` + name + `" ]; then
    _` + name + ` "$@"
else
    compdef _` + name + ` ` + name + `
fi
`
}

func zshCommandsFn(c *cmd) string {
	return `(( $+functions[_` + strings.ReplaceAll(c.bin, " ", "__") + `_commands] )) ||
_` + strings.ReplaceAll(c.bin, " ", "__") + `_commands() {
    local commands; commands=(` + zshSubcommandsOf(c) + `)
    _describe -t commands '` + c.bin + ` commands' commands "$@"
}`
}

func zshSubcommandDetails(root *cmd) string {
	ret := []string{zshCommandsFn(root)}
	var bins []string
	for _, s := range allSubcommands(root) {
		bins = append(bins, s.bin)
	}
	for _, bin := range sortedUnique(bins) {
		ret = append(ret, zshCommandsFn(findByBin(root, bin)))
	}
	return strings.Join(ret, "\n")
}

func zshSubcommandsOf(c *cmd) string {
	var segments []string
	for _, s := range c.subs {
		segments = append(segments, "'"+s.name+":"+zshEscapeHelp(s.about)+"' \\")
	}
	if len(segments) > 0 {
		segments = append([]string{""}, segments...)
		segments = append(segments, "    ")
	}
	return strings.Join(segments, "\n")
}

func zshSubcommands(parent *cmd) string {
	if len(parent.subs) == 0 {
		return ""
	}
	var all []string
	for _, s := range parent.subs {
		segments := []string{"(" + s.name + ")"}
		if args := zshArgs(s); args != "" {
			segments = append(segments, args)
		}
		if children := zshSubcommands(s); children != "" {
			segments = append(segments, children)
		}
		segments = append(segments, ";;")
		all = append(all, strings.Join(segments, "\n"))
	}
	pos := strconv.Itoa(len(parent.positionals()) + 1)
	return `
    case $state in
    (` + parent.name + `)
        words=($line[` + pos + `] "${words[@]}")
        (( CURRENT += 1 ))
        curcontext="${curcontext%:*:*}:` + strings.ReplaceAll(parent.bin, " ", "-") + `-command-$line[` + pos + `]:"
        case $line[` + pos + `] in
            ` + strings.Join(all, "\n") + `
        esac
    ;;
esac`
}

func zshArgs(c *cmd) string {
	segments := []string{`_arguments "${_arguments_options[@]}" : \`}
	if s := zshOpts(c); s != "" {
		segments = append(segments, s)
	}
	if s := zshFlags(c); s != "" {
		segments = append(segments, s)
	}
	if s := zshPositionals(c); s != "" {
		segments = append(segments, s)
	}
	if len(c.subs) > 0 {
		segments = append(segments, `":: :_`+strings.ReplaceAll(c.bin, " ", "__")+`_commands" \`)
		segments = append(segments, `"*::: :->`+c.name+`" \`)
	}
	segments = append(segments, "&& ret=0")
	return strings.Join(segments, "\n")
}

var zshHelpEscaper = strings.NewReplacer(
	`\`, `\\`, `'`, `'\''`, `[`, `\[`, `]`, `\]`, `:`, `\:`, `$`, `\$`, "`", "\\`", "\n", " ",
)

func zshEscapeHelp(s string) string { return zshHelpEscaper.Replace(strip(s)) }

var zshValueEscaper = strings.NewReplacer(
	`\`, `\\`, `'`, `'\''`, `[`, `\[`, `]`, `\]`, `:`, `\:`, `$`, `\$`, "`", "\\`", `(`, `\(`, `)`, `\)`, ` `, `\ `,
)

func zshValueCompletion(a *arg) string {
	if pvs := a.possibleValues(); len(pvs) > 0 {
		if pvsHaveHelp(pvs) {
			vals := make([]string, len(pvs))
			for i, pv := range pvs {
				vals[i] = zshValueEscaper.Replace(pv.Name) + `\:"` + zshEscapeHelp(pv.Help) + `"`
			}
			return "((" + strings.Join(vals, "\n") + "))"
		}
		return "(" + strings.Join(pvNames(pvs), " ") + ")"
	}
	if a.hint() == hintAnyPath {
		return "_files"
	}
	return "_default"
}

func zshOpts(c *cmd) string {
	var ret []string
	for _, o := range c.opts() {
		help := zshEscapeHelp(o.Help)
		vc := ":" + o.valueName() + ":" + zshValueCompletion(o)
		if o.Short != 0 {
			ret = append(ret, "'-"+string(o.Short)+"+["+help+"]"+vc+"' \\")
		}
		if o.Long != "" {
			ret = append(ret, "'--"+o.Long+"=["+help+"]"+vc+"' \\")
		}
	}
	return strings.Join(ret, "\n")
}

func zshFlags(c *cmd) string {
	var ret []string
	for _, f := range c.flags() {
		help := zshEscapeHelp(f.Help)
		if f.Short != 0 {
			ret = append(ret, "'-"+string(f.Short)+"["+help+"]' \\")
		}
		if f.Long != "" {
			ret = append(ret, "'--"+f.Long+"["+help+"]' \\")
		}
	}
	return strings.Join(ret, "\n")
}

var zshPositionalHelpEscaper = strings.NewReplacer(`[`, `\[`, `]`, `\]`, `'`, `'\''`, `:`, `\:`)

func zshPositionals(c *cmd) string {
	var ret []string
	catchAll := false
	for _, a := range c.positionals() {
		if catchAll && a.multiple() {
			continue
		}
		var cardinality string
		switch {
		case a.multiple() && len(c.subs) == 0:
			catchAll = true
			cardinality = "*:"
		case !a.Required:
			cardinality = ":"
		}
		help := ""
		if a.Help != "" {
			help = zshPositionalHelpEscaper.Replace(" -- " + strip(a.Help))
		}
		ret = append(ret, "'"+cardinality+":"+a.ID+help+":"+zshValueCompletion(a)+"' \\")
	}
	return strings.Join(ret, "\n")
}

// ---- elvish (aot/shells/elvish.rs) ----

func elvishScript(c *cmd) string {
	return `
use builtin;
use str;

set edit:completion:arg-completer[` + c.bin + `] = {|@words|
    fn spaces {|n|
        builtin:repeat $n ' ' | str:join ''
    }
    fn cand {|text desc|
        edit:complex-candidate $text &display=$text' '(spaces (- 14 (wcswidth $text)))$desc
    }
    var command = '` + c.bin + `'
    for word $words[1..-1] {
        if (str:has-prefix $word '-') {
            break
        }
        set command = $command';'$word
    }
    var completions = [` + elvishInner(c, "") + `
    ]
    $completions[$command]
}
`
}

func elvishHelp(help, fallback string) string {
	if help == "" {
		return fallback
	}
	return strings.ReplaceAll(strings.ReplaceAll(strip(help), "\n", " "), "'", "''")
}

func elvishInner(c *cmd, previous string) string {
	name := c.bin
	if previous != "" {
		name = previous + ";" + c.name
	}
	const preamble = "\n            cand "
	var completions strings.Builder
	named := append(c.opts(), c.flags()...)
	for _, a := range named {
		if a.Short != 0 {
			completions.WriteString(preamble + "-" + string(a.Short) + " '" + elvishHelp(a.Help, string(a.Short)) + "'")
		}
		if a.Long != "" {
			completions.WriteString(preamble + "--" + a.Long + " '" + elvishHelp(a.Help, a.Long) + "'")
		}
	}
	for _, s := range c.subs {
		completions.WriteString(preamble + s.name + " '" + elvishHelp(s.about, s.name) + "'")
	}
	var out strings.Builder
	out.WriteString("\n        &'" + name + "'= {" + completions.String() + "\n        }")
	for _, s := range c.subs {
		out.WriteString(elvishInner(s, name))
	}
	return out.String()
}

// ---- powershell (aot/shells/powershell.rs) ----

func powershellScript(c *cmd) string {
	return `
using namespace System.Management.Automation
using namespace System.Management.Automation.Language

Register-ArgumentCompleter -Native -CommandName '` + c.bin + `' -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $commandElements = $commandAst.CommandElements
    $command = @(
        '` + c.bin + `'
        for ($i = 1; $i -lt $commandElements.Count; $i++) {
            $element = $commandElements[$i]
            if ($element -isnot [StringConstantExpressionAst] -or
                $element.StringConstantType -ne [StringConstantType]::BareWord -or
                $element.Value.StartsWith('-') -or
                $element.Value -eq $wordToComplete) {
                break
        }
        $element.Value
    }) -join ';'

    $completions = @(switch ($command) {` + powershellInner(c, "") + `
    })

    $completions.Where{ $_.CompletionText -like "$wordToComplete*" } |
        Sort-Object -Property ListItemText
}
`
}

func powershellHelp(help, fallback string) string {
	help = strip(help)
	if help == "" {
		return fallback
	}
	help = strings.ReplaceAll(help, "\n", " ")
	return strings.ReplaceAll(strings.ReplaceAll(help, "'", "''"), "’", "'’")
}

func powershellInner(c *cmd, previous string) string {
	name := c.bin
	if previous != "" {
		name = previous + ";" + c.name
	}
	const preamble = "\n            [CompletionResult]::new("
	var completions strings.Builder
	named := append(c.opts(), c.flags()...)
	for _, a := range named {
		if a.Short != 0 {
			pad := ""
			if a.Short >= 'A' && a.Short <= 'Z' {
				pad = " "
			}
			fmt.Fprintf(&completions, "%s'-%c', '-%c%s', [CompletionResultType]::ParameterName, '%s')",
				preamble, a.Short, a.Short, pad, powershellHelp(a.Help, string(a.Short)))
		}
		if a.Long != "" {
			fmt.Fprintf(&completions, "%s'--%s', '--%s', [CompletionResultType]::ParameterName, '%s')",
				preamble, a.Long, a.Long, powershellHelp(a.Help, a.Long))
		}
	}
	for _, s := range c.subs {
		fmt.Fprintf(&completions, "%s'%s', '%s', [CompletionResultType]::ParameterValue, '%s')",
			preamble, s.name, s.name, powershellHelp(s.about, s.name))
	}
	var out strings.Builder
	out.WriteString("\n        '" + name + "' {" + completions.String() + "\n            break\n        }")
	for _, s := range c.subs {
		out.WriteString(powershellInner(s, name))
	}
	return out.String()
}
