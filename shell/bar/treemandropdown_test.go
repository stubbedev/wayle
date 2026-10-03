package bar

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/treeman"
)

func treemanFixture() *treeman.Status {
	return &treeman.Status{Total: 3, Stable: 2, Failed: 1, Repos: []treeman.Repo{
		{Repo: "wayle", Total: 2, Worktrees: []treeman.Worktree{
			{Branch: "master", Slug: "wayle-master", State: "ready", Bucket: "stable", IsMain: true, Path: "/w/wayle"},
			{Branch: "feat", Slug: "wayle-feat", State: "error", Bucket: "failed", Path: "/w/feat"},
		}},
		{Repo: "gelm", Total: 1, Worktrees: []treeman.Worktree{
			{Branch: "main", State: "ready", Bucket: "stable", IsMain: true, Path: "/w/gelm"},
		}},
	}}
}

func TestTreemanBucketHelpers(t *testing.T) {
	for b, want := range map[treeman.Bucket]string{
		treeman.BucketStable: "success", treeman.BucketUp: "info", treeman.BucketDown: "warning", treeman.BucketFailed: "error",
	} {
		if got := treemanVariant(b); got != want {
			t.Errorf("variant(%v) = %q", b, got)
		}
	}
	if treemanBucketLabel(treeman.BucketDown) != i18n.T("dropdown-treeman-bucket-down") {
		t.Error("bucket label")
	}
}

func TestTreemanDropdownEmpty(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	v := treemanDropdown(ctx).(*treemanView)
	if len(v.list.Children()) != 1 || v.back.Visible() {
		t.Errorf("no service: %d children, back shown %v", len(v.list.Children()), v.back.Visible())
	}
	ctx.Treeman = &fakeTreeman{status: &treeman.Status{}}
	v = treemanDropdown(ctx).(*treemanView)
	defer v.dropdownClosed()
	if _, ok := v.list.Children()[0].(*widget.Box); !ok || len(v.list.Children()) != 1 {
		t.Error("no repos: want only the empty state")
	}
	// The reset confirmation carries the stylesheet's alert primitive,
	// no invented class.
	if !v.confirm.HasClass("alert") || !v.confirm.HasClass("warning") || v.confirm.HasClass("treeman-confirm") {
		t.Errorf("confirm classes %v", v.confirm.Classes())
	}
}

func TestTreemanDropdownListAndCollapse(t *testing.T) {
	src := &fakeTreeman{status: treemanFixture(), ticks: make(chan struct{}, 1)}
	ctx := newTestContext(t, config.Defaults())
	ctx.Treeman = src
	v := treemanDropdown(ctx).(*treemanView)
	defer v.dropdownClosed()

	kids := v.list.Children()
	if len(kids) != 3 {
		t.Fatalf("list = %d children, want the summary and two repo cards", len(kids))
	}
	if chips := kids[0].(*widget.Box).Children(); len(chips) != 2 {
		t.Errorf("summary chips = %d, want ready and failed only", len(chips))
	}
	card := kids[1].(*widget.Box)
	header := card.Children()[0].(*widget.Button)
	fold := card.Children()[1].(*widget.Revealer)
	rows := fold.Child().(*widget.Box)
	if len(rows.Children()) != 2 || !fold.Revealed() {
		t.Fatalf("wayle rows = %d revealed %v", len(rows.Children()), fold.Revealed())
	}
	header.OnClick()
	if fold.Revealed() || !v.collapsed["wayle"] {
		t.Error("the header did not fold the card")
	}
	// The fold survives a status refresh.
	src.ticks <- struct{}{}
	waitHeadless(t, "the refresh", func() bool { return v.list.Children()[1] != card })
	refreshed := v.list.Children()[1].(*widget.Box).Children()[1].(*widget.Revealer)
	if refreshed.Revealed() {
		t.Error("a refresh reopened the folded card")
	}

	// A row is the classed overlay over its treeman-wt row, with the
	// action cluster floating at the trailing edge.
	wtRows := v.list.Children()[2].(*widget.Box).Children()[1].(*widget.Revealer).Child().(*widget.Box)
	row := wtRows.Children()[0].(*widget.Overlay)
	if !row.HasClass("treeman-wt-overlay") {
		t.Errorf("row classes %v", row.Classes())
	}
	under := row.Children()[0].(*widget.Box)
	if !under.HasClass("treeman-wt") {
		t.Error("the overlay does not wrap the treeman-wt row")
	}
	if under.TooltipText() != "/w/gelm" {
		t.Errorf("tooltip = %q", under.TooltipText())
	}
	if cluster, ok := row.Children()[1].(*widget.Box); !ok || !cluster.HasClass("treeman-actions") {
		t.Errorf("the overlay's floating child = %v", row.Children()[1])
	}
}

