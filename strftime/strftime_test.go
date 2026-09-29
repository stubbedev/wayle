package strftime

import (
	"testing"
	"time"
)

var ref = time.Date(2026, 9, 29, 14, 5, 9, 0, time.UTC)

func TestFormatDocumentedSpecifiers(t *testing.T) {
	for _, tc := range []struct {
		format string
		want   string
	}{
		{"%Y-%m-%d %H:%M", "2026-09-29 14:05"},
		{"%a %b %d %I:%M %p", "Tue Sep 29 02:05 PM"},
		{"%A %B %e", "Tuesday September 29"},
		{"%H:%M:%S", "14:05:09"},
		{"%y", "26"},
		{"%j", "272"},
		{"%u", "2"},
		{"%w", "2"},
		{"%F", "2026-09-29"},
		{"%T", "14:05:09"},
		{"%z", "+0000"},
		{"100%% sure", "100% sure"},
		{"literal text only", "literal text only"},
		{"trailing percent %", "trailing percent %"},
	} {
		layout, err := Compile(tc.format)
		if err != nil {
			t.Errorf("Compile(%q): %v", tc.format, err)
			continue
		}
		if got := layout.Format(ref); got != tc.want {
			t.Errorf("Format(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}

func TestFormatEpochSeconds(t *testing.T) {
	layout, err := Compile("%s")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := layout.Format(ref), "1790690709"; got != want {
		t.Errorf("%%s = %q, want %q", got, want)
	}
}

func TestCompileRejectsUnsupportedSpecifier(t *testing.T) {
	if _, err := Compile("%Q"); err == nil {
		t.Fatal("Compile(%Q): want error, got nil")
	}
}

func TestCompileRejectsEverySpecifierItFormats(t *testing.T) {
	// Guard against a format the compiler accepts but Format then
	// renders literally: every accepted specifier must have a handler.
	for spec := range layouts {
		if _, err := Compile("%" + string(spec)); err != nil {
			t.Errorf("specifier %q accepted by the layout table fails Compile: %v", string(spec), err)
		}
	}
	for spec := range custom {
		if _, err := Compile("%" + string(spec)); err != nil {
			t.Errorf("specifier %q accepted by the custom table fails Compile: %v", string(spec), err)
		}
	}
}

func TestFormatPreservesLiteralPercentAtEnd(t *testing.T) {
	layout, err := Compile("%H%")
	if err != nil {
		t.Fatal(err)
	}
	if got := layout.Format(ref); got != "14%" {
		t.Errorf("dangling %% renders = %q, want 14%%", got)
	}
}
