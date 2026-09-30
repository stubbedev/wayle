package wallpaper

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

func seeded() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

// imageDir makes a directory holding the named files (empty content).
func imageDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newTestService(monitors ...string) *Service {
	s := New(Options{Extractor: extract.Config{Tool: extract.None}, Rand: seeded()})
	for _, m := range monitors {
		s.RegisterMonitor(m)
	}
	return s
}

func wallpaperOf(t *testing.T, s *Service, monitor string) string {
	t.Helper()
	return s.Monitors()[monitor].Wallpaper
}

func TestParseModes(t *testing.T) {
	for in, want := range map[string]FitMode{"FILL": FitFill, "Fit": FitFit, "center": FitCenter, "stretch": FitStretch} {
		if got, err := ParseFitMode(in); err != nil || got != want {
			t.Errorf("ParseFitMode(%q) = %v %v", in, got, err)
		}
	}
	if _, err := ParseFitMode("tile"); err == nil || err.Error() != "Invalid fit mode: tile" {
		t.Errorf("tile err = %v, want the Rust message", err)
	}
	if m, err := ParseCyclingMode("Shuffle"); err != nil || m != Shuffle {
		t.Errorf("Shuffle = %v %v", m, err)
	}
	if _, err := ParseCyclingMode("random"); err == nil || err.Error() != "Invalid cycling mode: random" {
		t.Errorf("random err = %v", err)
	}
}

func TestMonitorStateWraps(t *testing.T) {
	st := MonitorState{CycleIndex: 4}
	st.advance(5)
	if st.CycleIndex != 0 {
		t.Errorf("advance past the end = %d", st.CycleIndex)
	}
	st.previous(5)
	if st.CycleIndex != 4 {
		t.Errorf("previous before the start = %d", st.CycleIndex)
	}
	st.advance(0)
	st.previous(0)
	if st.CycleIndex != 4 {
		t.Errorf("an empty pool moved the index to %d", st.CycleIndex)
	}
}

func TestScanPicksSupportedExtensionsOnly(t *testing.T) {
	dir := imageDir(t, "b.PNG", "a.jpg", "notes.txt", ".png", "c.webp", "noext")
	c, err := newCycling(dir, Sequential, time.Minute, seeded())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.jpg"), filepath.Join(dir, "b.PNG"), filepath.Join(dir, "c.webp")}
	if !slices.Equal(c.Images, want) {
		t.Errorf("images = %v, want %v", c.Images, want)
	}
	if img, _ := c.ImageAt(4); img != want[1] {
		t.Errorf("ImageAt wraps to %q", img)
	}
}

func TestNewCyclingErrors(t *testing.T) {
	var notFound *DirectoryNotFoundError
	if _, err := newCycling("/nonexistent/wayle", Sequential, time.Minute, seeded()); !errors.As(err, &notFound) ||
		err.Error() != "directory not found: /nonexistent/wayle" {
		t.Errorf("missing dir err = %v", err)
	}
	dir := imageDir(t, "readme.md")
	var none *NoImagesFoundError
	if _, err := newCycling(dir, Sequential, time.Minute, seeded()); !errors.As(err, &none) {
		t.Errorf("imageless dir err = %v", err)
	}
}

func TestSetWallpaperTargetsOneOrAll(t *testing.T) {
	dir := imageDir(t, "a.png", "b.png")
	s := newTestService("DP-1", "DP-2")
	if err := s.SetWallpaper(filepath.Join(dir, "a.png"), ""); err != nil {
		t.Fatal(err)
	}
	if wallpaperOf(t, s, "DP-1") != filepath.Join(dir, "a.png") || wallpaperOf(t, s, "DP-2") != filepath.Join(dir, "a.png") {
		t.Fatalf("all-monitor set = %v", s.Monitors())
	}
	if err := s.SetWallpaper(filepath.Join(dir, "b.png"), "DP-2"); err != nil {
		t.Fatal(err)
	}
	if wallpaperOf(t, s, "DP-1") != filepath.Join(dir, "a.png") || wallpaperOf(t, s, "DP-2") != filepath.Join(dir, "b.png") {
		t.Errorf("one-monitor set leaked: %v", s.Monitors())
	}
	// An unknown monitor is ignored, not registered.
	if err := s.SetWallpaper(filepath.Join(dir, "b.png"), "HDMI-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Monitors()["HDMI-1"]; ok {
		t.Error("setting a wallpaper registered a monitor")
	}
	var nf *ImageNotFoundError
	if err := s.SetWallpaper(filepath.Join(dir, "missing.png"), ""); !errors.As(err, &nf) {
		t.Errorf("missing image err = %v", err)
	}
	if wallpaperOf(t, s, "DP-1") != filepath.Join(dir, "a.png") {
		t.Error("a failed set changed state")
	}
}

