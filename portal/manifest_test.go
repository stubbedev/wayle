package portal

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
)

const implPrefix = "org.freedesktop.impl.portal."

// pendingInterfaces are declared in the manifests but not yet served
// by the Go backend; the list only shrinks.
var pendingInterfaces = []string{
	"RemoteDesktop", "ScreenCast",
}

// mounted is the short names of the interfaces the backend serves.
func mounted() []string {
	var names []string
	for _, iface := range New(nil, config.Load("", config.DiscardDiagnostics)).interfaces() {
		names = append(names, strings.TrimPrefix(iface.Name, implPrefix))
	}
	slices.Sort(names)
	return names
}

func readResource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../resources/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestManifestsMatchTheMountedInterfaces pins wayle.portal (what the
// frontend discovers) and wayle-portals.conf (how it routes) to what
// the backend mounts, as manifest.rs does: a drifted list silently
// drops an interface at runtime.
func TestManifestsMatchTheMountedInterfaces(t *testing.T) {
	want := slices.Sorted(slices.Values(append(mounted(), pendingInterfaces...)))
	for i := 1; i < len(want); i++ {
		if want[i] == want[i-1] {
			t.Fatalf("%s is both mounted and pending", want[i])
		}
	}

	var declared []string
	for line := range strings.Lines(readResource(t, "wayle.portal")) {
		if list, ok := strings.CutPrefix(strings.TrimSpace(line), "Interfaces="); ok {
			for iface := range strings.SplitSeq(list, ";") {
				if name, ok := strings.CutPrefix(iface, implPrefix); ok {
					declared = append(declared, name)
				}
			}
		}
	}
	slices.Sort(declared)
	if !slices.Equal(declared, want) {
		t.Errorf("wayle.portal Interfaces= %v\nwant %v", declared, want)
	}

	conf := readResource(t, "wayle-portals.conf")
	var routed []string
	for line := range strings.Lines(conf) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), implPrefix); ok {
			if name, target, ok := strings.Cut(rest, "="); ok && strings.TrimSpace(target) == "wayle" {
				routed = append(routed, name)
			}
		}
	}
	slices.Sort(routed)
	if !slices.Equal(routed, want) {
		t.Errorf("wayle-portals.conf routes %v\nwant %v", routed, want)
	}
	if !strings.Contains(conf, "\ndefault=wayle\n") || strings.Contains(conf, "=gtk") {
		t.Error("portals.conf must default to wayle and delegate nothing to gtk")
	}
}
