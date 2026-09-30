// Package logging is wayle's log setup (wayle/src/core/tracing.rs and
// wayle-shell's tracing_init): a daily file in the state directory,
// "<prefix>.<YYYY-MM-DD>.log", keeping the newest seven, behind the
// standard log package. The CLI logs to the file only, so its output
// stays clean; the shell also echoes to a console writer.
package logging

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// daysToKeep is tracing.rs's DAYS_TO_KEEP.
const daysToKeep = 7

// Dir is ConfigPaths::log_dir: $XDG_STATE_HOME/wayle, or
// ~/.local/state/wayle.
func Dir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "wayle"), nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("neither XDG_STATE_HOME nor HOME environment variable found")
	}
	return filepath.Join(home, ".local", "state", "wayle"), nil
}

// Setup routes the standard logger to prefix's daily file in Dir, and
// also to console when it is non-nil. Close flushes nothing (writes
// are unbuffered) but closes the file.
func Setup(prefix string, console io.Writer) (io.Closer, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	w := &dailyFile{dir: dir, prefix: prefix, now: time.Now}
	// The appender opens today's file up front, as tracing_appender's
	// builder does.
	if _, err := w.Write(nil); err != nil {
		return nil, err
	}
	var out io.Writer = w
	if console != nil {
		out = io.MultiWriter(w, console)
	}
	log.SetOutput(out)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return w, nil
}

// dailyFile is tracing_appender's daily rolling appender.
type dailyFile struct {
	dir    string
	prefix string
	now    func() time.Time

	mu   sync.Mutex
	day  string
	file *os.File
}

func (d *dailyFile) name(day string) string { return d.prefix + "." + day + ".log" }

func (d *dailyFile) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	day := d.now().UTC().Format("2006-01-02")
	if d.file == nil || day != d.day {
		if d.file != nil {
			_ = d.file.Close()
		}
		f, err := os.OpenFile(filepath.Join(d.dir, d.name(day)), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return 0, err
		}
		d.file, d.day = f, day
		d.prune()
	}
	return d.file.Write(p)
}

// prune removes all but the newest daysToKeep files of this prefix.
func (d *dailyFile) prune() {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return
	}
	var logs []string
	for _, e := range entries {
		name := e.Name()
		day, ok := strings.CutPrefix(name, d.prefix+".")
		if !ok || !strings.HasSuffix(day, ".log") {
			continue
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSuffix(day, ".log")); err == nil {
			logs = append(logs, name)
		}
	}
	sort.Strings(logs)
	for len(logs) > daysToKeep {
		_ = os.Remove(filepath.Join(d.dir, logs[0]))
		logs = logs[1:]
	}
}

// Close closes the current file.
func (d *dailyFile) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}
