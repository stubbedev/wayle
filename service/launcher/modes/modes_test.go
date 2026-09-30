package modes

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/service/clipboard"
	"github.com/stubbedev/wayle/service/launcher"
)

var bg = context.Background()

func TestIsIgnored(t *testing.T) {
	prefixes := []string{"sudo ", "tmp-"}
	if !IsIgnored("sudo vim", prefixes) || !IsIgnored("tmp-script", prefixes) {
		t.Error("prefixed entries are kept out of history")
	}
	if IsIgnored("vim", prefixes) || IsIgnored("run sudo vim", prefixes) || IsIgnored("vim", nil) || IsIgnored("vim", []string{""}) {
		t.Error("everything else is still recorded")
	}
}

func dmenuWith(rows []string, cfg DmenuConfig) *Dmenu {
	ch := make(chan []string, 1)
	ch <- rows
	close(ch)
	return NewDmenu(cfg, ch)
}

func TestDmenuRowsBecomeItemsWithFlags(t *testing.T) {
	m := dmenuWith([]string{"alpha", "beta\x00icon\x1ffirefox"}, DmenuConfig{Urgent: []uint32{0, 9}})
	st := m.Load(bg)
	if len(st.Items) != 2 || !st.Items[0].Flags.Has(launcher.FlagUrgent) || st.Items[1].Icon == nil || !st.UseHotKeys {
		t.Errorf("state = %+v", st)
	}
	if st.Prompt != "dmenu" {
		t.Errorf("default prompt = %q", st.Prompt)
	}
}

func TestDmenuAcceptExitsWithIndexAndText(t *testing.T) {
	m := dmenuWith([]string{"alpha", "beta"}, DmenuConfig{})
	m.Load(bg)
	exit, ok := m.Activate(bg, launcher.RowTarget(1), launcher.ActivateDefault{}, "be").(launcher.ActionExit)
	if !ok || exit.Code != 0 || !reflect.DeepEqual(exit.Selected, []launcher.Selection{{Index: 1, Text: "beta"}}) {
		t.Errorf("exit = %#v", exit)
	}
	exit, _ = m.Activate(bg, launcher.NoRow, launcher.ActivateCustom{Text: "typed"}, "typed").(launcher.ActionExit)
	if !reflect.DeepEqual(exit.Selected, []launcher.Selection{{Index: -1, Text: "typed"}}) {
		t.Errorf("custom = %#v", exit)
	}
	if _, ok := m.Activate(bg, launcher.RowTarget(7), launcher.ActivateDefault{}, "").(launcher.ActionNothing); !ok {
		t.Error("a row past the end accepts nothing")
	}
}

func TestDmenuKbCustomMapsToRofiCodes(t *testing.T) {
	m := dmenuWith([]string{"alpha"}, DmenuConfig{})
	m.Load(bg)
	exit, _ := m.Activate(bg, launcher.RowTarget(0), launcher.ActivateKbCustom{N: 3}, "").(launcher.ActionExit)
	if exit.Code != 12 {
		t.Errorf("code = %d, want 12", exit.Code)
	}
}

func TestDmenuMultiSelectCarriesAll(t *testing.T) {
	m := dmenuWith([]string{"a", "b", "c"}, DmenuConfig{})
	m.Load(bg)
	exit, _ := m.ActivateMany(bg, []uint32{0, 2}, "").(launcher.ActionExit)
	if !reflect.DeepEqual(exit.Selected, []launcher.Selection{{Index: 0, Text: "a"}, {Index: 2, Text: "c"}}) {
		t.Errorf("selected = %#v", exit.Selected)
	}
	if _, ok := m.ActivateMany(bg, nil, "").(launcher.ActionNothing); !ok {
		t.Error("an empty pick accepts nothing")
	}
}

type staticChild struct {
	name    string
	entries []string
	custom  bool
}

func (s *staticChild) Name() string { return s.name }

func (s *staticChild) Load(context.Context) launcher.ModeState {
	items := make([]launcher.Item, len(s.entries))
	for i, e := range s.entries {
		items[i] = launcher.NewItem(e)
	}
	return launcher.ModeState{Items: items}
}

