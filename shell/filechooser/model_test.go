package filechooser

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/wayle/internal/mime"
)

// testDB is a shared-mime-info fixture with the types the filter tests
// guess.
func testDB(t *testing.T) *mime.Database {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "mime"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"globs2": "50:image/png:*.png\n50:image/jpeg:*.jpg\n50:image/webp:*.webp\n50:image/gif:*.gif\n" +
			"50:text/plain:*.txt\n50:text/markdown:*.md\n50:image/svg+xml:*.svg\n",
		"subclasses": "image/svg+xml application/xml\napplication/xml text/plain\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, "mime", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return mime.Load([]string{dir})
}

func entry(path string, dir bool, size int64) Entry { return Entry{Path: path, IsDir: dir, Size: size} }

func names(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func touch(t *testing.T, root string, rel ...string) {
	t.Helper()
	for _, r := range rel {
		p := filepath.Join(root, r)
		if strings.HasSuffix(r, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHumanSizeScalesByUnit(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 512: "512 B", 1024: "1.0 KB", 1536: "1.5 KB",
		1 << 20: "1.0 MB", 3 << 30: "3.0 GB",
	} {
		if got := HumanSize(n); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatTime(t *testing.T) {
	if got := FormatTime(time.Time{}); got != "" {
		t.Errorf("no time = %q, want empty", got)
	}
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.Local)
	if got := FormatTime(at); got != "2026-03-04 05:06" {
		t.Errorf("FormatTime = %q", got)
	}
}

func TestMatchesFilterHandlesGlobsMIMEAndBounds(t *testing.T) {
	db := testDB(t)
	filters := []Filter{
		{"Images", []Rule{{RuleGlob, "*.png"}, {RuleGlob, "*.jpg"}}},
		{"Text", []Rule{{RuleGlob, "*.txt"}}},
	}
	check := func(name string, fs []Filter, active int, want bool) {
		t.Helper()
		if got := MatchesFilter(db, name, fs, active); got != want {
			t.Errorf("MatchesFilter(%q, %d) = %v, want %v", name, active, got, want)
		}
	}
	check("a.png", filters, 0, true)
	check("a.jpg", filters, 0, true)
	check("a.txt", filters, 0, false)
	check("a.txt", filters, 1, true)
	check("a.png", filters, 1, false)
	check("anything", nil, 0, true)
	check("x.bin", []Filter{{"All", []Rule{{RuleGlob, "*"}}}}, 0, true)
	exact := []Filter{{"Make", []Rule{{RuleGlob, "Makefile"}}}}
	check("Makefile", exact, 0, true)
	check("makefile", exact, 0, true)
	check("Makefile.old", exact, 0, false)
	mimeOnly := []Filter{{"Img", []Rule{{RuleMIME, "image/png"}}}}
	check("photo.png", mimeOnly, 0, true)
	check("notes.txt", mimeOnly, 0, false)
	images := []Filter{{"Images", []Rule{{RuleMIME, "image/*"}}}}
	check("a.png", images, 0, true)
	check("a.jpg", images, 0, true)
	check("a.txt", images, 0, false)
	// A MIME rule takes subtypes: svg is xml is text.
	check("drawing.svg", []Filter{{"Text", []Rule{{RuleMIME, "text/plain"}}}}, 0, true)
	check("x.bin", []Filter{{"Any", nil}}, 0, true)
	check("x", filters, 99, true)
	check("x", filters, -1, true)
}

func TestGTK4SuffixFiltersMatchInAnyCase(t *testing.T) {
	db := testDB(t)
	gtk4 := []Filter{{"Images", []Rule{
		{RuleGlob, "*.[pP][nN][gG]"},
		{RuleGlob, "*.[jJ][pP][gG]"},
		{RuleGlob, "*.gif"},
		{RuleMIME, "image/webp"},
	}}}
	for _, name := range []string{"photo.png", "Photo.PNG", "photo.PnG", "shot.jpg", "SHOT.JPG", "anim.gif", "ANIM.GIF", "sticker.webp"} {
		if !MatchesFilter(db, name, gtk4, 0) {
			t.Errorf("%s should match", name)
		}
	}
	for _, name := range []string{"photo.txt", "notes.md", "png", "photo.png.bak"} {
		if MatchesFilter(db, name, gtk4, 0) {
			t.Errorf("%s should not match", name)
		}
	}
}

func TestGlobMatcherHandlesWildcardsClassesAndNegation(t *testing.T) {
	for _, c := range []struct {
		name, glob string
		want       bool
	}{
		{"archive.tar.gz", "*.tar.*", true},
		{"archive.zip", "*.tar.*", false},
		{"image_1.png", "image_?.png", true},
		{"image_10.png", "image_?.png", false},
		{"1file", "[!a-z]*", true},
		{"afile", "[!a-z]*", false},
		{"report_2024.pdf", "report_*.pdf", true},
		// Unparseable degrades to an exact, case-insensitive compare.
		{"[oops", "[oops", true},
		{"[OOPS", "[oops", true},
		{"other", "[oops", false},
	} {
		if got := matchGlob(c.name, c.glob); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.name, c.glob, got, c.want)
		}
	}
}

func TestTextFilterIsSubstringThenGlob(t *testing.T) {
	plain := newTextFilter("REP")
	if !plain.matches("report.txt") || !plain.matches("my-Report") || plain.matches("notes.txt") {
		t.Error("a plain query is a case-insensitive substring")
	}
	if !newTextFilter("").matches("anything") {
		t.Error("an empty query passes everything")
	}
	globbed := newTextFilter("*.rs")
	if !globbed.matches("main.rs") || !globbed.matches("MAIN.RS") || globbed.matches("main.rs.bak") {
		t.Error("a metacharacter makes the query a whole-name glob")
	}
	// A malformed glob stays a substring.
	if !newTextFilter("[a").matches("x[a]y") {
		t.Error("an unparseable glob query falls back to the substring")
	}
}

func TestMimeExtCoversCommonTypes(t *testing.T) {
	for mimeType, want := range map[string]string{
		"image/png": "png", "image/jpeg": "jpg", "image/svg+xml": "svg", "image/gif": "gif",
		"image/webp": "webp", "video/mp4": "mp4", "video/x-matroska": "mkv",
		"application/pdf": "pdf", "application/zip": "zip", "application/json": "json",
		"text/plain": "txt", "text/csv": "csv", "text/html": "html",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
		"audio/mpeg": "mp3", "audio/flac": "flac", "audio/aac": "aac", "video/quicktime": "mov",
	} {
		if got := mimeExt(mimeType); got != want {
			t.Errorf("mimeExt(%s) = %q, want %q", mimeType, got, want)
		}
	}
	for _, unknown := range []string{"image/*", "application/x-made-up"} {
		if got := mimeExt(unknown); got != "" {
			t.Errorf("mimeExt(%s) = %q, want none", unknown, got)
		}
	}
}

func TestFilterLabelPrefersNameFallsBackToPatterns(t *testing.T) {
	for _, c := range []struct {
		f    Filter
		want string
	}{
		{Filter{"Images", []Rule{{RuleGlob, "*.png"}, {RuleGlob, "*.[Jj][Pp][Gg]"}}}, "Images"},
		{Filter{"", []Rule{{RuleGlob, "*.png"}, {RuleMIME, "image/png"}}}, "*.png"},
		{Filter{" ", []Rule{{RuleMIME, "image/jpeg"}}}, "*.jpg"},
		{Filter{"", []Rule{{RuleMIME, "image/*"}}}, "image/*"},
	} {
		if got := FilterLabel(c.f); got != c.want {
			t.Errorf("FilterLabel(%+v) = %q, want %q", c.f, got, c.want)
		}
	}
}

func TestWithAllFilesAddsTheEscapeHatchOnce(t *testing.T) {
	images := []Filter{{"Images", []Rule{{RuleGlob, "*.png"}}}}
	got := withAllFiles(images)
	if len(got) != 2 || got[1].Name != "All Files" {
		t.Fatalf("restrictive filters = %+v, want All Files appended", got)
	}
	if len(images) != 1 {
		t.Error("appending mutated the request's filters")
	}
	for _, catchAll := range []string{"*", "*.*"} {
		fs := []Filter{{"Any", []Rule{{RuleGlob, catchAll}}}}
		if got := withAllFiles(fs); len(got) != 1 {
			t.Errorf("a %q filter got All Files appended", catchAll)
		}
	}
	if got := withAllFiles(nil); len(got) != 0 {
		t.Errorf("no filters = %+v, want still none", got)
	}
}

func TestSortEntriesKeepsDirsFirst(t *testing.T) {
	entries := []Entry{entry("b.txt", false, 10), entry("Adir", true, 0), entry("a.txt", false, 30), entry("Zdir", true, 0)}
	SortEntries(entries, SortName, true)
	if got := names(entries); !slices.Equal(got, []string{"Adir", "Zdir", "a.txt", "b.txt"}) {
		t.Errorf("name asc = %v", got)
	}
	SortEntries(entries, SortName, false)
	if got := names(entries); !slices.Equal(got, []string{"Zdir", "Adir", "b.txt", "a.txt"}) {
		t.Errorf("name desc = %v (folders stay first)", got)
	}
	SortEntries(entries, SortSize, true)
	if got := names(entries); !entries[0].IsDir || !entries[1].IsDir || !slices.Equal(got[2:], []string{"b.txt", "a.txt"}) {
		t.Errorf("size asc = %v", got)
	}
	kinds := []Entry{entry("b.png", false, 0), entry("a.txt", false, 0), entry("a.png", false, 0)}
	SortEntries(kinds, SortKind, true)
	if got := names(kinds); !slices.Equal(got, []string{"a.png", "b.png", "a.txt"}) {
		t.Errorf("kind asc = %v, want grouped by type then name", got)
	}
	old, newer := entry("old", false, 0), entry("new", false, 0)
	old.Modified, newer.Modified = time.Unix(100, 0), time.Unix(200, 0)
	byTime := []Entry{newer, old}
	SortEntries(byTime, SortModified, true)
	if got := names(byTime); !slices.Equal(got, []string{"old", "new"}) {
		t.Errorf("modified asc = %v", got)
	}
}

func TestEntryKindLabelsFolderExtensionOrFile(t *testing.T) {
	for e, want := range map[Entry]string{
		entry("/x/docs", true, 0):         "Folder",
		entry("/x/a.png", false, 0):       "PNG",
		entry("/x/archive.TAR", false, 0): "TAR",
		entry("/x/Makefile", false, 0):    "File",
	} {
		if got := e.Kind(); got != want {
			t.Errorf("Kind(%s) = %q, want %q", e.Path, got, want)
		}
	}
}

func TestSearchRanksCloseAndWellMatchedFirst(t *testing.T) {
	mk := func(rel string) Entry { return entry("/home/u/"+rel, false, 0) }
	entries := []Entry{mk("a/b/c/d/deep-php-helper.rs"), mk("php.ini"), mk("src/php-config.txt"), mk("vendor/x/y/php")}
	SortSearch(entries, "/home/u", "PHP")
	want := []string{"php", "php.ini", "php-config.txt", "deep-php-helper.rs"}
	if got := names(entries); !slices.Equal(got, want) {
		t.Errorf("search order = %v, want %v", got, want)
	}
}

func TestDetectsRasterImagesByExtension(t *testing.T) {
	for path, want := range map[string]bool{
		"/x/a.png": true, "/x/a.JPG": true, "/x/photo.jpeg": true, "/x/b.webp": true,
		"/x/a.txt": false, "/x/a.svg": false, "/x/noext": false,
	} {
		if got := isRaster(path); got != want {
			t.Errorf("isRaster(%s) = %v", path, got)
		}
	}
}

func TestWalkSearchRecursesAndHonorsHidden(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "top_match.txt", "plain.txt", "sub/nested_match.txt", ".hidden/buried_match.txt", ".secret_match.txt")
	run := func(ctx context.Context, query string, hidden bool) []string {
		var out []string
		WalkSearch(ctx, root, query, hidden, func(batch []string) {
			for _, p := range batch {
				out = append(out, filepath.Base(p))
			}
		})
		slices.Sort(out)
		return out
	}
	live := context.Background()
	if got := run(live, "match", false); !slices.Equal(got, []string{"nested_match.txt", "top_match.txt"}) {
		t.Errorf("hidden excluded = %v", got)
	}
	if got := run(live, "MATCH", false); len(got) != 2 {
		t.Errorf("case-insensitive query = %v", got)
	}
	want := []string{".secret_match.txt", "buried_match.txt", "nested_match.txt", "top_match.txt"}
	if got := run(live, "match", true); !slices.Equal(got, want) {
		t.Errorf("hidden shown = %v, want %v", got, want)
	}
	cancelled, cancel := context.WithCancel(live)
	cancel()
	if got := run(cancelled, "match", false); len(got) != 0 {
		t.Errorf("a cancelled walk emitted %v", got)
	}
}

func TestWalkSearchDoesNotFollowSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "real/match_a")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "real", "loop")); err != nil {
		t.Fatal(err)
	}
	var got []string
	WalkSearch(context.Background(), root, "match", false, func(b []string) { got = append(got, b...) })
	if len(got) != 1 || got[0] != filepath.Join(root, "real", "match_a") {
		t.Errorf("walk through symlinks = %v, want only the real match", got)
	}
}