func TestTreemanDropdownDetailAndActions(t *testing.T) {
	src := &fakeTreeman{status: treemanFixture(), ticks: make(chan struct{}, 1)}
	ctx := newTestContext(t, config.Defaults())
	ctx.Treeman = src
	var toastMu sync.Mutex
	var toasts []string
	ctx.Toast = func(label, _ string) {
		toastMu.Lock()
		toasts = append(toasts, label)
		toastMu.Unlock()
	}
	v := treemanDropdown(ctx).(*treemanView)
	defer v.dropdownClosed()

	v.openDetail("/w/feat")
	if v.pages.Visible() != "detail" || v.title.Text() != "feat" || !v.back.Visible() {
		t.Fatalf("detail: page %q title %q back %v", v.pages.Visible(), v.title.Text(), v.back.Visible())
	}
	fields := v.details.Children()[1].(*widget.Box).Children()
	if len(fields) != 4 {
		t.Errorf("fields = %d, want repo, bucket, slug, path", len(fields))
	}

	// Prepare and teardown run at once; reset asks first.
	v.run(treeman.ActionPrepare, "/w/feat")
	v.confirmReset("/w/feat")
	if v.pages.Visible() != "confirm" {
		t.Fatal("reset did not ask")
	}
	buttons := v.confirm.Children()[2].(*widget.Box).Children()
	buttons[1].(*widget.Button).OnClick() // cancel
	if v.pages.Visible() != "detail" {
		t.Errorf("cancel returned to %q", v.pages.Visible())
	}
	v.confirmReset("/w/feat")
	v.confirm.Children()[2].(*widget.Box).Children()[2].(*widget.Button).OnClick() // accept
	waitHeadless(t, "the actions", func() bool { return len(src.ran()) == 2 })
	if got := src.ran(); !slices.Equal(got, []string{"prepare --worktree /w/feat", "db reset /w/feat"}) {
		t.Errorf("ran = %q", got)
	}

	// A failed action toasts its reason.
	src.mu.Lock()
	src.actionErr = errors.New("busy")
	src.mu.Unlock()
	v.run(treeman.ActionTeardown, "/w/feat")
	waitHeadless(t, "the failure toast", func() bool {
		toastMu.Lock()
		defer toastMu.Unlock()
		return len(toasts) == 1
	})
	if !strings.HasSuffix(toasts[0], ": busy") {
		t.Errorf("toast = %q", toasts[0])
	}

	// The worktree going away drops back to the list.
	next := treemanFixture()
	next.Repos[0].Worktrees = next.Repos[0].Worktrees[:1]
	src.setStatus(next)
	src.ticks <- struct{}{}
	waitHeadless(t, "back to the list", func() bool { return v.pages.Visible() == "list" })
	if v.title.Text() != i18n.T("dropdown-treeman-title") || v.back.Visible() {
		t.Error("the header kept the detail state")
	}
}

