package sharepicker

import (
	"os"
	"testing"

	"github.com/stubbedev/gelm/capture"

	"github.com/stubbedev/wayle/service/sharepreview"
)

// Real previews against a private headless compositor; runs only with
// WAYLE_CAPTURE_HEADLESS=1 and the session variables pointing at it.
func TestHeadlessPreviews(t *testing.T) {
	if os.Getenv("WAYLE_CAPTURE_HEADLESS") == "" {
		t.Skip("WAYLE_CAPTURE_HEADLESS is not set")
	}
	c, err := capture.Connect()
	if err != nil {
		t.Fatal(err)
	}
	infos := outputInfos(c.Outputs())
	_ = c.Close()
	if len(infos) == 0 {
		t.Fatal("no outputs")
	}
	img, err := outputThumb(infos[0].name, 100)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); min(b.Dx(), b.Dy()) != 100 {
		t.Fatalf("preview %v, want the smaller side at 100", b)
	}
	if _, err := outputThumb("NOPE-1", 100); err == nil {
		t.Fatal("a missing output previewed")
	}
	if _, err := windowThumb(sharepreview.Toplevel{Class: "no.such.app", Title: "x"}, 100); err == nil {
		t.Fatal("a missing window previewed")
	}
	_ = fallbackToplevels() // must not fail on a compositor with no windows
}
