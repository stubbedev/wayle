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
}

// worktreeTrailing finds a worktree row's state/actions stack.
func worktreeTrailing(row widget.Widget) *widget.Stack {
	kids := row.(*widget.Box).Children()
	return kids[len(kids)-1].(*widget.Stack)
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
	rows := card.Children()[1].(*widget.Box)
	if len(rows.Children()) != 2 || !rows.Visible() {
		t.Fatalf("wayle rows = %d visible %v", len(rows.Children()), rows.Visible())
	}
	header.OnClick()
	if rows.Visible() || !v.collapsed["wayle"] {
		t.Error("the header did not fold the card")
	}
	// The fold survives a status refresh.
	src.ticks <- struct{}{}
	waitHeadless(t, "the refresh", func() bool { return v.list.Children()[1] != card })
	refreshed := v.list.Children()[1].(*widget.Box).Children()[1].(*widget.Box)
	if refreshed.Visible() {
		t.Error("a refresh reopened the folded card")
	}

	// Hovering a row reveals its actions in place of the state badge.
	row := v.list.Children()[2].(*widget.Box).Children()[1].(*widget.Box).Children()[0].(*widget.Box)
	trailing := worktreeTrailing(row)
	if trailing.Visible() != "state" {
		t.Errorf("at rest = %q", trailing.Visible())
	}
	if row.TooltipText() != "/w/gelm" {
		t.Errorf("tooltip = %q", row.TooltipText())
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

func TestTreemanRowHoverRevealsActions(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	ctx.Treeman = &fakeTreeman{status: treemanFixture()}
	v := treemanDropdown(ctx).(*treemanView)
	defer v.dropdownClosed()
	sz := v.Measure(widget.Constraints{Max: widget.Size{W: 440, H: 2000}})
	v.Arrange(render.Rect{W: 440, H: sz.H})
	row := v.list.Children()[1].(*widget.Box).Children()[1].(*widget.Box).Children()[0].(*widget.Box)
	trailing := worktreeTrailing(row)
	r := &widget.Router{Root: v}
	b := row.Bounds()
	r.Move(widget.Point{X: b.X + 4, Y: b.Y + b.H/2})
	if trailing.Visible() != "actions" {
		t.Errorf("hovered row shows %q, want the actions", trailing.Visible())
	}
	r.Leave()
	if trailing.Visible() != "state" {
		t.Errorf("left row shows %q, want the state badge", trailing.Visible())
	}
}
