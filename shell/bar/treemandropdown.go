package bar

import (
	"context"
	"log"
	"strconv"
	"sync"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/treeman"
)

// treemanMaxLabelChars is MAX_LABEL_CHARS: the natural-width cap for
// branch, slug, and path labels.
const treemanMaxLabelChars = 20

// treemanBucketLabel is bucket_label.
func treemanBucketLabel(b treeman.Bucket) string {
	switch b {
	case treeman.BucketUp:
		return i18n.T("dropdown-treeman-bucket-up")
	case treeman.BucketDown:
		return i18n.T("dropdown-treeman-bucket-down")
	case treeman.BucketFailed:
		return i18n.T("dropdown-treeman-bucket-failed")
	}
	return i18n.T("dropdown-treeman-bucket-stable")
}

// treemanVariant is dot_variant: the status-dot and badge color class.
func treemanVariant(b treeman.Bucket) string {
	switch b {
	case treeman.BucketUp:
		return "info"
	case treeman.BucketDown:
		return "warning"
	case treeman.BucketFailed:
		return "error"
	}
	return "success"
}

// treemanView is the treeman dropdown: the repo list (bucket summary
// chips over accordion repo cards of worktree rows) and a worktree's
// detail page, plus the reset confirmation; the header's back button
// returns to the list.
type treemanView struct {
	ctx  ModuleContext
	font render.Font
	px   float64

	*widget.Box
	title   *widget.Label
	back    *widget.Button
	pages   *widget.Stack
	list    *widget.Box
	details *widget.Box
	confirm *widget.Box

	status    *treeman.Status
	detail    string
	collapsed map[string]bool
	// returnTo is the page the confirmation goes back to.
	returnTo string

	once   sync.Once
	cancel context.CancelFunc
}

func treemanDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &treemanView{ctx: ctx, font: font, px: px, collapsed: map[string]bool{}, cancel: func() {}}
	v.Box = widget.NewBox(widget.Column, 0, 0)
	v.AddClass("dropdown", "treeman-dropdown")
	backIcon := widget.NewThemeIcon("ld-arrow-left-symbolic", int(px))
	v.back = dropdownButton(backIcon, "ghost-icon", func() { v.openDetail("") })
	v.back.SetTooltip(i18n.T("dropdown-treeman-back"))
	header, _, title := dropdownHeaderParts(font, px, "ld-layers-symbolic", i18n.T("dropdown-treeman-title"), v.back)
	v.title = title
	v.Append(header, false)

	v.list = widget.NewBox(widget.Column, 0, 0)
	v.list.AddClass("treeman-list")
	v.details = widget.NewBox(widget.Column, 0, 0)
	v.details.AddClass("treeman-list", "treeman-detail")
	// The reset confirmation: the stylesheet's alert primitive (the
	// warning variant, its title and description classes) instead of an
	// invented class. Rust shows a native modal AlertDialog; gelm has
	// no dialog, so the dropdown keeps an in-place page.
	v.confirm = widget.NewBox(widget.Column, 10, 4)
	v.confirm.AddClass("alert", "warning")
	v.pages = widget.NewStack()
	pageSlide(v.pages, ctx.Config)
	v.pages.Add("list", v.list)
	v.pages.Add("detail", v.details)
	v.pages.Add("confirm", v.confirm)
	scroll := dropdownScroll(v.pages, "treeman-scroll")
	// DropdownContent: the content box the stylesheet's .dropdown-content
	// rules hang off (default ink, the section-label family).
	content := widget.NewBox(widget.Column, 0, 0)
	content.AddClass("dropdown-content")
	content.Append(scroll, true)
	v.Append(content, true)

	v.apply(v.read(context.Background()))
	v.follow()
	return v
}

func (v *treemanView) read(ctx context.Context) *treeman.Status {
	if v.ctx.Treeman == nil {
		return nil
	}
	s, err := v.ctx.Treeman.Read(ctx)
	if err != nil {
		log.Printf("treeman: %v", err)
		return nil
	}
	return s
}

// apply is StatusChanged: the list re-renders and the page resyncs.
func (v *treemanView) apply(s *treeman.Status) {
	v.status = s
	v.renderList()
	v.syncPage()
}

// openDetail is OpenDetail and Back ("" is the list).
func (v *treemanView) openDetail(path string) {
	v.detail = path
	v.syncPage()
}