func TestWalkSearchStreamsBatchesAndCaps(t *testing.T) {
	root := t.TempDir()
	for i := range searchCap + 10 {
		touch(t, root, filepath.Join("d", "m"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+"_"+time.Duration(i).String()))
	}
	var batches []int
	total := WalkSearch(context.Background(), root, "m", false, func(b []string) { batches = append(batches, len(b)) })
	if total != searchCap {
		t.Errorf("found %d, want the cap %d", total, searchCap)
	}
	sum := 0
	for i, n := range batches {
		sum += n
		if n > searchBatch || (i < len(batches)-1 && n != searchBatch) {
			t.Errorf("batch %d holds %d, want %d (the last may be short)", i, n, searchBatch)
		}
	}
	if sum != searchCap {
		t.Errorf("batches carried %d, want %d", sum, searchCap)
	}
}

func TestListDirReturnsEverythingButHonorsFolderMode(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "sub/", "visible.txt", ".hidden.txt")
	got := names(ListDir(root, ModeOpenFile))
	slices.Sort(got)
	if !slices.Equal(got, []string{".hidden.txt", "sub", "visible.txt"}) {
		t.Errorf("listing = %v, want everything", got)
	}
	if got := names(ListDir(root, ModeFolder)); !slices.Equal(got, []string{"sub"}) {
		t.Errorf("folder mode = %v, want only folders", got)
	}
	if got := ListDir(filepath.Join(root, "missing"), ModeOpenFile); got != nil {
		t.Errorf("an unreadable directory = %v, want none", got)
	}
}

