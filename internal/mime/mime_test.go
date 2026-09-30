package mime

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) *Database {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "mime"), 0o755); err != nil {
		t.Fatal(err)
	}
	globs := "# generated\n" +
		"50:image/png:*.png\n" +
		"50:text/plain:*.txt\n" +
		"60:text/x-readme:README*\n" +
		"50:application/x-compressed-tar:*.tar.gz\n" +
		"45:application/gzip:*.gz\n" +
		"50:text/x-makefile:Makefile\n" +
		"50:text/x-csrc:*.C:cs\n"
	if err := os.WriteFile(filepath.Join(dir, "mime", "globs2"), []byte(globs), 0o600); err != nil {
		t.Fatal(err)
	}
	icons := "image/png:image-x-generic\napplication/x-compressed-tar:package-x-generic\n"
	if err := os.WriteFile(filepath.Join(dir, "mime", "generic-icons"), []byte(icons), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load([]string{dir})
}

func TestTypeByName(t *testing.T) {
	db := fixture(t)
	for name, want := range map[string]string{
		"/home/u/me.png":    "image/png",
		"/home/u/ME.PNG":    "image/png",
		"notes.txt":         "text/plain",
		"src.tar.gz":        "application/x-compressed-tar",
		"plain.gz":          "application/gzip",
		"Makefile":          "text/x-makefile",
		"README.md":         "text/x-readme",
		"code.C":            "text/x-csrc",
		"nothing.unknownxx": Unknown,
	} {
		if got := db.TypeByName(name); got != want {
			t.Errorf("TypeByName(%q) = %q, want %q", name, got, want)
		}
	}
	// A case-sensitive glob does not claim the other case.
	if got := db.TypeByName("code.c"); got == "text/x-csrc" {
		t.Error("a cs glob matched a different case")
	}
}

func TestGenericIcon(t *testing.T) {
	db := fixture(t)
	if got := db.GenericIcon("image/png"); got != "image-x-generic" {
		t.Errorf("listed icon = %q", got)
	}
	if got := db.GenericIcon("text/plain"); got != "text-x-generic" {
		t.Errorf("unlisted type falls back to <media>-x-generic, got %q", got)
	}
	if got := db.GenericIcon("garbage"); got != "text-x-generic-symbolic" {
		t.Errorf("a malformed type = %q", got)
	}
}

func TestAMissingDatabaseGuessesUnknown(t *testing.T) {
	db := Load([]string{t.TempDir()})
	if got := db.TypeByName("a.png"); got != Unknown {
		t.Errorf("no database = %q, want %q", got, Unknown)
	}
}
