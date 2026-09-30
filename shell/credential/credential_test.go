package credential

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

func testFonts(t *testing.T) Fonts {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	return Fonts{Text: face, Clock: face}
}

// TestBuildShape pins the prompt: the secret entry masks and submits
// on Enter, the clock follows ShowClock, the error line starts hidden,
// and the greeter's username entry moves focus to the secret entry.
func TestBuildShape(t *testing.T) {
	var submitted []string
	var focused []widget.Widget
	p := Build(Options{
		Fonts: testFonts(t), Palette: styling.Default(), ShowClock: true, WithUsername: true,
		Focus: func(w widget.Widget) { focused = append(focused, w) },
	}, func(s string) { submitted = append(submitted, s) })
	if p.Entry.Echo() != widget.EchoPassword {
		t.Error("the secret entry must mask its text")
	}
	p.Entry.SetText("hunter2")
	p.Entry.KeyAction(widget.KeyEnter, 0)
	if len(submitted) != 1 || submitted[0] != "hunter2" {
		t.Errorf("submits = %q", submitted)
	}
	if p.Username == nil {
		t.Fatal("WithUsername built no username entry")
	}
	p.Username.KeyAction(widget.KeyEnter, 0)
	if len(focused) != 1 || focused[0] != p.Entry {
		t.Error("Enter in the username entry must move focus to the secret entry")
	}
	if !p.Clock.Visible() || p.Error.Visible() {
		t.Error("clock shown, error hidden at start")
	}
	p.SetMessage("Incorrect password")
	if !p.Error.Visible() || p.Error.Text() != "Incorrect password" {
		t.Error("SetMessage must show the text")
	}
	p.SetMessage("")
	if p.Error.Visible() {
		t.Error("an empty message hides the line")
	}

	lock := Build(Options{Fonts: testFonts(t), Palette: styling.Default()}, func(string) {})
	if lock.Username != nil || lock.Clock.Visible() {
		t.Error("the lock-screen prompt has no username entry, and ShowClock false hides the clock")
	}
}

func TestBlurSmoothsAndKeepsSize(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 21, 5))
	src.Set(10, 2, color.RGBA{255, 255, 255, 255})
	out := Blur(src, 2)
	if out.Rect.Dx() != 21 || out.Rect.Dy() != 5 {
		t.Fatalf("size = %v", out.Rect)
	}
	center := out.RGBAAt(10, 2).R
	side := out.RGBAAt(12, 2).R
	far := out.RGBAAt(0, 2).R
	if center == 255 || side == 0 || far != 0 || side > center {
		t.Errorf("blur profile center=%d side=%d far=%d", center, side, far)
	}
	if same := Blur(src, 0); same.RGBAAt(10, 2).R != 255 {
		t.Error("radius 0 must leave the image untouched")
	}
}

func TestLoadImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bg.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	img, err := LoadImage(path, 0)
	if err != nil || img.Bounds().Dx() != 3 {
		t.Fatalf("LoadImage = %v, %v", img, err)
	}
	if _, err := LoadImage(path, 4); err != nil {
		t.Errorf("blurred load: %v", err)
	}
	if _, err := LoadImage(filepath.Join(t.TempDir(), "absent.png"), 0); err == nil {
		t.Error("a missing file loaded")
	}
	junk := filepath.Join(t.TempDir(), "junk.png")
	_ = os.WriteFile(junk, []byte("not an image"), 0o600)
	if _, err := LoadImage(junk, 0); err == nil {
		t.Error("an undecodable file loaded")
	}
}

func TestBackgroundAndFill(t *testing.T) {
	c, _ := config.ParseHexColor("#102030")
	if got := HexFill(c); got != render.RGB(0x10, 0x20, 0x30) {
		t.Errorf("HexFill = %#x", uint32(got))
	}
	if _, ok := Background(nil, HexFill(c)).(*Fill); !ok {
		t.Error("no image: a solid fill")
	}
	if _, ok := Background(image.NewRGBA(image.Rect(0, 0, 1, 1)), 0).(*widget.Overlay); !ok {
		t.Error("an image: picture plus scrim")
	}
	f := NewFill(render.RGB(0, 0, 0))
	f.Arrange(render.Rect{W: 10, H: 10})
	if f.HitTest(widget.Point{X: 1, Y: 1}) == nil {
		t.Error("a shown blackout swallows the pointer")
	}
	f.SetOn(false)
	if f.HitTest(widget.Point{X: 1, Y: 1}) != nil {
		t.Error("a hidden blackout lets input through")
	}
}
