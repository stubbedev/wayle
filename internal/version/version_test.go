package version

import (
	"os"
	"regexp"
	"testing"
)

func TestVersionMatchesCargoWorkspace(t *testing.T) {
	data, err := os.ReadFile("../../Cargo.toml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^version = "([^"]+)"`).FindSubmatch(data)
	if m == nil {
		t.Fatal("no workspace version in Cargo.toml")
	}
	if got := string(m[1]); got != Version {
		t.Fatalf("Version = %q, Cargo.toml workspace version = %q", Version, got)
	}
}

func TestVersionMismatchIsDetected(t *testing.T) {
	m := regexp.MustCompile(`(?m)^version = "([^"]+)"`).FindSubmatch([]byte("[workspace.package]\nversion = \"9.9.9\"\n"))
	if m == nil || string(m[1]) == Version {
		t.Fatalf("the Cargo.toml version pattern must tell 9.9.9 from %s", Version)
	}
}