func TestUserPlacesListsHomeAndExistingDirs(t *testing.T) {
	home := t.TempDir()
	touch(t, home, "Documents/", "Pictures/", "Music")
	var got []string
	for _, p := range UserPlaces(home) {
		got = append(got, p.Label)
	}
	// Music is a file, not a folder, so it is not offered.
	if !slices.Equal(got, []string{"Home", "Documents", "Pictures"}) {
		t.Errorf("places = %v", got)
	}
	if UserPlaces("") != nil {
		t.Error("no home offered places")
	}
}

func TestOtherLocationsShowsUserVisibleMounts(t *testing.T) {
	mounts := strings.Join([]string{
		"/dev/nvme0n1p2 / ext4 rw 0 0",
		"proc /proc proc rw 0 0",
		"/dev/sda1 /run/media/u/USB\\040STICK vfat rw 0 0",
		"/dev/sda2 /run/media/other/THEIRS vfat rw 0 0",
		"/dev/sdb1 /media/Backup ext4 rw 0 0",
		"/dev/sdb1 /media/Backup ext4 rw 0 0",
		"/dev/sdc1 /home/u/Mounts ext4 rw 0 0",
		"/dev/sdc2 /home/u/.cache/x ext4 rw 0 0",
		"tmpfs /run/user/1000 tmpfs rw 0 0",
	}, "\n")
	var got []string
	for _, p := range OtherLocations(strings.NewReader(mounts), "/home/u", "u") {
		got = append(got, p.Label+"="+p.Path)
	}
	want := []string{"Computer=/", "USB STICK=/run/media/u/USB STICK", "Backup=/media/Backup", "Mounts=/home/u/Mounts"}
	if !slices.Equal(got, want) {
		t.Errorf("locations = %v, want %v", got, want)
	}
	if got := OtherLocations(nil, "/home/u", "u"); len(got) != 1 {
		t.Errorf("no mount table = %v, want just Computer", got)
	}
}

