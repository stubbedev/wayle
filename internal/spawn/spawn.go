// Package spawn runs the shell commands the config names (click
// actions, power commands): process.rs's run_if_set and spawn_quiet.
package spawn

import (
	"bytes"
	"errors"
	"log"
	"os/exec"
	"strings"
)

// Quiet runs cmd through sh -c without waiting: stdout discarded, and
// a failure logged with its exit code and stderr once it exits, so the
// child is always reaped. An empty command runs nothing. The returned
// error is a failure to start; how the command ends is only logged.
func Quiet(cmd string) error {
	if cmd == "" {
		return nil
	}
	c := exec.Command("sh", "-c", cmd) //nolint:gosec // the command comes from the user's own config
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		log.Printf("cannot spawn command %q: %v", cmd, err)
		return err
	}
	go func() {
		err := c.Wait()
		var exit *exec.ExitError
		switch {
		case errors.As(err, &exit):
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				log.Printf("command failed: %q exit %d: %s", cmd, exit.ExitCode(), msg)
			} else {
				log.Printf("command failed: %q exit %d", cmd, exit.ExitCode())
			}
		case err != nil:
			log.Printf("cannot wait on command %q: %v", cmd, err)
		}
	}()
	return nil
}
