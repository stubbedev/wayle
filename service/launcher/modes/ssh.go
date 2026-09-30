package modes

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/wayle/service/launcher"
)

// SSHConfig is the ssh mode's knobs (rofi's -ssh-* family).
type SSHConfig struct {
	// Client is the ssh binary.
	Client string
	// Command is the connect template ({terminal}, {ssh-client},
	// {host}).
	Command string
	// ParseHosts adds /etc/hosts names.
	ParseHosts bool
	// ParseKnownHosts adds ~/.ssh/known_hosts names.
	ParseKnownHosts bool
	// Terminal is the terminal emulator; empty autodetects.
	Terminal   string
	MaxHistory uint32
}

// DefaultSSHConfig is SshConfig::default.
func DefaultSSHConfig() SSHConfig {
	return SSHConfig{Client: "ssh", Command: "{terminal} -e {ssh-client} {host}", ParseKnownHosts: true, MaxHistory: 25}
}

// SSH connects to known hosts in a terminal (ssh.rs).
type SSH struct {
	cfg     SSHConfig
	history *launcher.History
	hosts   []string
}

// NewSSH builds the mode.
func NewSSH(cfg SSHConfig, history *launcher.History) *SSH { return &SSH{cfg: cfg, history: history} }

// Name is "ssh".
func (*SSH) Name() string { return "ssh" }

func (s *SSH) connect(host string) {
	terminal := launcher.DetectTerminal(s.cfg.Terminal)
	launcher.RunShell(launcher.Render(s.cfg.Command, launcher.Values(map[string]string{
		"host": host, "terminal": terminal, "ssh-client": s.cfg.Client,
	})))
	if s.history == nil {
		return
	}
	if err := s.history.Record("ssh", host, s.cfg.MaxHistory); err != nil {
		log.Printf("launcher: ssh history record failed: %v", err)
	}
}

// Load lists ~/.ssh/config hosts (plus known_hosts and /etc/hosts when
// enabled), recently used first, then alphabetically.
func (s *SSH) Load(context.Context) launcher.ModeState {
	home := os.Getenv("HOME")
	hosts := map[string]bool{}
	parseSSHConfig(filepath.Join(home, ".ssh", "config"), hosts)
	if s.cfg.ParseKnownHosts {
		parseKnownHosts(filepath.Join(home, ".ssh", "known_hosts"), hosts)
	}
	if s.cfg.ParseHosts {
		parseEtcHosts("/etc/hosts", hosts)
	}
	var recent []string
	if s.history != nil {
		recent, _ = s.history.Recent("ssh")
	}
	s.hosts = orderByRecent(hosts, recent)
	items := make([]launcher.Item, len(s.hosts))
	for i, h := range s.hosts {
		items[i] = launcher.NewItem(h)
	}
	return launcher.ModeState{Items: items, Prompt: "ssh"}
}

// Activate connects to the row's host or the typed one.
func (s *SSH) Activate(_ context.Context, target launcher.Target, kind launcher.ActivateKind, _ string) launcher.Action {
	var host string
	if row, ok := target.Row(); ok {
		if int(row) >= len(s.hosts) {
			return launcher.ActionNothing{}
		}
		host = s.hosts[row]
	} else if custom, ok := kind.(launcher.ActivateCustom); ok {
		host = custom.Text
	} else {
		return launcher.ActionNothing{}
	}
	if strings.TrimSpace(host) == "" {
		return launcher.ActionNothing{}
	}
	s.connect(strings.TrimSpace(host))
	return launcher.ActionClose{}
}

// Delete forgets the host's history entry and reloads.
func (s *SSH) Delete(ctx context.Context, index uint32) launcher.Action {
	if s.history == nil || int(index) >= len(s.hosts) {
		return launcher.ActionNothing{}
	}
	if err := s.history.Remove("ssh", s.hosts[index]); err != nil {
		log.Printf("launcher: ssh history delete failed: %v", err)
	}
	return launcher.ActionReload{State: s.Load(ctx)}
}

// hostNames adds a Host line's names, skipping wildcards and negations.
func hostNames(rest string, hosts map[string]bool) {
	for h := range strings.FieldsSeq(rest) {
		if !strings.ContainsAny(h, "*?") && !strings.HasPrefix(h, "!") {
			hosts[h] = true
		}
	}
}

// keyword splits a config line at its first whitespace.
func keyword(line string) (string, string, bool) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return "", "", false
	}
	return line[:i], line[i+1:], true
}

// parseSSHConfig reads Host entries, following Include one level deep.
func parseSSHConfig(path string, hosts map[string]bool) {
	data, err := os.ReadFile(path) //nolint:gosec // the user's own ssh config
	if err != nil {
		return
	}
	home := os.Getenv("HOME")
	for line := range strings.SplitSeq(string(data), "\n") {
		kw, rest, ok := keyword(strings.TrimSpace(line))
		if !ok {
			continue
		}
		switch strings.ToLower(kw) {
		case "host":
			hostNames(rest, hosts)
		case "include":
			for pattern := range strings.FieldsSeq(rest) {
				if strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "~") {
					pattern = strings.ReplaceAll(pattern, "~", home)
				} else {
					// Relative includes resolve against ~/.ssh.
					pattern = home + "/.ssh/" + pattern
				}
				matches, _ := filepath.Glob(pattern)
				for _, included := range matches {
					parseSSHConfigFlat(included, hosts)
				}
			}
		}
	}
}

// parseSSHConfigFlat reads an included file's Host lines only.
func parseSSHConfigFlat(path string, hosts map[string]bool) {
	data, err := os.ReadFile(path) //nolint:gosec // an ssh config include
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if kw, rest, ok := keyword(strings.TrimSpace(line)); ok && strings.EqualFold(kw, "host") {
			hostNames(rest, hosts)
		}
	}
}

// parseKnownHosts reads each line's first field: hashed entries
// skipped, brackets and ports stripped, comma lists split.
func parseKnownHosts(path string, hosts map[string]bool) {
	data, err := os.ReadFile(path) //nolint:gosec // the user's known_hosts
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		for entry := range strings.SplitSeq(fields[0], ",") {
			host, _, _ := strings.Cut(strings.TrimLeft(entry, "["), "]")
			host, _, _ = strings.Cut(host, ":")
			if host != "" {
				hosts[host] = true
			}
		}
	}
}

// parseEtcHosts reads every name after the address; comments and the
// localhost names are skipped.
func parseEtcHosts(path string, hosts map[string]bool) {
	data, err := os.ReadFile(path) //nolint:gosec // /etc/hosts
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, name := range fields[1:] {
			switch {
			case name == "localhost", name == "localhost.localdomain", name == "ip6-localhost":
			case strings.HasPrefix(name, "ip6-"):
			default:
				hosts[name] = true
			}
		}
	}
}
