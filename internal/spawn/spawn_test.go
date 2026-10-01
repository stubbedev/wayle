package spawn

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuf is a log sink safe to read while the logger writes.
type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLog(t *testing.T) *lockedBuf {
	t.Helper()
	buf := &lockedBuf{}
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return buf
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestQuietRunsTheCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	if err := Quiet("echo hello > " + marker); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the command", func() bool {
		data, _ := os.ReadFile(marker)
		return strings.TrimSpace(string(data)) == "hello"
	})
}

func TestQuietLogsFailures(t *testing.T) {
	logs := captureLog(t)
	if err := Quiet("echo oops >&2; exit 3"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure log", func() bool { return strings.Contains(logs.String(), "exit 3: oops") })
}

func TestQuietEmptyRunsNothing(t *testing.T) {
	logs := captureLog(t)
	if err := Quiet(""); err != nil {
		t.Errorf("empty command: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if logs.String() != "" {
		t.Errorf("an empty command logged %q", logs.String())
	}
}
