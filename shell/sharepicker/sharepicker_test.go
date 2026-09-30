package sharepicker

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/capture"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/sharepreview"
	"github.com/stubbedev/wayle/shell/regionoverlay"
	"github.com/stubbedev/wayle/styling"
)

// testPicker is a picker without an application: the loop marshal is a
// no-op and the compositor queries are canned.
func testPicker(t *testing.T, cfg config.SharePickerConfig, allowToken, multiple bool) (*Picker, chan string) {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	p := &Picker{
		cfg:    cfg,
		style:  Style{Font: face, LabelPx: 12, Palette: styling.Default()},
		invoke: func(func()) {},
		windows: func() []sharepreview.Toplevel {
			return []sharepreview.Toplevel{{ID: 0, Class: "foot", Title: "a", Identifier: "id-a"}, {ID: 1, Class: "kitty", Title: " "}}
		},
		outputs: func() []outputInfo {
			return []outputInfo{{name: "DP-1", width: 1920, height: 1080, scale: 1}, {name: "DP-2", x: 1920, width: 2560, height: 1440, scale: 2}}
		},
	}
	ch := make(chan string, 1)
	p.reply, p.allowToken, p.multiple = ch, allowToken, multiple
	p.root = p.build(nil)
	return p, ch
}

func TestSingleSelectConfirmsAtOnce(t *testing.T) {
	p, ch := testPicker(t, config.DefaultsSharePicker(), false, false)
	p.selectPayload("screen:DP-1")
	if got := <-ch; got != "/screen:DP-1" {
		t.Fatalf("reply %q", got)
	}
	p, ch = testPicker(t, config.DefaultsSharePicker(), true, false)
	p.selectPayload("window:7")
	if got := <-ch; got != "r/window:7" {
		t.Fatalf("token reply %q", got)
	}
	if p.reply != nil {
		t.Fatal("an answered request stays open")
	}
}

func TestMultiSelectTogglesAndConfirms(t *testing.T) {
	p, ch := testPicker(t, config.DefaultsSharePicker(), true, true)
	if p.confirm == nil || p.confirm.Enabled() {
		t.Fatal("multi-select starts without a disabled Share button")
	}
	p.confirmPending()
	select {
	case got := <-ch:
		t.Fatalf("an empty set confirmed %q", got)
	default:
	}
	p.selectPayload("screen:DP-1")
	p.selectPayload("window:3")
	p.selectPayload("region:DP-1@0,0,10,10")
	p.selectPayload("window:3") // toggles back out
	if !p.confirm.Enabled() || p.confirmLbl.Text() != "Share 2 sources" {
		t.Fatalf("button %v %q", p.confirm.Enabled(), p.confirmLbl.Text())
	}
	select {
	case got := <-ch:
		t.Fatalf("a toggle answered %q", got)
	default:
	}
	p.confirmPending()
	if got := <-ch; got != "r/screen:DP-1;region:DP-1@0,0,10,10" {
		t.Fatalf("reply %q", got)
	}
}

func TestCancelAnswersEmpty(t *testing.T) {
	p, ch := testPicker(t, config.DefaultsSharePicker(), true, false)
	p.answer("")
	if got := <-ch; got != "" {
		t.Fatalf("cancel %q", got)
	}
	p.answer("late") // no request open: nothing to answer, no panic
}

func TestPayloads(t *testing.T) {
	if got := windowPayload(sharepreview.Toplevel{ID: 5, Identifier: "abc"}); got != "window:abc" {
		t.Errorf("ext payload %q", got)
	}
	if got := windowPayload(sharepreview.Toplevel{ID: 5}); got != "window:5" {
		t.Errorf("XDPH payload %q", got)
	}
	if got := regionPayload(regionoverlay.Selection{Output: "DP-1", X: 1, Y: 2, Width: 3, Height: 4}); got != "region:DP-1@1,2,3,4" {
		t.Errorf("region payload %q", got)
	}
	if cardTitle(sharepreview.Toplevel{Title: "  ", Class: "kitty"}) != "kitty" || cardTitle(sharepreview.Toplevel{Title: "a", Class: "foot"}) != "a" {
		t.Error("a blank title does not fall back to the class")
	}
	if confirmLabel(0) != "Share" || confirmLabel(1) != "Share" || confirmLabel(3) != "Share 3 sources" {
		t.Error("confirm labels")
	}
}

func TestBuildPagesAndOptions(t *testing.T) {
	cfg := config.DefaultsSharePicker()
	cfg.DefaultPage = config.SharePickerOutputs
	p, _ := testPicker(t, cfg, false, false)
	nb := findNotebook(p.root)
	if nb == nil || nb.SelectedTab() != pageOutputs {
		t.Fatalf("notebook %v", nb)
	}
	if countClass(p.root, "share-picker-restore-button") != 1 {
		t.Error("the restore-token box is missing")
	}
	if p.confirm != nil {
		t.Error("single-select built a Share button")
	}
	cfg.HideTokenRestore = true
	p, _ = testPicker(t, cfg, false, false)
	if countClass(p.root, "share-picker-restore-button") != 0 {
		t.Error("hide-token-restore kept the box")
	}
	if n := countClass(p.windowsPage(nil), "share-picker-card-button") + countClass(p.outputsPage(), "share-picker-card-button"); n != 4 {
		t.Errorf("%d cards, want 2 windows + 2 outputs", n)
	}
}

