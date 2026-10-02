package fileuri

import "testing"

func TestPath(t *testing.T) {
	for uri, want := range map[string]string{
		"file:///home/u/bg.png":    "/home/u/bg.png",
		"file://host/srv/bg.png":   "/srv/bg.png",
		"file:///a%2":              "/a%2",
		"file:///a%zz":             "/a%zz",
		"file:///%E2%9C%93%ff.png": "/✓�.png",
		"file:///trailing%41":      "/trailingA",
		"file:///My%20Walls/a%23b": "/My Walls/a#b",
	} {
		if got, ok := Path(uri); !ok || got != want {
			t.Errorf("Path(%q) = %q, %v; want %q", uri, got, ok, want)
		}
	}
	for _, uri := range []string{"https://x/y", "file://host", "/plain"} {
		if _, ok := Path(uri); ok {
			t.Errorf("%q read as a path", uri)
		}
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	if got := FromPath("/home/u/Shot 1#.png"); got != "file:///home/u/Shot%201%23.png" {
		t.Errorf("FromPath = %q", got)
	}
	if got := Encode("Line & stuff~", ""); got != "Line%20%26%20stuff~" {
		t.Errorf("Encode = %q", got)
	}
	for _, s := range []string{"a b/c#d%e", "✓"} {
		if got := DecodeString(Encode(s, "")); got != s {
			t.Errorf("round trip %q = %q", s, got)
		}
	}
}