func TestTreemanRowHoverPaints(t *testing.T) {
	restore := widget.SetAnimationsInstant(true)
	t.Cleanup(restore)
	ctx := styledContext(t, config.Defaults())
	ctx.Treeman = &fakeTreeman{status: treemanFixture()}
	v := treemanDropdown(ctx).(*treemanView)
	defer v.dropdownClosed()
	ctx.Theme.Attach(v)
	sz := v.Measure(widget.Constraints{Max: widget.Size{W: 440, H: 2000}})
	v.Arrange(render.Rect{W: 440, H: sz.H})

	row := v.list.Children()[1].(*widget.Box).Children()[1].(*widget.Revealer).Child().(*widget.Box).Children()[0].(*widget.Overlay)
	under := row.Children()[0].(*widget.Box)
	r := &widget.Router{Root: v}
	b := row.Bounds()
	// At rest the row paints no background; hovered, the
	// .treeman-wt-overlay:hover rule's bg-elevated reaches it.
	if got := widget.StyleBackground(under); got != 0 {
		t.Errorf("the row at rest paints %#08x", uint32(got))
	}
	r.Move(widget.Point{X: b.X + b.W/2, Y: b.Y + b.H/2})
	if got := widget.StyleBackground(under); got == 0 {
		t.Error("the hovered row got no background from the overlay hover rule")
	}
	// Leaving the pointer drops the hover again.
	r.Leave()
	if got := widget.StyleBackground(under); got != 0 {
		t.Errorf("the row after leave paints %#08x", uint32(got))
	}
}

// TestTreemanDotsAndBadgesDoNotStretch pins the valign centers: the
// status dots and the badges keep their natural cross size inside
// their rows (AppendAligned Center), the dots staying compact
// circles.
func TestTreemanDotsAndBadgesDoNotStretch(t *testing.T) {
	restore := widget.SetAnimationsInstant(true)
	t.Cleanup(restore)
	ctx := styledContext(t, config.Defaults())
	ctx.Treeman = &fakeTreeman{status: treemanFixture()}
	v := treemanDropdown(ctx).(*treemanView)
	defer v.dropdownClosed()
	ctx.Theme.Attach(v)
	sz := v.Measure(widget.Constraints{Max: widget.Size{W: 440, H: 2000}})
	v.Arrange(render.Rect{W: 440, H: sz.H})

	// A worktree row: the dot and the state badge are shorter than the
	// row (the branch line's text sets the height).
	rows := v.list.Children()[1].(*widget.Box).Children()[1].(*widget.Revealer).Child().(*widget.Box)
	row := rows.Children()[0].(*widget.Overlay).Children()[0].(*widget.Box)
	dot := row.Children()[0].(*widget.Box)
	badge := row.Children()[2].(*widget.Label)
	if dot.Bounds().H >= row.Bounds().H {
		t.Errorf("the row dot stretched to %d of %d", dot.Bounds().H, row.Bounds().H)
	}
	if badge.Bounds().H >= row.Bounds().H {
		t.Errorf("the state badge stretched to %d of %d", badge.Bounds().H, row.Bounds().H)
	}
	// The summary chips: the dot stays a compact circle centered in
	// the chip.
	chip := v.list.Children()[0].(*widget.Box).Children()[0].(*widget.Box)
	chipDot := chip.Children()[0].(*widget.Box)
	if chipDot.Bounds().H >= chip.Bounds().H && chip.Bounds().H > 0 {
		t.Errorf("the chip dot stretched to %d of %d", chipDot.Bounds().H, chip.Bounds().H)
	}
	// The detail page: the head dot and the badge stay compact too.
	v.openDetail("/w/feat")
	sz = v.Measure(widget.Constraints{Max: widget.Size{W: 440, H: 2000}})
	v.Arrange(render.Rect{W: 440, H: sz.H})
	head := v.details.Children()[0].(*widget.Box)
	if headDot := head.Children()[0].(*widget.Box); headDot.Bounds().H >= head.Bounds().H {
		t.Errorf("the detail dot stretched to %d of %d", headDot.Bounds().H, head.Bounds().H)
	}
	headBadge := head.Children()[2].(*widget.Label)
	if headBadge.Bounds().H >= head.Bounds().H {
		t.Errorf("the detail badge stretched to %d of %d", headBadge.Bounds().H, head.Bounds().H)
	}
}