func TestWindowCardsRunInReverseWithTheirTitles(t *testing.T) {
	p, _ := testPicker(t, config.DefaultsSharePicker(), false, false)
	var titles []string
	walk(p.windowsPage(nil), func(w widget.Widget) {
		if b, ok := w.(*widget.Button); ok && widget.HasClass(b, "share-picker-card-button") {
			titles = append(titles, b.TooltipText())
		}
	})
	// The tooltips carry title and class; the list order is reversed.
	if len(titles) != 2 || titles[0] != " \nkitty" || titles[1] != "a\nfoot" {
		t.Fatalf("titles %v", titles)
	}
}

func TestEmptyPagesShowPlaceholders(t *testing.T) {
	p, _ := testPicker(t, config.DefaultsSharePicker(), false, false)
	p.windows = func() []sharepreview.Toplevel { return nil }
	p.outputs = func() []outputInfo { return nil }
	p.root = p.build(nil)
	if n := countClass(p.windowsPage(nil), "share-picker-placeholder") + countClass(p.outputsPage(), "share-picker-placeholder"); n != 2 {
		t.Fatalf("%d placeholders, want 2", n)
	}
}

func TestOutputInfosAndScaling(t *testing.T) {
	infos := outputInfos([]capture.Output{
		{Name: "DP-1", Width: 1920, Height: 1080, Scale: 1},
		{Name: "DP-2", X: 1920, Width: 2560, Height: 1440, Scale: 2, Transform: capture.Transform90},
		{Name: "", Width: 800, Height: 600}, // unnamed: skipped
		{Name: "DP-3", Width: 0, Height: 0}, // no mode: skipped
	})
	if len(infos) != 2 {
		t.Fatalf("infos %+v", infos)
	}
	if infos[1].width != 1440 || infos[1].height != 2560 {
		t.Fatalf("rotation did not swap the axes: %+v", infos[1])
	}
	applyScaling(infos)
	if infos[0].width != 1920 || infos[1].width != 720 || infos[1].height != 1280 {
		t.Fatalf("scaled %+v", infos)
	}
}

func TestMonitorAreaPlacement(t *testing.T) {
	slots := []outputSlot{{x: 0, y: 0, width: 1920, height: 1080}, {x: 1920, y: 0, width: 1920, height: 1080}}
	a := newMonitorArea(slots)
	if a.width != 3840 || a.height != 1080 {
		t.Fatalf("area %+v", a)
	}
	// A wide map in a squarer box fits by width and centers vertically.
	r := a.place(slots[1], 1000, 500)
	if r.X != 500 || r.W != 500 || r.H != 281 || r.Y != 109 {
		t.Fatalf("place %+v", r)
	}
	left, top, right, bottom := a.margins(slots[0], 6)
	if left != 0 || right != 6 || top != 0 || bottom != 0 {
		t.Fatalf("margins %d %d %d %d", left, top, right, bottom)
	}
	if gridColumns(2, 3, 4) != 3 || gridColumns(10, 3, 4) != 4 || gridColumns(0, 0, 0) != 1 {
		t.Error("gridColumns")
	}
}

type fakePicker struct{ reply string }

func (f fakePicker) Pick(list string, token, multi bool) string {
	return f.reply + "|" + list + "|" + boolStr(token) + boolStr(multi)
}

func boolStr(b bool) string {
	if b {
		return "t"
	}
	return "f"
}

func TestDaemonOverTheBus(t *testing.T) {
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("no dbus-daemon")
	}
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	server, err := dbus.Connect(strings.TrimSpace(addr))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := dbus.Connect(strings.TrimSpace(addr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	release, err := (&Daemon{picker: fakePicker{reply: "/screen:DP-1"}}).Export(server)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	got, err := NewClient(client).Pick(context.Background(), "1[HC>]a[HT>]b[HE>]", true, false)
	if err != nil || got != "/screen:DP-1|1[HC>]a[HT>]b[HE>]|tf" {
		t.Fatalf("Pick = %q %v", got, err)
	}
	if _, err := (&Daemon{picker: fakePicker{}}).Export(client); err == nil {
		t.Fatal("a second daemon claimed the owned name")
	}
}

// walk visits the tree depth first.
func walk(w widget.Widget, fn func(widget.Widget)) {
	fn(w)
	if c, ok := w.(interface{ Children() []widget.Widget }); ok {
		for _, k := range c.Children() {
			walk(k, fn)
		}
	}
}

func countClass(root widget.Widget, class string) int {
	n := 0
	walk(root, func(w widget.Widget) {
		if widget.HasClass(w, class) {
			n++
		}
	})
	return n
}

func findNotebook(root widget.Widget) *widget.Notebook {
	var nb *widget.Notebook
	walk(root, func(w widget.Widget) {
		if n, ok := w.(*widget.Notebook); ok {
			nb = n
		}
	})
	return nb
}
