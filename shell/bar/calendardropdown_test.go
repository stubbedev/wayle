package bar

import (
	"testing"
	"time"

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
