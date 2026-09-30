package main

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/launcheripc"
)

func parseOK(t *testing.T, args ...string) launcherInvocation {
	t.Helper()
	inv, err := parseLauncherArgs(args, io.Discard)
	if err != nil {
		t.Fatalf("parse %q: %v", args, err)
	}
	return inv
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func TestShowModeAndPrompt(t *testing.T) {
	inv := parseOK(t, "-show", "drun", "-p", "apps")
	if deref(inv.options.Mode) != "drun" || deref(inv.options.Prompt) != "apps" {
		t.Errorf("options = %+v", inv.options)
	}
}

func TestDmenuFlags(t *testing.T) {
	inv := parseOK(t, "-dmenu", "-i", "-multi-select", "-password", "-mesg", "hello", "-format", "i")
	o := inv.options
	if !o.Dmenu || !o.MultiSelect || !o.Password || !deref(o.CaseInsensitive) || deref(o.Mesg) != "hello" || inv.format != "i" {
		t.Errorf("inv = %+v", inv)
	}
}

func TestKbAndDisplayPrefixes(t *testing.T) {
	inv := parseOK(t, "-kb-accept-entry", "Return", "-display-drun", "apps")
	if inv.options.KbOverrides["accept-entry"] != "Return" || inv.options.DisplayNames["drun"] != "apps" {
		t.Errorf("options = %+v", inv.options)
	}
}

func TestModesSplitOnCommaAndHash(t *testing.T) {
	inv := parseOK(t, "-modi", "drun,run#clip:~/bin/clip.sh")
	if !reflect.DeepEqual(inv.options.Modes, []string{"drun", "run", "clip:~/bin/clip.sh"}) {
		t.Errorf("modes = %q", inv.options.Modes)
	}
}

func TestIgnoredFlagsDoNotError(t *testing.T) {
	var stderr bytes.Buffer
	inv, err := parseLauncherArgs([]string{"-theme", "gruvbox", "-no-lazy-grab", "-show", "run"}, &stderr)
	if err != nil || deref(inv.options.Mode) != "run" {
		t.Fatalf("inv = %+v %v", inv, err)
	}
	if !strings.Contains(stderr.String(), "-theme is ignored") || !strings.Contains(stderr.String(), "-no-lazy-grab is not supported") {
		t.Errorf("warnings = %q", stderr.String())
	}
	stderr.Reset()
	_, _ = parseLauncherArgs([]string{"-frobnicate"}, &stderr)
	if !strings.Contains(stderr.String(), "unknown option -frobnicate (skipped)") {
		t.Errorf("unknown = %q", stderr.String())
	}
}

func TestPluginFlagsAreSwallowedWithTheirValue(t *testing.T) {
	if inv := parseOK(t, "-plugin-path", "/usr/lib/rofi", "-show", "drun"); deref(inv.options.Mode) != "drun" {
		t.Error("-plugin-path must consume its value")
	}
	if inv := parseOK(t, "-no-plugins", "-plugins", "-show", "run"); deref(inv.options.Mode) != "run" {
		t.Error("the bare plugin flags take no value")
	}
}

func TestWidthTakesAllThreeForms(t *testing.T) {
	for raw, want := range map[string]launcheripc.Width{
		"60":    {Unit: launcheripc.WidthPercent, Value: 60},
		"-30":   {Unit: launcheripc.WidthChars, Value: 30},
		"600px": {Unit: launcheripc.WidthPixels, Value: 600},
	} {
		if got := parseOK(t, "-width", raw).options.Width; got == nil || *got != want {
			t.Errorf("-width %s = %+v", raw, got)
		}
	}
	if parseOK(t, "-show", "window").options.Width != nil {
		t.Error("width is unset unless asked, so the configured one wins")
	}
	if _, err := parseLauncherArgs([]string{"-width", "wide"}, io.Discard); err == nil {
		t.Error("a non-numeric width is a usage error")
	}
}

func TestLinesAliasAndOffsets(t *testing.T) {
	inv := parseOK(t, "-lines", "5", "-xoffset", "20", "-yoffset", "-8")
	if deref(inv.options.Lines) != 5 || deref(inv.options.XOffset) != 20 || deref(inv.options.YOffset) != -8 {
		t.Errorf("options = %+v", inv.options)
	}
	if deref(parseOK(t, "-l", "5").options.Lines) != 5 {
		t.Error("-l is -lines")
	}
	if parseOK(t, "-l", "many").options.Lines != nil {
		t.Error("an unparseable number is no value, not an error")
	}
}

func TestCenteredOffsetsWarn(t *testing.T) {
	var stderr bytes.Buffer
	_, _ = parseLauncherArgs([]string{"-location", "0", "-xoffset", "4"}, &stderr)
	if !strings.Contains(stderr.String(), "-xoffset/-yoffset have no effect with -location 0") {
		t.Errorf("stderr = %q", stderr.String())
	}
	stderr.Reset()
	_, _ = parseLauncherArgs([]string{"-location", "3", "-xoffset", "4"}, &stderr)
	if stderr.Len() != 0 {
		t.Errorf("an edge location must not warn: %q", stderr.String())
	}
}

func TestHistoryAndIconFlagsReachTheSession(t *testing.T) {
	inv := parseOK(t, "-show", "drun", "-ignored-prefixes", "sudo ,tmp-", "-application-fallback-icon", "application-x-executable")
	if !reflect.DeepEqual(inv.options.IgnoredPrefixes, []string{"sudo", "tmp-"}) || deref(inv.options.ApplicationFallbackIcon) != "application-x-executable" {
		t.Errorf("options = %+v", inv.options)
	}
	plain := parseOK(t, "-show", "drun")
	if plain.options.IgnoredPrefixes != nil || plain.options.ApplicationFallbackIcon != nil {
		t.Error("absent unless asked")
	}
}

func TestEventHooksReachTheSession(t *testing.T) {
	inv := parseOK(t, "-show", "drun", "-on-selection-changed", "preview {entry}", "-on-entry-accepted", "log {input}",
		"-on-mode-changed", "mode {mode}", "-on-menu-canceled", "cleanup", "-on-menu-error", "report {error}")
	o := inv.options
	if deref(o.OnSelectionChanged) != "preview {entry}" || deref(o.OnEntryAccepted) != "log {input}" ||
		deref(o.OnModeChanged) != "mode {mode}" || deref(o.OnMenuCanceled) != "cleanup" || deref(o.OnMenuError) != "report {error}" {
		t.Errorf("hooks = %+v", o)
	}
	if deref(parseOK(t, "-on-menu-cancelled", "x").options.OnMenuCanceled) != "x" {
		t.Error("the British spelling is the same hook")
	}
	plain := parseOK(t, "-show", "drun")
	if plain.options.OnSelectionChanged != nil || plain.options.OnMenuError != nil {
		t.Error("an absent hook must not become an empty command")
	}
}

func TestCompleterModeConsumesItsValue(t *testing.T) {
	inv := parseOK(t, "-completer-mode", "run", "-show", "drun")
	if deref(inv.options.CompleterMode) != "run" || deref(inv.options.Mode) != "drun" {
		t.Errorf("options = %+v", inv.options)
	}
}

func TestTheScreenshotHookIsIgnoredWithItsValue(t *testing.T) {
	var stderr bytes.Buffer
	inv, _ := parseLauncherArgs([]string{"-on-screenshot-taken", "upload {file}", "-show", "run"}, &stderr)
	if deref(inv.options.Mode) != "run" || !strings.Contains(stderr.String(), "takes no screenshots") {
		t.Errorf("inv = %+v, stderr = %q", inv.options, stderr.String())
	}
}

func TestMouseBindingsKeepTheirNamespacePrefix(t *testing.T) {
	inv := parseOK(t, "-me-accept-entry", "MousePrimary", "-ml-row-down", "ScrollDown")
	if inv.options.MouseOverrides["me-accept-entry"] != "MousePrimary" || inv.options.MouseOverrides["ml-row-down"] != "ScrollDown" {
		t.Errorf("mouse = %+v", inv.options.MouseOverrides)
	}
	inv = parseOK(t, "-mesg", "hello")
	if len(inv.options.MouseOverrides) != 0 || deref(inv.options.Mesg) != "hello" {
		t.Error("-mesg is not a mouse binding")
	}
}

func TestAppearanceFlagsReachTheSession(t *testing.T) {
	inv := parseOK(t, "-font", "Inter 12", "-style", "compact", "-preview-cmd", "thumb {input} {output} {size}")
	if deref(inv.options.Font) != "Inter 12" || deref(inv.options.Style) != "compact" || deref(inv.options.PreviewCmd) != "thumb {input} {output} {size}" {
		t.Errorf("options = %+v", inv.options)
	}
}

func TestMissingValueErrors(t *testing.T) {
	_, err := parseLauncherArgs([]string{"-show"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-show requires a value") {
		t.Errorf("err = %v", err)
	}
	_, err = parseLauncherArgs([]string{"-lines"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-lines requires a value") {
		t.Errorf("err = %v", err)
	}
}

func TestDoubleDashTolerated(t *testing.T) {
	if deref(parseOK(t, "--show", "drun").options.Mode) != "drun" {
		t.Error("--show is -show")
	}
}

func TestLocalCommands(t *testing.T) {
	if parseOK(t, "-dump-config").local != localDumpConfig || parseOK(t, "-list-keybindings").local != localListKeybindings {
		t.Error("local commands")
	}
	var out bytes.Buffer
	if code := runLauncher([]string{"-help"}, nil, &out, io.Discard); code != 0 || !strings.HasPrefix(out.String(), "wayle launcher — rofi-compatible") {
		t.Errorf("help = %d %q", code, out.String())
	}
	out.Reset()
	if code := runLauncher([]string{"-dump-theme"}, nil, &out, io.Discard); code != 0 || !strings.Contains(out.String(), "does not use rasi themes") {
		t.Errorf("dump-theme = %q", out.String())
	}
}

func TestListKeybindingsPrintsBothTables(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out bytes.Buffer
	if code := runLauncher([]string{"-list-keybindings"}, nil, &out, io.Discard); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out.String(), "kb-accept-entry: Return,KP_Enter\n") || !strings.Contains(out.String(), "me-accept-entry: MousePrimary,MouseDPrimary\n") {
		t.Errorf("listing = %q", out.String())
	}
}

func TestNothingToShowIsAUsageError(t *testing.T) {
	var stderr bytes.Buffer
	if code := runLauncher([]string{"-p", "x"}, nil, io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "Error: nothing to show") {
		t.Errorf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRowsSplitAndTrailingEmptyDropped(t *testing.T) {
	for _, tc := range []struct {
		raw, sep string
		want     []string
	}{
		{"a\nb\n", "\n", []string{"a", "b"}},
		{"a\nb", "\n", []string{"a", "b"}},
		{"a|b|", "|", []string{"a", "b"}},
		{"a\x00b", `\0`, []string{"a", "b"}},
		{"", "\n", []string{}},
	} {
		if got := splitRows(tc.raw, tc.sep); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitRows(%q, %q) = %q", tc.raw, tc.sep, got)
		}
	}
}

func TestFormatOutput(t *testing.T) {
	sel := []launcheripc.Selected{{Index: 2, Text: "my file"}}
	for format, want := range map[string]string{
		"s": "my file\n", "i": "2\n", "d": "3\n", "q": "'my file'\n", "p": "pick\n", "f": "fi\n", "F": "fi\n",
	} {
		var out bytes.Buffer
		printLauncherResult(&out, format, sel, "fi", "pick")
		if out.String() != want {
			t.Errorf("-format %s = %q, want %q", format, out.String(), want)
		}
	}
	var out bytes.Buffer
	printLauncherResult(&out, "F", nil, "a b", "pick")
	if out.String() != "'a b'\n" {
		t.Errorf("an empty selection still prints the filter: %q", out.String())
	}
	out.Reset()
	printLauncherResult(&out, "s", nil, "a b", "pick")
	if out.Len() != 0 {
		t.Errorf("an empty selection prints no rows: %q", out.String())
	}
}
