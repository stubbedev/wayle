package appicons

import "testing"

func app(id string) Window { return Window{AppID: id, HasAppID: true} }

// The cases are niri helpers.rs's resolve_app_icon tests.
func TestResolveUnprefixedMatchesAppID(t *testing.T) {
	user := map[string]string{"*firefox*": "user-firefox"}
	if got := Resolve(app("org.mozilla.firefox"), user, "fb"); got != "user-firefox" {
		t.Errorf("got %q", got)
	}
}

func TestResolveAppPrefixMatchesAppID(t *testing.T) {
	user := map[string]string{"app:*firefox*": "user-firefox"}
	if got := Resolve(app("org.mozilla.firefox"), user, "fb"); got != "user-firefox" {
		t.Errorf("got %q", got)
	}
}

func TestResolveTitlePrefixMatchesTitle(t *testing.T) {
	user := map[string]string{"title:*YouTube*": "yt"}
	w := Window{AppID: "firefox", HasAppID: true, Title: "Music - YouTube", HasTitle: true}
	if got := Resolve(w, user, "fb"); got != "yt" {
		t.Errorf("got %q", got)
	}
	// A title pattern never matches the app id.
	if got := Resolve(app("YouTube"), map[string]string{"title:YouTube": "yt"}, "fb"); got == "yt" {
		t.Error("title: pattern matched the app id")
	}
}

func TestResolveTitleTakesPriorityOverApp(t *testing.T) {
	user := map[string]string{"app:firefox": "app-icon", "title:*Docs*": "title-icon"}
	w := Window{AppID: "firefox", HasAppID: true, Title: "Docs", HasTitle: true}
	if got := Resolve(w, user, "fb"); got != "title-icon" {
		t.Errorf("got %q", got)
	}
}

func TestResolveFallsBack(t *testing.T) {
	if got := Resolve(app("zz-unknown-app"), nil, "fb"); got != "fb" {
		t.Errorf("got %q", got)
	}
	if got := Resolve(Window{}, map[string]string{"*": "any"}, "fb"); got != "fb" {
		t.Errorf("missing app id: got %q, want the fallback", got)
	}
}

func TestResolveBuiltinDefaultAndUserOverride(t *testing.T) {
	if got := Resolve(app("firefox"), nil, "fb"); got != "si-firefox-symbolic" {
		t.Errorf("builtin: got %q", got)
	}
	if got := Resolve(app("firefox"), map[string]string{"firefox": "mine"}, "fb"); got != "mine" {
		t.Errorf("user override: got %q", got)
	}
	// The builtin table is case-sensitive, as the wildcard crate is.
	if got := Resolve(app("Firefox"), nil, "fb"); got == "si-firefox-symbolic" {
		t.Error("builtin matched with different case")
	}
}