func TestSetFitMode(t *testing.T) {
	s := newTestService("DP-1", "DP-2")
	s.SetFitMode(FitCenter, "DP-1")
	if m := s.Monitors(); m["DP-1"].FitMode != FitCenter || m["DP-2"].FitMode != FitFill {
		t.Errorf("per-monitor fit = %v", m)
	}
	s.SetFitMode(FitStretch, "")
	if m := s.Monitors(); m["DP-1"].FitMode != FitStretch || m["DP-2"].FitMode != FitStretch {
		t.Errorf("global fit = %v", m)
	}
	if _, ok := s.FitModeOf("HDMI-1"); ok {
		t.Error("an unknown monitor has a fit mode")
	}
}

func TestSequentialCyclingStartsEveryMonitorAtTheFirstImage(t *testing.T) {
	dir := imageDir(t, "c.png", "a.png", "b.png")
	s := newTestService("DP-1", "DP-2")
	if err := s.StartCycling(dir, time.Hour, Sequential); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"DP-1", "DP-2"} {
		if got := wallpaperOf(t, s, m); got != filepath.Join(dir, "a.png") {
			t.Errorf("%s starts at %q", m, got)
		}
	}
	s.Advance()
	if got := wallpaperOf(t, s, "DP-1"); got != filepath.Join(dir, "b.png") {
		t.Errorf("next = %q", got)
	}
	s.Rewind()
	s.Rewind()
	if got := wallpaperOf(t, s, "DP-2"); got != filepath.Join(dir, "c.png") {
		t.Errorf("previous wraps to %q", got)
	}
	s.StopCycling()
	if s.CyclingConfig() != nil {
		t.Fatal("still cycling")
	}
	before := s.Monitors()
	s.Advance()
	if got := s.Monitors(); got["DP-1"] != before["DP-1"] {
		t.Error("next moved a stopped cycle")
	}
}

func TestSharedShuffleShowsOneImage(t *testing.T) {
	dir := imageDir(t, "1.png", "2.png", "3.png", "4.png", "5.png", "6.png", "7.png", "8.png")
	s := New(Options{Extractor: extract.Config{Tool: extract.None}, Rand: seeded(), SharedCycle: true})
	for _, m := range []string{"DP-1", "DP-2", "DP-3"} {
		s.RegisterMonitor(m)
	}
	if err := s.StartCycling(dir, time.Hour, Shuffle); err != nil {
		t.Fatal(err)
	}
	m := s.Monitors()
	if m["DP-1"].CycleIndex != m["DP-2"].CycleIndex || m["DP-2"].CycleIndex != m["DP-3"].CycleIndex {
		t.Fatalf("shared shuffle split the indices: %v", m)
	}
	// A monitor plugged in later joins the shared index.
	s.RegisterMonitor("HDMI-1")
	if got := s.Monitors()["HDMI-1"]; got.CycleIndex != m["DP-1"].CycleIndex || got.Wallpaper != m["DP-1"].Wallpaper {
		t.Errorf("hotplugged monitor = %+v, want the shared %+v", got, m["DP-1"])
	}
	// Turning sharing off hands out independent indices; on again
	// re-synchronizes on the lowest-named monitor.
	s.SetSharedCycle(false)
	s.SetSharedCycle(true)
	m = s.Monitors()
	for _, name := range []string{"DP-2", "DP-3", "HDMI-1"} {
		if m[name].CycleIndex != m["DP-1"].CycleIndex {
			t.Errorf("resync left %s at %d, DP-1 at %d", name, m[name].CycleIndex, m["DP-1"].CycleIndex)
		}
	}
}

func TestIndependentShuffleDiffersAcrossMonitors(t *testing.T) {
	names := []string{}
	for i := range 32 {
		names = append(names, string(rune('a'+i%26))+string(rune('a'+i/26))+".png")
	}
	dir := imageDir(t, names...)
	s := newTestService("DP-1", "DP-2", "DP-3", "DP-4")
	if err := s.StartCycling(dir, time.Hour, Shuffle); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, st := range s.Monitors() {
		seen[st.CycleIndex] = true
	}
	if len(seen) < 2 {
		t.Errorf("four monitors share one index in independent shuffle: %v", s.Monitors())
	}
}