func TestCrumbsCollapseHomeAndDepth(t *testing.T) {
	render := func(cs []Crumb) string {
		var parts []string
		for _, c := range cs {
			s := c.Label + "=" + c.Path
			if c.Current {
				s += "*"
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, " ")
	}
	for _, c := range []struct{ dir, want string }{
		{"/home/u", "Home=/home/u*"},
		{"/home/u/a/b", "Home=/home/u a=/home/u/a b=/home/u/a/b*"},
		{"/", "/=/*"},
		{"/etc/nix", "/=/ etc=/etc nix=/etc/nix*"},
		// A sibling of home sharing its prefix is not under it.
		{"/home/user2", "/=/ home=/home user2=/home/user2*"},
		// Past three segments the deeper ones fold into …, which jumps
		// to the last folded one.
		{"/home/u/a/b/c/d/e", "Home=/home/u …=/home/u/a/b c=/home/u/a/b/c d=/home/u/a/b/c/d e=/home/u/a/b/c/d/e*"},
	} {
		if got := render(Crumbs(c.dir, "/home/u")); got != c.want {
			t.Errorf("Crumbs(%s) = %s\n want %s", c.dir, got, c.want)
		}
	}
}

func TestViewRoundTripsAndDefaults(t *testing.T) {
	for _, v := range []view{defaultView, {SortSize, false, true}, {SortKind, true, false}, {SortModified, false, false}} {
		if got := parseView(v.String()); got != v {
			t.Errorf("round trip %+v = %+v", v, got)
		}
	}
	if got := parseView(""); got != defaultView {
		t.Errorf("empty = %+v, want the default", got)
	}
	if got := parseView("bogus sideways mosaic"); got != defaultView {
		t.Errorf("garbage = %+v, want the default", got)
	}
}

func TestThumbnailIsACentreCroppedSquare(t *testing.T) {
	// 16x8: red left half, blue right half; the centred square crop is
	// columns 4..12, so the thumbnail's left edge is red, right blue.
	src := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			c := color.RGBA{200, 30, 40, 255}
			if x >= 8 {
				c = color.RGBA{20, 60, 220, 255}
			}
			src.Set(x, y, c)
		}
	}
	path := filepath.Join(t.TempDir(), "t.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	got, err := thumbnail(path, 24)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != image.Rect(0, 0, 24, 24) {
		t.Fatalf("bounds = %v, want a 24px square", got.Bounds())
	}
	if l, r := got.RGBAAt(0, 12), got.RGBAAt(23, 12); l.R <= l.B || r.B <= r.R {
		t.Errorf("left %v / right %v, want red then blue", l, r)
	}
	if _, err := thumbnail(filepath.Join(t.TempDir(), "missing.png"), 24); err == nil {
		t.Error("a missing file thumbnailed")
	}
	garbage := filepath.Join(t.TempDir(), "g.png")
	if err := os.WriteFile(garbage, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := thumbnail(garbage, 24); err == nil {
		t.Error("garbage thumbnailed")
	}
	big := filepath.Join(t.TempDir(), "big.png")
	if err := os.WriteFile(big, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, thumbMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := thumbnail(big, 24); !errors.Is(err, errThumbTooLarge) {
		t.Errorf("an oversized file = %v, want errThumbTooLarge", err)
	}
}

// The appended All Files must not land in the caller's spare capacity,
// where the caller's next append would overwrite it.
func TestWithAllFilesDoesNotAliasTheRequest(t *testing.T) {
	request := make([]Filter, 1, 4)
	request[0] = Filter{"Images", []Rule{{RuleGlob, "*.png"}}}
	got := withAllFiles(request)
	_ = append(request, Filter{Name: "Clobber"})
	if got[1].Name != "All Files" {
		t.Errorf("All Files became %q through the request's backing array", got[1].Name)
	}
}
