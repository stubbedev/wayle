package bar

import (
	"context"
	"log"
	"strconv"
	"sync"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
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

// treemanVariantToken colors a variant class programmatically.
var treemanVariantToken = map[string]config.CssToken{
	"success": config.TokenStatusSuccess,
	"info":    config.TokenStatusInfo,
	"warning": config.TokenStatusWarning,
	"error":   config.TokenStatusError,
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
	v.Box = widget.NewBox(widget.Column, 10, 14)
	v.AddClass("dropdown", "treeman-dropdown")
	backIcon := widget.NewThemeIcon("ld-arrow-left-symbolic", int(px))
	backIcon.SetTint(ctx.Style.fg)
	v.back = dropdownButton(ctx, backIcon, "ghost-icon", func() { v.openDetail("") })
	v.back.SetTooltip(i18n.T("dropdown-treeman-back"))
	header, _, title := dropdownHeaderParts(ctx, font, px, "ld-layers-symbolic", i18n.T("dropdown-treeman-title"), v.back)
	v.title = title
	v.Append(header, false)

	v.list = widget.NewBox(widget.Column, 8, 0)
	v.list.AddClass("treeman-list")
	v.details = widget.NewBox(widget.Column, 8, 0)
	v.details.AddClass("treeman-list")
	v.confirm = widget.NewBox(widget.Column, 10, 4)
	v.confirm.AddClass("treeman-confirm")
	v.pages = widget.NewStack()
	pageSlide(v.pages, ctx.Config)
	v.pages.Add("list", v.list)
	v.pages.Add("detail", v.details)
	v.pages.Add("confirm", v.confirm)
	scroll := dropdownScroll(v.pages, "treeman-scroll")
	v.Append(scroll, true)

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

func (v *treemanView) tint(variant string) render.Color {
	return tokenColor(v.ctx.Style.palette, treemanVariantToken[variant])
}

// dot is a status-dot in the bucket's color.
func (v *treemanView) dot(b treeman.Bucket) *widget.Label {
	variant := treemanVariant(b)
	d := widget.NewLabel(v.font, v.px*0.7, "●", v.tint(variant))
	d.AddClass("status-dot", variant)
	return d
}

// badge is a small colored label.
func (v *treemanView) badge(text, variant string) *widget.Label {
	color := mutedFg(v.ctx.Style.palette)
	if variant != "" {
		color = v.tint(variant)
	}
	b := widget.NewLabel(v.font, v.px*0.8, text, color)
	b.AddClass("badge")
	if variant != "" {
		b.AddClass(variant)
	}
	return b
}

// capped is a label whose natural width cannot widen the popover
// (cap_natural_width).
func (v *treemanView) capped(text string, scale float64, color render.Color, class string) *widget.Label {
	l := widget.NewLabel(v.font, v.px*scale, text, color)
	l.AddClass(class)
	l.SetEllipsize(widget.EllipsizeEnd)
	l.SetMaxWidthChars(treemanMaxLabelChars)
	return l
}

func (v *treemanView) mainBadge() *widget.Label {
	b := widget.NewLabel(v.font, v.px*0.75, i18n.T("dropdown-treeman-main"), tokenColor(v.ctx.Style.palette, config.TokenAccent))
	b.AddClass("treeman-badge", "main")
	return b
}

// renderList is render_list.
func (v *treemanView) renderList() {
	v.list.Clear()
	if v.status == nil || len(v.status.Repos) == 0 {
		empty := emptyState(v.ctx, v.font, v.px, "ld-layers-symbolic",
			i18n.T("dropdown-treeman-empty-title"), i18n.T("dropdown-treeman-empty-desc"))
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
	row := widget.NewBox(widget.Row, 12, 0)
	row.AddClass("treeman-summary")
	s := v.status
	for _, c := range []struct {
		n uint32
		b treeman.Bucket
	}{{s.Stable, treeman.BucketStable}, {s.Up, treeman.BucketUp}, {s.Down, treeman.BucketDown}, {s.Failed, treeman.BucketFailed}} {
		if c.n == 0 {
			continue
		}
		chip := widget.NewBox(widget.Row, 4, 0)
		chip.AddClass("treeman-stat")
		chip.Append(v.dot(c.b), false)
		label := widget.NewLabel(v.font, v.px*0.85, strconv.FormatUint(uint64(c.n), 10)+" "+treemanBucketLabel(c.b), v.ctx.Style.fg)
		label.AddClass("treeman-stat-label")
		chip.Append(label, false)
		row.Append(chip, false)
	}
	return row
}

// repoCard is repo_card: a header folding the worktree rows, the fold
// kept across status refreshes.
func (v *treemanView) repoCard(repo treeman.Repo) widget.Widget {
	card := widget.NewBox(widget.Column, 4, 8)
	card.AddClass("card", "treeman-repo")
	rows := widget.NewBox(widget.Column, 2, 0)
	for _, wt := range repo.Worktrees {
		rows.Append(v.worktreeRow(wt), false)
	}
	expanded := !v.collapsed[repo.Repo]
	rows.SetVisible(expanded)

	head := widget.NewBox(widget.Row, 8, 0)
	chevron := widget.NewThemeIcon(treemanChevron(expanded), int(v.px))
	chevron.SetTint(mutedFg(v.ctx.Style.palette))
	chevron.AddClass("treeman-repo-chevron")
	head.Append(chevron, false)
	head.Append(v.capped(repo.Repo, 1, v.ctx.Style.fg, "treeman-repo-name"), true)
	head.Append(v.badge(strconv.FormatUint(uint64(repo.Total), 10), ""), false)
	name := repo.Repo
	header := dropdownButton(v.ctx, head, "treeman-repo-header", func() {
		open := !rows.Visible()
		rows.SetVisible(open)
		chevron.SetThemeName(treemanChevron(open))
		if open {
			delete(v.collapsed, name)
		} else {
			v.collapsed[name] = true
		}
	})
	card.Append(header, false)
	card.Append(rows, false)
	return card
}

func treemanChevron(expanded bool) string {
	if expanded {
		return "ld-chevron-down-symbolic"
	}
	return "ld-chevron-right-symbolic"
}

// worktreeRow is worktree_row: the dot, branch, main badge, and state
// badge; hovering the row swaps the badge for the action cluster (the
// overlay that reserves no width at rest).
func (v *treemanView) worktreeRow(wt treeman.Worktree) widget.Widget {
	bucket := treeman.ParseBucket(wt.Bucket)
	row := widget.NewBox(widget.Row, 8, 4)
	row.AddClass("treeman-wt")
	row.Append(v.dot(bucket), false)
	line := widget.NewBox(widget.Row, 6, 0)
	line.AddClass("treeman-wt-info")
	line.Append(v.capped(wt.Branch, 1, v.ctx.Style.fg, "treeman-branch"), true)
	if wt.IsMain {
		line.Append(v.mainBadge(), false)
	}
	row.Append(line, true)
	row.SetTooltip(wt.Path)
	trailing := widget.NewStack()
	trailing.Add("state", v.badge(wt.State, treemanVariant(bucket)))
	if wt.Path != "" {
		cluster := v.actions(wt.Path)
		info := v.ghostIcon("ld-info-symbolic", i18n.T("dropdown-treeman-action-info"), func() { v.openDetail(wt.Path) })
		cluster.InsertAt(0, info, false)
		trailing.Add("actions", cluster)
		row.SetOnHoverWithin(func(on bool) {
			if on {
				trailing.Show("actions")
			} else {
				trailing.Show("state")
			}
		})
	}
	trailing.Show("state")
	row.Append(trailing, false)
	return row
}

func (v *treemanView) ghostIcon(icon, tooltip string, onClick func()) *widget.Button {
	glyph := widget.NewThemeIcon(icon, int(v.px*0.9))
	glyph.SetTint(mutedFg(v.ctx.Style.palette))
	b := dropdownButton(v.ctx, glyph, "ghost-icon", onClick)
	b.SetTooltip(tooltip)
	return b
}

// actions is Actions::buttons: prepare, reset (confirmed), teardown.
func (v *treemanView) actions(path string) *widget.Box {
	row := widget.NewBox(widget.Row, 2, 0)
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
// (the Rust modal) with the worktree path, Cancel and Reset.
func (v *treemanView) confirmReset(path string) {
	v.returnTo = v.pages.Visible()
	v.confirm.Clear()
	title := widget.NewLabel(v.font, v.px*1.05, i18n.T("dropdown-treeman-confirm-reset-title"), v.ctx.Style.fg)
	title.SetWrap(true)
	v.confirm.Append(title, false)
	detail := widget.NewLabel(v.font, v.px*0.85, path, mutedFg(v.ctx.Style.palette))
	detail.SetWrap(true)
	v.confirm.Append(detail, false)
	buttons := widget.NewBox(widget.Row, 8, 0)
	buttons.Append(widget.NewSpacer(0, 0), true)
	cancel := widget.NewLabel(v.font, v.px, i18n.T("dropdown-treeman-confirm-cancel"), v.ctx.Style.fg)
	buttons.Append(dropdownButton(v.ctx, cancel, "treeman-confirm-cancel", v.endConfirm), false)
	accept := widget.NewLabel(v.font, v.px, i18n.T("dropdown-treeman-confirm-reset-accept"),
		tokenColor(v.ctx.Style.palette, config.TokenStatusError))
	buttons.Append(dropdownButton(v.ctx, accept, "treeman-confirm-accept", func() {
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
	head := widget.NewBox(widget.Row, 8, 0)
	head.AddClass("treeman-detail-head")
	head.Append(v.dot(bucket), false)
	head.Append(v.capped(wt.Branch, 1.05, v.ctx.Style.fg, "treeman-detail-branch"), true)
	if wt.IsMain {
		head.Append(v.mainBadge(), false)
	}
	head.Append(v.badge(wt.State, treemanVariant(bucket)), false)
	v.details.Append(head, false)

	fields := widget.NewBox(widget.Column, 6, 10)
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
	row := widget.NewBox(widget.Row, 10, 0)
	row.AddClass("treeman-detail-field")
	k := widget.NewLabel(v.font, v.px*0.85, key, mutedFg(v.ctx.Style.palette))
	k.AddClass("treeman-detail-key")
	row.Append(k, false)
	val := widget.NewLabel(v.font, v.px*0.9, value, v.ctx.Style.fg)
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