func TestHotpluggedMonitorSeededFromTheCycle(t *testing.T) {
	dir := imageDir(t, "a.png", "b.png")
	s := newTestService("DP-1")
	if err := s.StartCycling(dir, time.Hour, Sequential); err != nil {
		t.Fatal(err)
	}
	s.RegisterMonitor("DP-2")
	if got := wallpaperOf(t, s, "DP-2"); got != filepath.Join(dir, "a.png") {
		t.Errorf("hotplugged monitor = %q", got)
	}
	s.StopCycling()
	s.RegisterMonitor("DP-3")
	if got := wallpaperOf(t, s, "DP-3"); got != "" {
		t.Errorf("without cycling a new monitor got %q", got)
	}
	// Registering twice keeps the existing state.
	_ = s.SetWallpaper(filepath.Join(dir, "b.png"), "DP-3")
	s.RegisterMonitor("DP-3")
	if got := wallpaperOf(t, s, "DP-3"); got != filepath.Join(dir, "b.png") {
		t.Errorf("re-registration reset DP-3 to %q", got)
	}
	s.UnregisterMonitor("DP-3")
	s.UnregisterMonitor("DP-9")
	if slices.Contains(s.MonitorNames(), "DP-3") {
		t.Error("DP-3 still registered")
	}
}

func TestThemingPath(t *testing.T) {
	m := map[string]MonitorState{
		"DP-3":  {Wallpaper: "third.png"},
		"DP-1":  {Wallpaper: "first.png"},
		"eDP-1": {Wallpaper: "laptop.png"},
	}
	if p, skip := themingPath(m, ""); p != "first.png" || skip {
		t.Errorf("fallback = %q %v, want the lowest connector", p, skip)
	}
	if p, skip := themingPath(m, "eDP-1"); p != "laptop.png" || skip {
		t.Errorf("configured = %q %v", p, skip)
	}
	// An unplugged theming monitor must not hand theming elsewhere.
	if p, skip := themingPath(m, "HDMI-1"); !skip || p != "" {
		t.Errorf("unplugged theming monitor = %q %v, want a skip", p, skip)
	}
	// The lowest name wins even with nothing to show.
	m["DP-1"] = MonitorState{}
	if p, _ := themingPath(m, ""); p != "" {
		t.Errorf("fallback skipped to %q", p)
	}
	if p, skip := themingPath(map[string]MonitorState{}, ""); p != "" || skip {
		t.Error("no monitors resolved a path")
	}
}

func waitTick(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no tick within 3s")
	}
}

func TestChangesTickOnlyOnChange(t *testing.T) {
	s := newTestService("DP-1")
	ch, stop := s.Changes()
	defer stop()
	s.SetFitMode(FitFill, "") // already fill
	select {
	case <-ch:
		t.Fatal("an unchanged set ticked")
	default:
	}
	s.SetFitMode(FitFit, "")
	waitTick(t, ch)
}

func TestTimerAdvancesTheCycle(t *testing.T) {
	dir := imageDir(t, "a.png", "b.png", "c.png")
	s := newTestService("DP-1")
	ctx := t.Context()
	go s.Run(ctx)
	if err := s.StartCycling(dir, 30*time.Millisecond, Sequential); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for wallpaperOf(t, s, "DP-1") == filepath.Join(dir, "a.png") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := wallpaperOf(t, s, "DP-1"); got == filepath.Join(dir, "a.png") {
		t.Fatal("the timer never advanced")
	}
	s.StopCycling()
	time.Sleep(20 * time.Millisecond)
	frozen := wallpaperOf(t, s, "DP-1")
	time.Sleep(120 * time.Millisecond)
	if got := wallpaperOf(t, s, "DP-1"); got != frozen {
		t.Errorf("a stopped cycle kept ticking: %q -> %q", frozen, got)
	}
}

func TestDirectoryChangeRefreshesThePool(t *testing.T) {
	dir := imageDir(t, "a.png")
	s := newTestService("DP-1")
	ctx := t.Context()
	go s.Run(ctx)
	if err := s.StartCycling(dir, time.Hour, Sequential); err != nil {
		t.Fatal(err)
	}
	// The watcher is set up asynchronously by Run.
	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = os.WriteFile(filepath.Join(dir, "b.png"), nil, 0o600)
		time.Sleep(30 * time.Millisecond)
		if s.CyclingConfig().ImageCount() == 2 || time.Now().After(deadline) {
			break
		}
		_ = os.Remove(filepath.Join(dir, "b.png"))
	}
	if n := s.CyclingConfig().ImageCount(); n != 2 {
		t.Fatalf("pool = %d images after adding one, want 2", n)
	}
	// A new subdirectory is not a file change.
	if err := os.Mkdir(filepath.Join(dir, "sub.png"), 0o700); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if n := s.CyclingConfig().ImageCount(); n != 2 {
		t.Errorf("a new directory refreshed the pool to %d", n)
	}
}

