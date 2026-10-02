package bar

import (
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// pagedPanel is a w=200 panel over a column holding one expanding page
// stack (sized to its visible page) with a 100px page and a tall one.
func pagedPanel(h int, tall widget.Widget) (*panelBox, *widget.Stack) {
	s := widget.NewStack().Add("short", widget.NewSpacer(10, 100)).Add("tall", tall)
	s.SetHomogeneous(false)
	col := widget.NewBox(widget.Column, 0, 0)
	col.Append(s, true)
	return newPanelBox(200, h, col), s
}

func panelLayout(p *panelBox) (surface int, card render.Rect) {
	sz := p.Measure(widget.Constraints{Max: widget.Size{W: 1000, H: 2000}})
	p.Arrange(render.Rect{W: sz.W, H: sz.H})
	return sz.H, p.child.(interface{ Bounds() render.Rect }).Bounds()
}

func TestPanelReservesTheTallestPageAndGrowsTheCard(t *testing.T) {
	p, s := pagedPanel(300, widget.NewSpacer(10, 500))
	surface, card := panelLayout(p)
	if surface != 500 || card.H != 300 || card.Y != 0 {
		t.Errorf("short page: surface %d card %+v, want 500 reserved and a 300 card at the top", surface, card)
	}
	s.Show("tall")
	surface, card = panelLayout(p)
	if surface != 500 || card.H != 500 {
		t.Errorf("tall page: surface %d card %+v, want the card grown to 500 in the same surface", surface, card)
	}
}

func TestPanelIgnoresPagesThatScroll(t *testing.T) {
	p, s := pagedPanel(300, widget.NewScroll(widget.NewSpacer(10, 900)))
	s.Show("tall")
	surface, card := panelLayout(p)
	if surface != 300 || card.H != 300 {
		t.Errorf("scrolling page: surface %d card %+v, want the floor (the list scrolls)", surface, card)
	}
	// Beside a page that does need 500, the list still asks only for
	// the floor: the surface reserves 500, the card stays 300.
	s.Add("form", widget.NewSpacer(10, 500))
	surface, card = panelLayout(p)
	if surface != 500 || card.H != 300 {
		t.Errorf("list beside a tall form: surface %d card %+v, want 500 reserved and the 300 card", surface, card)
	}
}

func TestContentSizedPanelFollowsTheVisiblePage(t *testing.T) {
	p, s := pagedPanel(-1, widget.NewSpacer(10, 500))
	surface, card := panelLayout(p)
	if surface != 500 || card.H != 100 {
		t.Errorf("content-sized: surface %d card %+v, want 500 reserved and the 100 page", surface, card)
	}
	s.Show("tall")
	if _, card = panelLayout(p); card.H != 500 {
		t.Errorf("content-sized tall: card %+v, want 500", card)
	}
}

func TestPanelAnchorsAtTheBarAndTheRestCloses(t *testing.T) {
	p, _ := pagedPanel(300, widget.NewSpacer(10, 500))
	p.bottom = true
	_, card := panelLayout(p)
	if card.Y != 200 {
		t.Errorf("bottom bar: card %+v, want it at the surface's bottom", card)
	}
	pop := &fakePopover{}
	p.attachPopover(pop)
	if hit := p.HitTest(widget.Point{X: 10, Y: 50}); hit != widget.Widget(p) {
		t.Fatalf("the transparent rest hit %T, want the panel", hit)
	}
	p.ClickAt(widget.Point{X: 10, Y: 50})
	if pop.dismissed != 1 {
		t.Errorf("a click on the rest dismissed %d times, want once", pop.dismissed)
	}
	if hit := p.HitTest(widget.Point{X: 10, Y: 250}); hit == widget.Widget(p) {
		t.Error("a click on the card fell through to the panel")
	}
}