// syncPage is sync_page: the detail page while its worktree exists,
// else back to the list (a teardown finishing under the open page).
func (v *treemanView) syncPage() {
	if v.pages.Visible() == "confirm" {
		return
	}
	if v.detail != "" && v.status != nil {
		if repo, wt, ok := v.status.FindWorktree(v.detail); ok {
			v.title.SetText(wt.Branch)
			v.back.SetVisible(true)
			v.renderDetail(repo, wt)
			v.pages.Show("detail")
			return
		}
	}
	v.detail = ""
	v.title.SetText(i18n.T("dropdown-treeman-title"))
	v.back.SetVisible(false)
	v.pages.Show("list")
}

// dot is a status-dot: the empty classed box whose variant rule
// paints the circle (.status-dot.info/.warning/.error/.success).
func (v *treemanView) dot(b treeman.Bucket) *widget.Box {
	d := widget.NewBox(widget.Row, 0, 0)
	d.AddClass("status-dot", treemanVariant(b))
	return d
}

// dotIn appends a status-dot to a row centered across it, GTK's
// set_valign Center, so the dot keeps its compact circle.
func (v *treemanView) dotIn(row *widget.Box, b treeman.Bucket) {
	row.AppendAligned(v.dot(b), false, widget.AlignCenter)
}

// badge is a small colored label; the .badge rule (or its variant)
// paints both the fill and the ink.
func (v *treemanView) badge(text, variant string) *widget.Label {
	b := widget.NewLabel(v.font, v.px*0.8, text, 0)
	b.AddClass("badge")
	if variant != "" {
		b.AddClass(variant)
	}
	return b
}

// capped is a label whose natural width cannot widen the popover
// (cap_natural_width); its ink comes from the class's rule.
func (v *treemanView) capped(text string, scale float64, class string) *widget.Label {
	l := widget.NewLabel(v.font, v.px*scale, text, 0)
	l.AddClass(class)
	l.SetEllipsize(widget.EllipsizeEnd)
	l.SetMaxWidthChars(treemanMaxLabelChars)
	return l
}

func (v *treemanView) mainBadge() *widget.Label {
	b := widget.NewLabel(v.font, v.px*0.75, i18n.T("dropdown-treeman-main"), 0)
	b.AddClass("treeman-badge", "main")
	return b
}

// renderList is render_list.
func (v *treemanView) renderList() {
	v.list.Clear()
	if v.status == nil || len(v.status.Repos) == 0 {
		empty := emptyState(v.font, v.px, "ld-layers-symbolic", i18n.T("dropdown-treeman-empty-title"), i18n.T("dropdown-treeman-empty-desc"))
		v.list.Append(empty, false)
		return
	}
	v.list.Append(v.summary(), false)
	for _, repo := range v.status.Repos {
		v.list.Append(v.repoCard(repo), false)
	}
}

// summary is the per-bucket chip row; empty buckets are left out.
func (v *treemanView) summary() widget.Widget {
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("treeman-summary")
	s := v.status
	for _, c := range []struct {
		n uint32
		b treeman.Bucket
	}{{s.Stable, treeman.BucketStable}, {s.Up, treeman.BucketUp}, {s.Down, treeman.BucketDown}, {s.Failed, treeman.BucketFailed}} {
		if c.n == 0 {
			continue
		}
		chip := widget.NewBox(widget.Row, 0, 0)
		chip.AddClass("treeman-stat")
		v.dotIn(chip, c.b)
		label := widget.NewLabel(v.font, v.px*0.85, strconv.FormatUint(uint64(c.n), 10)+" "+treemanBucketLabel(c.b), 0)
		label.AddClass("treeman-stat-label")
		chip.Append(label, false)
		row.Append(chip, false)
	}
	return row
}