func TestFileEventFilter(t *testing.T) {
	ev := func(mask uint32) []byte {
		b := make([]byte, 16)
		b[4], b[5], b[6], b[7] = byte(mask), byte(mask>>8), byte(mask>>16), byte(mask>>24)
		return b
	}
	const inCreate, inDelete, inMovedTo, inIsDir, inModify = 0x100, 0x200, 0x80, 0x40000000, 0x2
	for mask, want := range map[uint32]bool{
		inCreate: true, inDelete: true, inMovedTo: true, inMovedTo | inIsDir: true,
		inCreate | inIsDir: false, inDelete | inIsDir: false, inModify: false,
	} {
		if got := fileEvent(ev(mask)); got != want {
			t.Errorf("mask %#x = %v, want %v", mask, got, want)
		}
	}
}

// fakeExtractor installs a matugen that records its image argument.
func fakeExtractor(t *testing.T) (log string) {
	t.Helper()
	bin := t.TempDir()
	log = filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\necho \"$2\" >> " + log + "\necho '{}'\n"
	if err := os.WriteFile(filepath.Join(bin, "matugen"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func TestExtractColorsDeduplicatesAndFollowsTheming(t *testing.T) {
	log := fakeExtractor(t)
	dir := imageDir(t, "a.png", "b.png")
	s := New(Options{Extractor: extract.Config{Tool: extract.Matugen}, Rand: seeded()})
	s.RegisterMonitor("DP-2")
	s.RegisterMonitor("DP-1")
	ctx := context.Background()
	done, stop := s.Extracted()
	defer stop()

	// Nothing set: no tool runs, but the pass still completes.
	if err := s.ExtractColors(ctx); err != nil {
		t.Fatal(err)
	}
	waitTick(t, done)
	if got := calls(t, log); len(got) != 0 {
		t.Fatalf("extracted without a wallpaper: %v", got)
	}
	_ = s.SetWallpaper(filepath.Join(dir, "a.png"), "DP-1")
	_ = s.SetWallpaper(filepath.Join(dir, "b.png"), "DP-2")
	_ = s.ExtractColors(ctx)
	_ = s.ExtractColors(ctx) // same wallpaper: deduplicated
	if got := calls(t, log); !slices.Equal(got, []string{filepath.Join(dir, "a.png")}) {
		t.Fatalf("calls = %v, want one extraction of DP-1's wallpaper", got)
	}
	// A configured theming monitor that is unplugged keeps the palette.
	s.mu.Lock()
	s.themingMonitor = "HDMI-1"
	s.mu.Unlock()
	_ = s.ExtractColors(ctx)
	if got := calls(t, log); len(got) != 1 {
		t.Fatalf("an unplugged theming monitor re-extracted: %v", got)
	}
	s.mu.Lock()
	s.themingMonitor = "DP-2"
	s.mu.Unlock()
	_ = s.ExtractColors(ctx)
	if got := calls(t, log); len(got) != 2 || got[1] != filepath.Join(dir, "b.png") {
		t.Errorf("theming DP-2 = %v", got)
	}
}

func TestRunReExtractsOnExtractorChange(t *testing.T) {
	log := fakeExtractor(t)
	dir := imageDir(t, "a.png")
	s := New(Options{Extractor: extract.Config{Tool: extract.Matugen}, Rand: seeded()})
	s.RegisterMonitor("DP-1")
	_ = s.SetWallpaper(filepath.Join(dir, "a.png"), "")
	ctx := t.Context()
	done, stop := s.Extracted()
	defer stop()
	go s.Run(ctx)
	waitCalls := func(n int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for len(calls(t, log)) < n && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := calls(t, log); len(got) != n {
			t.Fatalf("calls = %v, want %d", got, n)
		}
	}
	waitCalls(1) // the startup pass
	waitTick(t, done)
	// Same extractor config: nothing to do.
	s.SetExtractor(extract.Config{Tool: extract.Matugen})
	time.Sleep(50 * time.Millisecond)
	if got := calls(t, log); len(got) != 1 {
		t.Fatalf("an unchanged extractor re-extracted: %v", got)
	}
	// A changed config forgets the last extraction and runs again.
	s.SetExtractor(extract.Config{Tool: extract.Matugen, MatugenLight: true})
	waitCalls(2)
	s.SetThemingMonitor("DP-1")
	waitCalls(3)
}
