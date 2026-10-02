package portal

import (
	"log"
	"os/exec"
)

// detach starts argv with its output discarded and reaps it on a
// goroutine: the portal never waits on the program it hands off to (a
// mail client, a URI handler). The error is a failure to start.
func detach(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // fixed programs; the arguments are the request's own
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// warnf is the backend's tracing::warn!.
func warnf(format string, args ...any) { log.Printf("portal: "+format, args...) }
