package bar

import (
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// build_month_grid: six weeks from the week holding the 1st, starting
// on the configured weekday, with the day states marked.
func TestCalendarGrid(t *testing.T) {
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) // a Thursday
	today := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	sel := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	cells := calendarGrid(month, today, &sel, time.Sunday)
	if len(cells) != 42 {
		t.Fatalf("cells = %d", len(cells))
	}
	// Sunday-first: the grid opens on Sunday September 27.
	if first := cells[0].date; first.Month() != time.September || first.Day() != 27 || cells[0].currentMonth {
		t.Errorf("first cell = %v current %v", first, cells[0].currentMonth)
	}
	if !cells[4].currentMonth || cells[4].date.Day() != 1 {
		t.Errorf("cell 4 = %v, want October 1", cells[4].date)
	}
	var todays, selecteds int
	for _, c := range cells {
		if c.today {
			todays++
			if c.date.Day() != 14 {
				t.Error("today on the wrong day")
			}
		}
		if c.selected {
			selecteds++
		}
		if c.weekend != (c.date.Weekday() == time.Saturday || c.date.Weekday() == time.Sunday) {
			t.Errorf("%v weekend = %v", c.date, c.weekend)
		}
	}
	if todays != 1 || selecteds != 1 {
		t.Errorf("today %d selected %d", todays, selecteds)
	}
	// Monday-first: the grid opens on Monday September 28.
	if first := calendarGrid(month, today, nil, time.Monday)[0].date; first.Day() != 28 || first.Weekday() != time.Monday {
		t.Errorf("monday-first opens on %v", first)
	}
	// A month starting on the week's first day has no leading days.
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // a Monday
	if c := calendarGrid(june, today, nil, time.Monday)[0]; c.date.Day() != 1 || !c.currentMonth {
		t.Errorf("june monday-first opens on %v", c.date)
	}
}

func TestCalendarViewNavigatesAndSelects(t *testing.T) {
	cfg := config.Defaults()
	cfg.Clock.Format = "%I:%M %p"
	cfg.Clock.DropdownShowSeconds = false
	ctx := newTestContext(t, cfg)
	v := calendarDropdown(ctx).(*calendarView)
	thisMonth := calendarMonthLabel(time.Now())
	if got := v.monthLabel.Text(); got != thisMonth {
		t.Errorf("month = %q, want %q", got, thisMonth)
	}
	if !v.ampm.Visible() || v.seconds.Visible() {
		t.Error("12h without seconds: the AM/PM shows and the seconds hide")
	}
	v.step(1)
	if v.monthLabel.Text() == thisMonth {
		t.Error("next month did not move")
	}
	day := time.Now().AddDate(0, 0, 3)
	v.click(day)
	if v.selected == nil {
		t.Fatal("a click did not select")
	}
	v.goToToday()
	if v.monthLabel.Text() != thisMonth || v.selected != nil {
		t.Error("today did not come back and clear the selection")
	}
	// Clicking today clears a selection.
	v.click(day)
	v.click(time.Now())
	if v.selected != nil {
		t.Error("clicking today kept a selection")
	}
	if got := v.dayName.Text(); got != i18n.T(calendarDayIDs[time.Now().Weekday()]) {
		t.Errorf("day name = %q", got)
	}
}

