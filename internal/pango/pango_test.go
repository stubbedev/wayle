package pango

import "testing"

func TestTranslateCarriesWhatGelmCanRender(t *testing.T) {
	for in, want := range map[string]string{
		"Firefox <span weight='light' size='small'><i>(Web Browser)</i></span>": "Firefox <i>(Web Browser)</i>",
		"<b>bold</b> and <span weight=\"bold\">heavy</span>":                    "<b>bold</b> and <b>heavy</b>",
		"<span foreground='#ff0000'>red</span>":                                 `<span color="#ff0000">red</span>`,
		"<span style='italic' weight='700'>x</span>":                            "<b><i>x</i></b>",
		"<u>under</u> <tt>mono</tt> <small>s</small>":                           "under mono s",
		"Tom &amp; Jerry&#39;s &lt;show&gt;":                                    "Tom &amp; Jerry&apos;s &lt;show&gt;",
		"<span foreground='red'>named</span>":                                   "named",
		"plain":                                                                 "plain",
	} {
		got, ok := Translate(in)
		if !ok || got != want {
			t.Errorf("Translate(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
}

func TestPlainTextFlattens(t *testing.T) {
	got, ok := PlainText("Alacritty <span weight='light'>(Terminal)</span> &amp; &#x41;")
	if !ok || got != "Alacritty (Terminal) & A" {
		t.Errorf("got %q %v", got, ok)
	}
}

func TestMalformedMarkupIsRefused(t *testing.T) {
	for _, in := range []string{"<b>open", "</b>", "<b><i>x</b></i>", "<blink>x</blink>", "a & b", "&nosuch;", "<span weight=bold>x</span>", "<u class='x'>y</u>", "<b"} {
		if _, ok := Translate(in); ok {
			t.Errorf("%q must not parse", in)
		}
	}
}

func TestEscapeIsGlibs(t *testing.T) {
	if got := Escape(`a<b>&'"`); got != "a&lt;b&gt;&amp;&#39;&quot;" {
		t.Errorf("got %q", got)
	}
	back, ok := PlainText(Escape(`it's "x" <y> & z`))
	if !ok || back != `it's "x" <y> & z` {
		t.Errorf("round trip = %q %v", back, ok)
	}
}
