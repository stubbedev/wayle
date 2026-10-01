package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/cli"
)

// The goldens under testdata/clap were captured from the Rust wayle
// 0.8.53 binary (clap 4.5): every command's -h and --help, a battery
// of parse errors with their exit codes, colored (tty) renderings, and
// the five completion scripts. The Go tree must reproduce them byte
// for byte.

func runCaptured(t *testing.T, color bool, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(append([]string{"wayle"}, args...), cli.Output{
		Stdout: &out, Stderr: &errOut, StdoutColor: color, StderrColor: color,
	})
	return out.String(), errOut.String(), code
}

func TestHelpMatchesClap(t *testing.T) {
	files, err := filepath.Glob("testdata/clap/help/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no help goldens: %v", err)
	}
	for _, file := range files {
		base := strings.TrimSuffix(filepath.Base(file), ".txt")
		mode, path, _ := strings.Cut(base, "_")
		var args []string
		if path != "ROOT" {
			args = strings.Split(path, "_")
		}
		flag := "--help"
		if mode == "short" {
			flag = "-h"
		}
		t.Run(base, func(t *testing.T) {
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := runCaptured(t, false, append(args, flag)...)
			if code != 0 || stderr != "" {
				t.Fatalf("code %d stderr %q", code, stderr)
			}
			if stdout != string(want) {
				t.Errorf("wayle %s %s\n--- got\n%s\n--- want\n%s", path, flag, stdout, want)
			}
		})
	}
}

// golden is one captured invocation: its args, exit code, and streams.
type golden struct {
	args           []string
	code           int
	stdout, stderr string
}

func readGolden(t *testing.T, file string) golden {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	head, rest, _ := strings.Cut(text, "\n")
	g := golden{args: strings.Fields(strings.TrimPrefix(head, "args:"))}
	codeLine, rest, _ := strings.Cut(rest, "\n")
	g.code, err = strconv.Atoi(strings.TrimPrefix(codeLine, "code: "))
	if err != nil {
		t.Fatalf("%s: code line %q", file, codeLine)
	}
	rest = strings.TrimPrefix(rest, "--- stdout\n")
	g.stdout, g.stderr, _ = strings.Cut(rest, "--- stderr\n")
	return g
}

func TestParseErrorsMatchClap(t *testing.T) {
	files, err := filepath.Glob("testdata/clap/errors/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no error goldens: %v", err)
	}
	for _, file := range files {
		g := readGolden(t, file)
		t.Run(strings.Join(g.args, " "), func(t *testing.T) {
			stdout, stderr, code := runCaptured(t, false, g.args...)
			if code != g.code {
				t.Errorf("exit code %d, want %d", code, g.code)
			}
			if stdout != g.stdout {
				t.Errorf("stdout\n--- got\n%s\n--- want\n%s", stdout, g.stdout)
			}
			if stderr != g.stderr {
				t.Errorf("stderr\n--- got\n%s\n--- want\n%s", stderr, g.stderr)
			}
		})
	}
}

func TestColoredOutputMatchesClap(t *testing.T) {
	files, err := filepath.Glob("testdata/clap/tty/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatalf("no tty goldens: %v", err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		head, want, _ := strings.Cut(string(raw), "\n")
		args := strings.Fields(strings.TrimPrefix(head, "args:"))
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, _ := runCaptured(t, true, args...)
			if got := stdout + stderr; got != want {
				t.Errorf("--- got\n%q\n--- want\n%q", got, want)
			}
		})
	}
}

func TestCompletionsMatchClap(t *testing.T) {
	for _, shell := range []string{"bash", "elvish", "fish", "powershell", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			want, err := os.ReadFile("testdata/clap/completions/wayle." + shell)
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := runCaptured(t, false, "completions", shell)
			if code != 0 || stderr != "" {
				t.Fatalf("code %d stderr %q", code, stderr)
			}
			if stdout != string(want) {
				gotLines, wantLines := strings.Split(stdout, "\n"), strings.Split(string(want), "\n")
				for i := range min(len(gotLines), len(wantLines)) {
					if gotLines[i] != wantLines[i] {
						t.Fatalf("first difference at line %d:\n got %q\nwant %q", i+1, gotLines[i], wantLines[i])
					}
				}
				t.Fatalf("length differs: got %d lines, want %d", len(gotLines), len(wantLines))
			}
		})
	}
}

// `wayle -V` reports the Cargo workspace version, so the two binaries
// cannot drift apart silently.
// The binary invoked as `rofi` hands its whole argv to the launcher,
// flags included; under its own name the same flags are wayle's.
func TestRofiInvocationRoutesToLauncher(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"/usr/bin/rofi", "-show", "drun"}, cli.Output{Stdout: &out, Stderr: &errOut})
	if code != 1 || !strings.Contains(errOut.String(), "cannot connect to wayle launcher socket") {
		t.Fatalf("rofi: code %d stderr %q", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	code = run([]string{"wayle", "-show", "drun"}, cli.Output{Stdout: &out, Stderr: &errOut})
	if code != 2 || !strings.Contains(errOut.String(), "unexpected argument '-s' found") {
		t.Fatalf("wayle: code %d stderr %q", code, errOut.String())
	}
}