func (s *staticChild) Activate(_ context.Context, target launcher.Target, _ launcher.ActivateKind, _ string) launcher.Action {
	i, ok := target.Row()
	if !ok {
		return launcher.ActionSetInput{Text: s.name + ":custom"}
	}
	return launcher.ActionSetInput{Text: s.name + ":" + s.entries[i]}
}

func (s *staticChild) AllowsCustom() bool { return s.custom }

func combiFixture() *Combi {
	return NewCombi([]launcher.Mode{
		&staticChild{name: "window", entries: []string{"term", "browser"}},
		&staticChild{name: "drun", entries: []string{"Firefox"}, custom: true},
	}, "{mode} {text}")
}

func TestCombiMergesChildrenWithFormat(t *testing.T) {
	st := combiFixture().Load(bg)
	if len(st.Items) != 3 || st.Items[0].Display != "window term" || st.Items[2].Display != "drun Firefox" || st.Prompt != "combi" {
		t.Errorf("state = %+v", st)
	}
}

func TestCombiBangSubsetMasksOtherChildren(t *testing.T) {
	c := combiFixture()
	c.Load(bg)
	if mask, ok := c.Subset("dr"); !ok || !reflect.DeepEqual(mask, []bool{false, false, true}) {
		t.Errorf("mask = %v %v", mask, ok)
	}
	if _, ok := c.Subset("xyz"); ok {
		t.Error("a bang naming no child is no subset")
	}
	if _, ok := c.Subset(""); ok {
		t.Error("an empty bang is no subset")
	}
}

func TestCombiRoutesToTheOwner(t *testing.T) {
	c := combiFixture()
	c.Load(bg)
	if a, _ := c.Activate(bg, launcher.RowTarget(2), launcher.ActivateDefault{}, "").(launcher.ActionSetInput); a.Text != "drun:Firefox" {
		t.Errorf("row 2 = %q", a.Text)
	}
	if a, _ := c.Activate(bg, launcher.RowTarget(1), launcher.ActivateDefault{}, "").(launcher.ActionSetInput); a.Text != "window:browser" {
		t.Errorf("row 1 = %q (the child's own index)", a.Text)
	}
	if a, _ := c.Activate(bg, launcher.NoRow, launcher.ActivateCustom{Text: "x"}, "x").(launcher.ActionSetInput); a.Text != "drun:custom" {
		t.Errorf("custom = %q (the first child that accepts it)", a.Text)
	}
}

func TestRunScanFindsOnlyExecutables(t *testing.T) {
	dir := t.TempDir()
	for name, mode := range map[string]os.FileMode{"mytool": 0o755, "notexec": 0o644} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "mytool"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	collectExecutables(dir, names)
	if !names["mytool"] || names["notexec"] || !names["linked"] {
		t.Errorf("names = %v (a symlinked executable counts)", names)
	}
}

func TestRecentCommandsOrderFirst(t *testing.T) {
	got := orderByRecent(map[string]bool{"alpha": true, "mytool": true, "zeta": true}, []string{"mytool", "gone"})
	if !reflect.DeepEqual(got, []string{"mytool", "alpha", "zeta"}) {
		t.Errorf("got %v", got)
	}
}