// repoCard is repo_card: a header folding its worktree rows into a
// slide-down revealer, the fold kept across status refreshes.
func (v *treemanView) repoCard(repo treeman.Repo) widget.Widget {
	card := widget.NewBox(widget.Column, 0, 0)
	card.AddClass("card", "treeman-repo")
	rows := widget.NewBox(widget.Column, 0, 0)
	for _, wt := range repo.Worktrees {
		rows.Append(v.worktreeRow(wt), false)
	}
	expanded := !v.collapsed[repo.Repo]
	// The accordion: a revealer sliding the rows in and out, the Rust
	// Revealer's SlideDown.
	fold := widget.NewRevealer(rows)
	fold.SetTransition(widget.RevealSlideDown)
	fold.SetRevealed(expanded)

	head := widget.NewBox(widget.Row, 0, 0)
	chevron := widget.NewThemeIcon(treemanChevron(expanded), int(v.px))
	chevron.AddClass("treeman-repo-chevron")
	head.Append(chevron, false)
	head.Append(v.capped(repo.Repo, 1, "treeman-repo-name"), true)
	head.AppendAligned(v.badge(strconv.FormatUint(uint64(repo.Total), 10), ""), false, widget.AlignCenter)
	name := repo.Repo
	header := dropdownButton(head, "treeman-repo-header", func() {
		open := !fold.Revealed()
		fold.SetRevealed(open)
		chevron.SetThemeName(treemanChevron(open))
		if open {
			delete(v.collapsed, name)
		} else {
			v.collapsed[name] = true
		}
	})
	card.Append(header, false)
	card.Append(fold, false)
	return card
}

func treemanChevron(expanded bool) string {
	if expanded {
		return "ld-chevron-down-symbolic"
	}
	return "ld-chevron-right-symbolic"
}

// worktreeRow is worktree_row: the dot, branch, main badge, and state
// badge under a treeman-wt-overlay: an Overlay whose second child is
// the action cluster, floating at the trailing edge (halign end,
// valign center). It reserves no width at rest and hits only inside
// its own rect; the hover rules — the row background, the cluster's
// opacity/translateX slide-in, the badge fade — are the stylesheet's
// .treeman-wt-overlay:hover paints, no Go swap.
func (v *treemanView) worktreeRow(wt treeman.Worktree) widget.Widget {
	bucket := treeman.ParseBucket(wt.Bucket)
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("treeman-wt")
	v.dotIn(row, bucket)
	line := widget.NewBox(widget.Row, 0, 0)
	line.AddClass("treeman-wt-info")
	line.Append(v.capped(wt.Branch, 1, "treeman-branch"), true)
	if wt.IsMain {
		line.AppendAligned(v.mainBadge(), false, widget.AlignCenter)
	}
	row.Append(line, true)
	row.SetTooltip(wt.Path)
	row.AppendAligned(v.badge(wt.State, treemanVariant(bucket)), false, widget.AlignCenter)

	overlay := widget.NewOverlay()
	overlay.AddClass("treeman-wt-overlay")
	overlay.Append(row)
	if wt.Path != "" {
		cluster := v.actions(wt.Path)
		cluster.InsertAt(0, v.ghostIcon("ld-info-symbolic", i18n.T("dropdown-treeman-action-info"), func() { v.openDetail(wt.Path) }), false)
		overlay.AppendAligned(cluster, widget.AlignEnd, widget.AlignCenter)
	}
	return overlay
}

func (v *treemanView) ghostIcon(icon, tooltip string, onClick func()) *widget.Button {
	glyph := widget.NewThemeIcon(icon, int(v.px*0.9))
	b := dropdownButton(glyph, "ghost-icon", onClick)
	b.SetTooltip(tooltip)
	return b
}

// actions is Actions::buttons: prepare, reset (confirmed), teardown.
func (v *treemanView) actions(path string) *widget.Box {
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("treeman-actions")
	row.Append(v.ghostIcon("tb-refresh-symbolic", i18n.T("dropdown-treeman-action-prepare"),
		func() { v.run(treeman.ActionPrepare, path) }), false)
	row.Append(v.ghostIcon("ld-rotate-ccw-symbolic", i18n.T("dropdown-treeman-action-reset"),
		func() { v.confirmReset(path) }), false)
	// Deliberately unconfirmed, as in Rust: a worktree is cheap to
	// recreate from its branch.
	row.Append(v.ghostIcon("ld-trash-2-symbolic", i18n.T("dropdown-treeman-action-teardown"),
		func() { v.run(treeman.ActionTeardown, path) }), false)
	return row
}

// run queues the action off the loop; a failure toasts.
func (v *treemanView) run(action treeman.Action, path string) {
	src := v.ctx.Treeman
	if src == nil {
		return
	}
	go func() {
		if err := src.RunAction(context.Background(), action, path); err != nil {
			log.Printf("treeman: action failed: %v", err)
			if v.ctx.Toast != nil {
				v.ctx.Toast(i18n.T("dropdown-treeman-action-failed")+": "+err.Error(), "tb-alert-triangle-symbolic")
			}
		}
	}()
}

