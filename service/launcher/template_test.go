package launcher

import (
	"reflect"
	"testing"
)

var templateLookup = Values(map[string]string{
	"name":    "Firefox",
	"generic": "Web Browser",
	"empty":   "",
})

func TestRenderReplacesPlaceholders(t *testing.T) {
	if got := Render("{name}!", templateLookup); got != "Firefox!" {
		t.Errorf("got %q", got)
	}
}

func TestOptionalBlocks(t *testing.T) {
	if got := Render("{name} [({generic})]", templateLookup); got != "Firefox (Web Browser)" {
		t.Errorf("a filled block is kept: %q", got)
	}
	for _, tpl := range []string{"{name} [({empty})]", "{name} [({missing})]"} {
		if got := Render(tpl, templateLookup); got != "Firefox " {
			t.Errorf("an unfilled block is dropped: %q -> %q", tpl, got)
		}
	}
}

func TestUnknownPlaceholderRendersEmpty(t *testing.T) {
	if got := Render("a{missing}b", templateLookup); got != "ab" {
		t.Errorf("got %q", got)
	}
}

func TestAnArgvPlaceholderIsOneArgument(t *testing.T) {
	song := Values(map[string]string{"entry": "my song.mp3"})
	if got := RenderArgv("play {entry} --loop", song); !reflect.DeepEqual(got, []string{"play", "my song.mp3", "--loop"}) {
		t.Errorf("a space in the value must not split: %q", got)
	}
	if got := RenderArgv(`play "{entry}"`, song); !reflect.DeepEqual(got, []string{"play", "my song.mp3"}) {
		t.Errorf("quoting is optional, not load-bearing: %q", got)
	}
	quoted := Values(map[string]string{"entry": "it's here"})
	if got := RenderArgv("preview {entry}", quoted); !reflect.DeepEqual(got, []string{"preview", "it's here"}) {
		t.Errorf("a quote in the value must not break the command: %q", got)
	}
}

func TestAnUnparseableTemplateYieldsNoArgv(t *testing.T) {
	if got := RenderArgv("preview '{entry}", templateLookup); got != nil {
		t.Errorf("unbalanced quote = %q", got)
	}
	if got := RenderArgv("", templateLookup); len(got) != 0 {
		t.Errorf("empty template = %q", got)
	}
}