// The calendar matches the Rust tree: the nav buttons sit in a
// .cal-nav box on the header's end and the grid rides in a
// .cal-grid-wrap panel under the grid element name.
func TestCalendarWrapsTheNavAndTheGrid(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	v := calendarDropdown(ctx).(*calendarView)
	section, ok := findByClass(v, "cal-section").(*widget.Box)
	if !ok {
		t.Fatal("no cal-section")
	}
	var header, wrap *widget.Box
	for _, c := range section.Children() {
		if b, isBox := c.(*widget.Box); isBox {
			switch {
			case b.HasClass("cal-header"):
				header = b
			case b.HasClass("cal-grid-wrap"):
				wrap = b
			}
		}
		// Negative: the grid hangs off the wrap, not the bare section.
		if h, hasClass := c.(interface{ HasClass(string) bool }); hasClass && h.HasClass("cal-grid") {
			t.Error("the cal-grid is a direct child of cal-section")
		}
	}
	if header == nil || wrap == nil {
		t.Fatalf("cal-section children missing the header or the wrap (%v, %v)", header, wrap)
	}
	nav, _ := findByClass(header, "cal-nav").(*widget.Box)
	if nav == nil {
		t.Fatal("no cal-nav box: the nav buttons ride the bare header row")
	}
	var navBtns []*widget.Button
	walkTree(nav, func(w widget.Widget) bool {
		if b, isBtn := w.(*widget.Button); isBtn && b.HasClass("cal-nav-btn") {
			navBtns = append(navBtns, b)
		}
		return true
	})
	if len(navBtns) != 3 {
		t.Fatalf("cal-nav buttons = %d, want today and the two chevrons", len(navBtns))
	}
	// The nav buttons paint from the stylesheet (.cal-nav-btn is
	// all:unset): no programmatic pressed shade.
	for _, b := range navBtns {
		if b.BgPressed != 0 {
			t.Errorf("a cal-nav button carries a pressed fill %#08x", uint32(b.BgPressed))
		}
	}
	// The month label sits beside the nav, not inside it.
	for _, c := range nav.Children() {
		if l, isLabel := c.(*widget.Label); isLabel && l.HasClass("cal-month") {
			t.Error("the month label landed in the cal-nav box")
		}
	}
	// The grid rides in the wrap under the grid element name, so the
	// stylesheet's .cal-grid-wrap grid rules reach it.
	grid, isBox := wrap.Children()[0].(*widget.Box)
	if len(wrap.Children()) != 1 || !isBox || !grid.HasClass("cal-grid") {
		t.Fatalf("cal-grid-wrap children = %v, want the cal-grid", wrap.Children())
	}
	if grid.Element() != "grid" {
		t.Errorf("cal-grid element = %q, want grid", grid.Element())
	}
}

// The clock row's state classes follow the clock config: use-12h
// indents the row, show-seconds stops the separator blink.
func TestCalendarClockRowCarriesTheStateClasses(t *testing.T) {
	cfg := config.Defaults()
	cfg.Clock.Format = "%I:%M %p"
	cfg.Clock.DropdownShowSeconds = true
	ctx := newTestContext(t, cfg)
	v := calendarDropdown(ctx).(*calendarView)
	if !v.timeRow.HasClass("use-12h") || !v.timeRow.HasClass("show-seconds") {
		t.Errorf("12h with seconds: classes = %v, want use-12h and show-seconds", v.timeRow.Classes())
	}
	// 24h without seconds drops both.
	cfg.Clock.Format = "%H:%M"
	cfg.Clock.DropdownShowSeconds = false
	v.tick()
	if v.timeRow.HasClass("use-12h") || v.timeRow.HasClass("show-seconds") {
		t.Errorf("24h without seconds: classes = %v, want neither", v.timeRow.Classes())
	}
	if v.ampm.Visible() {
		t.Error("24h renders the ampm")
	}
	// Seconds come back alone; use-12h waits for the format.
	cfg.Clock.DropdownShowSeconds = true
	v.tick()
	if !v.timeRow.HasClass("show-seconds") || v.timeRow.HasClass("use-12h") {
		t.Errorf("24h with seconds: classes = %v, want show-seconds only", v.timeRow.Classes())
	}
}

// The Rust boxes are spacing-0 — the stylesheet's border-spacing and
// margins carry every gap — so the constructors leave no additive
// spacing behind.
func TestCalendarSpacingIsCSSCarried(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	v := calendarDropdown(ctx).(*calendarView)
	sites := map[string]func() int{
		"dropdown root": func() int { return v.Spacing() },
		"dropdown-content": func() int {
			return classedBox(t, v, "dropdown-content").Spacing()
		},
		"clock-hero":    func() int { return classedBox(t, v, "clock-hero").Spacing() },
		"cal-section":   func() int { return classedBox(t, v, "cal-section").Spacing() },
		"cal-header":    func() int { return classedBox(t, v, "cal-header").Spacing() },
		"cal-nav":       func() int { return classedBox(t, v, "cal-nav").Spacing() },
		"cal-grid-wrap": func() int { return classedBox(t, v, "cal-grid-wrap").Spacing() },
		"cal-grid":      func() int { return classedBox(t, v, "cal-grid").Spacing() },
	}
	for what, get := range sites {
		if got := get(); got != 0 {
			t.Errorf("%s spacing = %d, want 0 (the CSS carries it)", what, got)
		}
	}
	grid := classedBox(t, v, "cal-grid")
	for _, row := range grid.Children() {
		if b, isBox := row.(*widget.Box); isBox && b.Spacing() != 0 {
			t.Errorf("a cal-grid row carries spacing %d", b.Spacing())
		}
	}
}