func TestRunRecordsLaunchesUnlessIgnored(t *testing.T) {
	h, err := launcher.OpenHistoryAt(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	cfg := DefaultRunConfig()
	cfg.RunCommand = "true {cmd}"
	cfg.IgnoredPrefixes = []string{"secret"}
	r := NewRun(cfg, h)
	r.Activate(bg, launcher.NoRow, launcher.ActivateCustom{Text: "visible"}, "")
	r.Activate(bg, launcher.NoRow, launcher.ActivateCustom{Text: "secret-thing"}, "")
	if _, ok := r.Activate(bg, launcher.NoRow, launcher.ActivateDefault{}, "").(launcher.ActionNothing); !ok {
		t.Error("a plain accept with no row does nothing")
	}
	if got, _ := h.Recent("run"); !reflect.DeepEqual(got, []string{"visible"}) {
		t.Errorf("history = %v", got)
	}
}

func TestDrunCategoryFiltering(t *testing.T) {
	cfg := DefaultDrunConfig()
	if !categoryAllowed("Network;WebBrowser;", cfg) {
		t.Error("no filter allows all")
	}
	cfg.ExcludeCategories = []string{"WebBrowser"}
	if categoryAllowed("Network;WebBrowser;", cfg) {
		t.Error("an excluded category hides")
	}
	cfg.ExcludeCategories = nil
	cfg.Categories = []string{"Development"}
	if categoryAllowed("Network;WebBrowser;", cfg) || !categoryAllowed("Development;IDE;", cfg) {
		t.Error("an allow list restricts")
	}
}

func TestFieldCodesDetected(t *testing.T) {
	if !isFieldCode("%U") || isFieldCode("100%") || isFieldCode("file.txt") {
		t.Error("field codes")
	}
}

func TestDrunMatchTextRespectsSelectedFields(t *testing.T) {
	got := buildMatchText([]DrunField{DrunFieldName, DrunFieldKeywords}, []fieldValue{
		{DrunFieldName, "Firefox"}, {DrunFieldExec, "firefox"}, {DrunFieldKeywords, "web browser"},
	})
	if got != "Firefox web browser" {
		t.Errorf("got %q", got)
	}
}

func TestDrunListsAppsAndLinks(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "mytool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("b.desktop", "[Desktop Entry]\nType=Application\nName=Beta & Co\nGenericName=Tool\nExec=mytool\nIcon=beta\n")
	write("a.desktop", "[Desktop Entry]\nType=Application\nName=Alpha\nExec=mytool\nNoDisplay=true\n")
	write("link.desktop", "[Desktop Entry]\nType=Link\nName=Example\nURL=https://example.com\n")
	d := NewDrun(DefaultDrunConfig(), nil)
	d.dirs = []string{dir}
	st := d.Load(bg)
	if len(st.Items) != 2 || !st.MarkupRows {
		t.Fatalf("items = %+v", st.Items)
	}
	if st.Items[0].Display != "Beta &amp; Co <span weight='light' size='small'><i>(Tool)</i></span>" {
		t.Errorf("display = %q (escaped name, optional generic block)", st.Items[0].Display)
	}
	if st.Items[0].Icon != launcher.IconName("beta") {
		t.Errorf("icon = %#v", st.Items[0].Icon)
	}
	if st.Items[1].MatchText != "Example https://example.com" {
		t.Errorf("link match text = %q", st.Items[1].MatchText)
	}
}

func TestSwayTreeWalkExtractsViews(t *testing.T) {
	tree := `{"type":"root","nodes":[{"type":"workspace","name":"3","nodes":[
		{"type":"con","id":7,"name":"vim","app_id":"foot","focused":true,"nodes":[]},
		{"type":"con","id":9,"name":null,"nodes":[]}],
		"floating_nodes":[{"type":"floating_con","id":11,"name":"calc","window_properties":{"class":"Galculator"},"focused":false,"nodes":[]}]}]}`
	windows, err := parseSwayTree([]byte(tree))
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 || windows[0].class != "foot" || windows[0].workspace != "3" || !windows[0].focused || windows[1].class != "Galculator" {
		t.Errorf("windows = %+v", windows)
	}
	if !windows[1].onCurrentWorkspace {
		t.Error("the focused window's workspace is current")
	}
}

func TestHyprlandClientsParse(t *testing.T) {
	active := int64(2)
	windows, err := parseHyprlandClients([]byte(`[
		{"address":"0x1","title":"a","class":"foot","mapped":true,"workspace":{"id":2,"name":"2"},"focusHistoryID":0},
		{"address":"0x2","title":"b","class":"kitty","mapped":false,"workspace":{"id":2,"name":"2"},"focusHistoryID":1},
		{"address":"0x3","title":"c","class":"zen","workspace":{"id":3,"name":"web"},"focusHistoryID":2}]`), &active)
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 || !windows[0].focused || !windows[0].onCurrentWorkspace || windows[1].onCurrentWorkspace || windows[1].workspace != "web" {
		t.Errorf("windows = %+v (an unmapped client is left out)", windows)
	}
}

func TestWindowFormatAndMatchText(t *testing.T) {
	item := windowItem(windowInfo{id: "0x1", title: "README.md - vim", class: "foot", workspace: "3"}, DefaultWindowConfig())
	if item.Display != "3   foot   README.md - vim" || item.MatchText != "README.md - vim foot" || item.Icon != launcher.IconName("foot") {
		t.Errorf("item = %+v", item)
	}
}

func TestWindowModeWithoutACompositorIsEmpty(t *testing.T) {
	w := &Window{cfg: DefaultWindowConfig()}
	st := w.Load(bg)
	if len(st.Items) != 0 || !st.NoCustom || st.Prompt != "window" {
		t.Errorf("state = %+v", st)
	}
	if _, ok := w.Activate(bg, launcher.RowTarget(0), launcher.ActivateDefault{}, "").(launcher.ActionNothing); !ok {
		t.Error("activating without a compositor does nothing")
	}
}

func TestSSHConfigHostsSkipWildcards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	_ = os.WriteFile(path, []byte("Host github.com\n  User git\nHost *.internal !bad prod-1 prod-2\nHost ?\n"), 0o600)
	hosts := map[string]bool{}
	parseSSHConfig(path, hosts)
	if !reflect.DeepEqual(hosts, map[string]bool{"github.com": true, "prod-1": true, "prod-2": true}) {
		t.Errorf("hosts = %v", hosts)
	}
}

func TestSSHIncludeIsFollowedOneLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_ = os.MkdirAll(filepath.Join(home, ".ssh", "conf.d"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".ssh", "conf.d", "work"), []byte("Host work-box\nInclude deeper\n"), 0o600)
	_ = os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Include conf.d/*\n"), 0o600)
	hosts := map[string]bool{}
	parseSSHConfig(filepath.Join(home, ".ssh", "config"), hosts)
	if !reflect.DeepEqual(hosts, map[string]bool{"work-box": true}) {
		t.Errorf("hosts = %v", hosts)
	}
}

func TestKnownHostsStripPortsAndHashes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	_ = os.WriteFile(path, []byte("github.com ssh-ed25519 AAAA\n[bastion.example.com]:2222,10.0.0.1 ecdsa AAAA\n|1|hashed|entry ssh-rsa AAAA\n"), 0o600)
	hosts := map[string]bool{}
	parseKnownHosts(path, hosts)
	if !reflect.DeepEqual(hosts, map[string]bool{"10.0.0.1": true, "bastion.example.com": true, "github.com": true}) {
		t.Errorf("hosts = %v", hosts)
	}
}

func TestEtcHostsSkipsLocalhost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	_ = os.WriteFile(path, []byte("127.0.0.1 localhost\n::1 ip6-localhost ip6-loopback\n10.0.0.5 nas nas.lan # media box\n"), 0o600)
	hosts := map[string]bool{}
	parseEtcHosts(path, hosts)
	if !reflect.DeepEqual(hosts, map[string]bool{"nas": true, "nas.lan": true}) {
		t.Errorf("hosts = %v", hosts)
	}
}

func fbFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	for name, body := range map[string]string{"b.txt": "b", "a.txt": "a", ".hidden": "h", "sub/deep.txt": "d"} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
	}
	return dir
}

func TestListingSortsDirsFirstAndHidesDotfiles(t *testing.T) {
	entries := listDir(fbFixture(t), false)
	sortEntries(entries, FileSortName, true)
	var names []string
	for _, e := range entries {
		names = append(names, filepath.Base(e.path))
	}
	if !reflect.DeepEqual(names, []string{"sub", "a.txt", "b.txt"}) {
		t.Errorf("names = %v", names)
	}
}

func TestRecursiveWalkFindsNestedFiles(t *testing.T) {
	entries := walkFiles(fbFixture(t), false)
	found := false
	for _, e := range entries {
		if e.isDir {
			t.Errorf("the recursive listing holds files only: %s", e.path)
		}
		found = found || strings.HasSuffix(e.path, "sub/deep.txt")
	}
	if !found {
		t.Error("the nested file is listed")
	}
}

func TestHiddenFilesShownWhenEnabled(t *testing.T) {
	found := false
	for _, e := range listDir(fbFixture(t), true) {
		found = found || strings.HasSuffix(e.path, ".hidden")
	}
	if !found {
		t.Error("dotfiles are listed with show-hidden")
	}
}

func TestFileBrowserNavigates(t *testing.T) {
	dir := fbFixture(t)
	cfg := DefaultFileBrowserConfig()
	cfg.Directory = dir
	f := NewFileBrowser(cfg)
	st := f.Load(bg)
	if st.Items[0].Display != ".." || st.Items[1].Display != "sub" {
		t.Fatalf("items = %v", st.Items)
	}
	r, ok := f.Activate(bg, launcher.RowTarget(1), launcher.ActivateDefault{}, "").(launcher.ActionReload)
	if !ok || r.State.Items[1].Display != "deep.txt" {
		t.Errorf("entering sub = %#v", r)
	}
	if _, ok := f.Activate(bg, launcher.NoRow, launcher.ActivateCustom{Text: filepath.Join(dir, "missing")}, "").(launcher.ActionNothing); !ok {
		t.Error("a typed path that does not exist does nothing")
	}
}

func TestCalcArithmetic(t *testing.T) {
	calc := func(in string) string {
		v, ok := Eval(in)
		if !ok {
			return "<none>"
		}
		return FormatResult(v)
	}
	for in, want := range map[string]string{
		"1+2": "3", "2+3*4": "14", "(2+3)*4": "20", "10/4": "2.5", "10 % 3": "1", "2^10": "1024",
		"2^3^2": "512", "10-3-2": "5", "-5+3": "-2", "3 - -2": "5", "  1   +   1  ": "2", "0.1+0.2": "0.3",
		".5*4": "2", "sqrt(16)": "4", "round(pi*100)/100": "3.14", "abs(3-10)": "7", "log(1000)": "3",
		"floor(2.7)+ceil(0.2)": "3",
	} {
		if got := calc(in); got != want {
			t.Errorf("%q = %s, want %s", in, got, want)
		}
	}
	for _, in := range []string{"", "1+", "(1+2", "1+2)", "sqrt", "sqrt(", "sqrt(4", "*", "hello", "1 2", "1+*2", "nope(4)", "()", "1/0", "0/0", "ln(-1)"} {
		if got := calc(in); got != "<none>" {
			t.Errorf("%q must not evaluate, got %s", in, got)
		}
	}
	if FormatResult(1.0/3.0) != "0.3333333333" || FormatResult(-7) != "-7" {
		t.Error("formatting")
	}
}

func TestCalcModeReloadsOnlyOnANewAnswer(t *testing.T) {
	m := NewCalc()
	st, changed := m.Query("1+2")
	if !changed || len(st.Items) != 1 || st.Items[0].Display != "= 3" || !st.Items[0].Flags.Has(launcher.FlagPermanent) {
		t.Fatalf("state = %+v", st)
	}
	if _, changed := m.Query("1 + 2"); changed {
		t.Error("the same answer must not reload")
	}
	st, changed = m.Query("1+")
	if !changed || len(st.Items) != 0 {
		t.Error("losing the answer clears the row")
	}
	m.Query("6*7")
	if c, ok := m.Activate(bg, launcher.RowTarget(0), launcher.ActivateDefault{}, "").(launcher.ActionCopy); !ok || c.Text != "42" {
		t.Errorf("accept = %#v", c)
	}
	if s, ok := m.Activate(bg, launcher.RowTarget(0), launcher.ActivateAlt{}, "").(launcher.ActionSetInput); !ok || s.Text != "42" {
		t.Errorf("alt accept = %#v", s)
	}
	if m.AllowsCustom() {
		t.Error("typed text is never the answer")
	}
	if _, ok := NewCalc().Activate(bg, launcher.NoRow, launcher.ActivateDefault{}, "x").(launcher.ActionNothing); !ok {
		t.Error("accepting nothing does nothing")
	}
}

func TestEmojiGlyphs(t *testing.T) {
	if GlyphFromCodes([]uint32{0x1f600}) != "😀" || GlyphFromCodes([]uint32{0x1f1e9, 0x1f1f0}) != "🇩🇰" {
		t.Error("codepoints to glyphs")
	}
	if GlyphFromCodes([]uint32{0x2764, 0}) != "❤️" {
		t.Error("a zero becomes VS16")
	}
	if _, ok := rowToEmoji(nil, "grinning", "grinning", nil, nil); ok {
		t.Error("no codepoints is no emoji")
	}
	if _, ok := rowToEmoji([]uint32{0xd800}, "bad", "bad", nil, nil); ok {
		t.Error("a surrogate is no emoji")
	}
	if _, ok := rowToEmoji([]uint32{0x1f600}, "", "", nil, nil); ok {
		t.Error("an unnamed glyph is no emoji")
	}
}

func TestEmojiNamesAndKeywords(t *testing.T) {
	e, _ := rowToEmoji([]uint32{0x1f37a}, "beer mug", "chope de bière", []string{"beer"}, []string{"bière"})
	if e.Name != "chope de bière" || !reflect.DeepEqual(e.Keywords, []string{"beer mug", "bière", "beer"}) {
		t.Errorf("localized = %+v", e)
	}
	e, _ = rowToEmoji([]uint32{0x1f37a}, "beer mug", "", []string{"beer"}, nil)
	if e.Name != "beer mug" || !reflect.DeepEqual(e.Keywords, []string{"beer"}) {
		t.Errorf("untranslated = %+v", e)
	}
	e, _ = rowToEmoji([]uint32{0x1f600}, "grin", "grin", []string{"face", "smile"}, []string{"face"})
	if !reflect.DeepEqual(e.Keywords, []string{"face", "smile"}) {
		t.Errorf("deduped = %q", e.Keywords)
	}
	item := emojiItem(Emoji{Glyph: "🍺", Name: "beer mug", Keywords: []string{"bar", "drink"}})
	if item.Display != "🍺  beer mug" || !strings.Contains(item.MatchText, "drink") || *item.Info != "🍺" {
		t.Errorf("item = %+v", item)
	}
}

func TestEmojiLanguagesWiden(t *testing.T) {
	for locale, want := range map[string][]string{
		"fr_CA.UTF-8": {"fr-ca", "fr", "en"}, "de_DE@euro": {"de-de", "de", "en"}, "fr": {"fr", "en"},
		"C": {"en"}, "POSIX": {"en"}, "": {"en"}, "en_GB.UTF-8": {"en-gb", "en"},
	} {
		if got := languagesFor(locale); !reflect.DeepEqual(got, want) {
			t.Errorf("%q = %q, want %q", locale, got, want)
		}
	}
}

func TestTheEmbeddedEmojiTableLoads(t *testing.T) {
	t.Setenv("LANG", "C")
	emojis := AvailableEmoji()
	if len(emojis) < 1000 {
		t.Fatalf("only %d emoji", len(emojis))
	}
	found := false
	for _, e := range emojis {
		if strings.HasPrefix(e.Glyph, "😀") {
			found = true
			break
		}
	}
	if !found {
		t.Error("grinning face missing from GTK's table")
	}
	m := NewEmoji()
	if c, ok := m.Activate(bg, launcher.RowTarget(0), launcher.ActivateDefault{}, "").(launcher.ActionCopy); !ok || c.Text != emojis[0].Glyph {
		t.Errorf("accept copies the glyph: %#v", c)
	}
}

// fakeClipboard is a ClipboardHistory double.
type fakeClipboard struct {
	entries []clipboard.Entry
	copied  []uint64
	cleared bool
}

func (f *fakeClipboard) Entries() []clipboard.Entry { return f.entries }

func (f *fakeClipboard) Forget(id uint64) bool {
	for i, e := range f.entries {
		if e.ID == id {
			f.entries = append(f.entries[:i], f.entries[i+1:]...)
			return true
		}
	}
	return false
}

func (f *fakeClipboard) Clear() { f.entries, f.cleared = nil, true }

func (f *fakeClipboard) Copy(id uint64) bool {
	f.copied = append(f.copied, id)
	return true
}

func TestClipboardKindsHaveTheirOwnIcons(t *testing.T) {
	entries := []clipboard.Entry{
		{ID: 1, Mime: "text/plain;charset=utf-8", Bytes: []byte("hi")},
		{ID: 2, Mime: "text/uri-list", Bytes: []byte("file:///a\nfile:///b\n")},
		{ID: 3, Mime: "image/png", Bytes: []byte("\x89PNG")},
		{ID: 4, Mime: "application/pdf", Bytes: []byte("%PDF")},
	}
	seen := map[launcher.Icon]bool{}
	for _, e := range entries {
		seen[iconFor(e)] = true
	}
	if len(seen) != 4 {
		t.Errorf("two kinds share an icon: %v", seen)
	}
	one := iconFor(clipboard.Entry{Mime: "text/uri-list", Bytes: []byte("file:///tmp/a.png\n")})
	if th, ok := one.(launcher.IconThumbnail); !ok || th.Path != "/tmp/a.png" || th.Fallback != iconFiles {
		t.Errorf("one copied file is pictured: %#v", one)
	}
}

func TestClipboardWithoutAHistorySaysSo(t *testing.T) {
	m := NewClipboard(nil)
	st := m.Load(bg)
	if len(st.Items) != 0 || st.Message == nil || !strings.Contains(*st.Message, "wlr-data-control") || !st.NoCustom || m.AllowsCustom() {
		t.Errorf("state = %+v", st)
	}
	if _, ok := m.Activate(bg, launcher.RowTarget(0), launcher.ActivateDefault{}, "").(launcher.ActionNothing); !ok {
		t.Error("activating without a clipboard does nothing")
	}
}

func TestClipboardRestoresAndForgets(t *testing.T) {
	f := &fakeClipboard{entries: []clipboard.Entry{
		{ID: 7, Mime: "text/plain", Bytes: []byte("seven")},
		{ID: 4, Mime: "text/plain", Bytes: []byte("four")},
	}}
	m := NewClipboard(f)
	m.Load(bg)
	if _, ok := m.Activate(bg, launcher.RowTarget(1), launcher.ActivateAlt{}, "").(launcher.ActionClose); !ok || !reflect.DeepEqual(f.copied, []uint64{4}) {
		t.Errorf("restore = %v", f.copied)
	}
	r, ok := m.Activate(bg, launcher.RowTarget(0), launcher.ActivateKbCustom{N: 1}, "").(launcher.ActionReload)
	if !ok || len(r.State.Items) != 1 || r.State.Items[0].Display != "four" {
		t.Errorf("forget one = %#v", r)
	}
	if _, ok := m.Activate(bg, launcher.RowTarget(0), launcher.ActivateKbCustom{N: 2}, "").(launcher.ActionReload); !ok || !f.cleared {
		t.Error("kb-custom-2 forgets everything")
	}
	if _, ok := m.idAt(launcher.RowTarget(5)); ok {
		t.Error("a row past the end resolves to nothing")
	}
}

func TestKeysListsBindingsReadOnly(t *testing.T) {
	m := NewKeys([]launcher.Binding{{Action: "cancel", Keys: "Escape"}})
	st := m.Load(bg)
	if len(st.Items) != 1 || st.Items[0].Display != "kb-cancel: Escape" || !st.Items[0].Flags.Has(launcher.FlagNonselectable) || !st.NoCustom {
		t.Errorf("state = %+v", st)
	}
	if _, ok := m.Activate(bg, launcher.RowTarget(0), launcher.ActivateDefault{}, "").(launcher.ActionNothing); !ok || m.AllowsCustom() {
		t.Error("keys is read-only")
	}
}