// confirmReset is confirm_then_run for reset: the confirmation page
// (the Go stand-in for the Rust AlertDialog) built on the stylesheet's
// alert primitive the view was built with — the warning variant, its
// title and description classes, and a danger accept button — with
// the worktree path, Cancel and Reset.
func (v *treemanView) confirmReset(path string) {
	v.returnTo = v.pages.Visible()
	v.confirm.Clear()
	title := widget.NewLabel(v.font, v.px*1.05, i18n.T("dropdown-treeman-confirm-reset-title"), 0)
	title.SetWrap(true)
	title.AddClass("alert-title")
	v.confirm.Append(title, false)
	detail := widget.NewLabel(v.font, v.px*0.85, path, 0)
	detail.SetWrap(true)
	detail.AddClass("alert-description")
	v.confirm.Append(detail, false)
	buttons := widget.NewBox(widget.Row, 8, 0)
	buttons.Append(widget.NewSpacer(0, 0), true)
	cancel := widget.NewLabel(v.font, v.px, i18n.T("dropdown-treeman-confirm-cancel"), 0)
	buttons.Append(dropdownButton(cancel, "ghost", v.endConfirm), false)
	accept := widget.NewLabel(v.font, v.px, i18n.T("dropdown-treeman-confirm-reset-accept"), 0)
	buttons.Append(dropdownButton(accept, "danger", func() {
		v.endConfirm()
		v.run(treeman.ActionReset, path)
	}), false)
	v.confirm.Append(buttons, false)
	v.pages.Show("confirm")
}

func (v *treemanView) endConfirm() {
	v.pages.Show(v.returnTo)
	v.syncPage()
}

// renderDetail is detail_page.
func (v *treemanView) renderDetail(repo treeman.Repo, wt treeman.Worktree) {
	v.details.Clear()
	bucket := treeman.ParseBucket(wt.Bucket)
	head := widget.NewBox(widget.Row, 0, 0)
	head.AddClass("treeman-detail-head")
	v.dotIn(head, bucket)
	head.Append(v.capped(wt.Branch, 1.05, "treeman-detail-branch"), true)
	if wt.IsMain {
		head.AppendAligned(v.mainBadge(), false, widget.AlignCenter)
	}
	head.AppendAligned(v.badge(wt.State, treemanVariant(bucket)), false, widget.AlignCenter)
	v.details.Append(head, false)

	fields := widget.NewBox(widget.Column, 0, 0)
	fields.AddClass("card", "treeman-detail-fields")
	fields.Append(v.field(i18n.T("dropdown-treeman-detail-repo"), repo.Repo, false), false)
	fields.Append(v.field(i18n.T("dropdown-treeman-detail-bucket"), treemanBucketLabel(bucket), false), false)
	if wt.Slug != "" {
		fields.Append(v.field(i18n.T("dropdown-treeman-detail-slug"), wt.Slug, false), false)
	}
	if wt.Path != "" {
		fields.Append(v.field(i18n.T("dropdown-treeman-detail-path"), wt.Path, true), false)
	}
	v.details.Append(fields, false)
	if wt.Path != "" {
		row := widget.NewBox(widget.Row, 0, 0)
		row.AddClass("treeman-detail-actions")
		row.Append(widget.NewSpacer(0, 0), true)
		row.Append(v.actions(wt.Path), false)
		v.details.Append(row, false)
	}
}

// field is one key/value line; the path wraps instead of truncating.
func (v *treemanView) field(key, value string, wrap bool) widget.Widget {
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("treeman-detail-field")
	k := widget.NewLabel(v.font, v.px*0.85, key, 0)
	k.AddClass("treeman-detail-key")
	row.Append(k, false)
	val := widget.NewLabel(v.font, v.px*0.9, value, 0)
	val.AddClass("treeman-detail-value")
	val.SetMaxWidthChars(treemanMaxLabelChars)
	if wrap {
		val.SetWrap(true)
	} else {
		val.SetEllipsize(widget.EllipsizeEnd)
	}
	row.Append(val, true)
	return row
}

// follow re-renders on every status change until the dropdown closes.
func (v *treemanView) follow() {
	if v.ctx.Treeman == nil {
		return
	}
	life, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	followTicks(v.ctx, life, "treeman", v.ctx.Treeman.Subscribe, v.read, v.apply)
}

// dropdownClosed implements dropdownCloser; reopening lands on the
// list, as the Rust popover's closed handler does.
func (v *treemanView) dropdownClosed() { v.once.Do(func() { v.cancel() }) }
