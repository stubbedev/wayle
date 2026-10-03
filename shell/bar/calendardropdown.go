package bar

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/strftime"
)

// calendarGridCells is GRID_CELLS: six weeks, every month.
const calendarGridCells = 42

// calendarCell is one day of the grid (helpers.rs DayCell).
type calendarCell struct {
	date                         time.Time
	currentMonth, today, weekend bool
	selected                     bool
}

// calendarGrid is build_month_grid: 42 days from the week containing
// the 1st, starting on weekStart.
func calendarGrid(month, today time.Time, selected *time.Time, weekStart time.Weekday) []calendarCell {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, month.Location())
	leading := (int(first.Weekday()) - int(weekStart) + 7) % 7
	start := first.AddDate(0, 0, -leading)
	cells := make([]calendarCell, calendarGridCells)
	for i := range cells {
		d := start.AddDate(0, 0, i)
		cells[i] = calendarCell{
			date:         d,
			currentMonth: d.Month() == first.Month(),
			today:        sameDay(d, today),
			weekend:      d.Weekday() == time.Saturday || d.Weekday() == time.Sunday,
			selected:     selected != nil && sameDay(d, *selected),
		}
	}
	return cells
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// calendarWeekStart maps calendar-weekday-start.
func calendarWeekStart(w config.WeekStart) time.Weekday {
	switch w {
	case config.WeekStartMonday:
		return time.Monday
	case config.WeekStartTuesday:
		return time.Tuesday
	case config.WeekStartWednesday:
		return time.Wednesday
	case config.WeekStartThursday:
		return time.Thursday
	case config.WeekStartFriday:
		return time.Friday
	case config.WeekStartSaturday:
		return time.Saturday
	}
	return time.Sunday
}

var (
	calendarWeekdayIDs = [7]string{"cal-weekday-sun", "cal-weekday-mon", "cal-weekday-tue", "cal-weekday-wed", "cal-weekday-thu", "cal-weekday-fri", "cal-weekday-sat"}
	calendarDayIDs     = [7]string{"cal-day-sunday", "cal-day-monday", "cal-day-tuesday", "cal-day-wednesday", "cal-day-thursday", "cal-day-friday", "cal-day-saturday"}
	calendarMonthIDs   = [12]string{
		"cal-month-january", "cal-month-february", "cal-month-march", "cal-month-april", "cal-month-may", "cal-month-june",
		"cal-month-july", "cal-month-august", "cal-month-september", "cal-month-october", "cal-month-november", "cal-month-december",
	}
)

// calendarMonthLabel is format_month_label over cal-month-year.
func calendarMonthLabel(d time.Time) string {
	return i18n.T("cal-month-year", i18n.Str("month", i18n.T(calendarMonthIDs[d.Month()-1])), i18n.Str("year", strconv.Itoa(d.Year())))
}

// calendarIs12h is is_12h_format: the clock format uses %I or %p.
func calendarIs12h(format string) bool {
	return strings.Contains(format, "%I") || strings.Contains(format, "%p")
}

func strftimeOf(layout string, t time.Time) string {
	f, err := strftime.Compile(layout)
	if err != nil {
		return ""
	}
	return f.Format(t)
}

// calendarView is the calendar dropdown: the clock hero over the month
// grid, ticking every second while open.
type calendarView struct {
	ctx  ModuleContext
	font render.Font
	px   float64

	*widget.Box
	hours, minutes, seconds, ampm *widget.Label
	secSep                        *widget.Label
	dayName, dateRest             *widget.Label
	monthLabel                    *widget.Label
	grid                          *widget.Box

	month, today time.Time
	selected     *time.Time
	weekStart    time.Weekday

	once sync.Once
	stop chan struct{}
	now  func() time.Time
}

func calendarDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &calendarView{ctx: ctx, font: font, px: px, stop: make(chan struct{}), now: time.Now}
	v.Box = widget.NewBox(widget.Column, 12, 14)
	v.AddClass("dropdown", "calendar-dropdown")
	v.Append(dropdownHeader(font, px, "tb-calendar-time-symbolic", i18n.T("dropdown-calendar-title")), false)
	// DropdownContent: the content box the stylesheet's .dropdown-content
	// rules hang off (default ink, the section-label family).
	content := widget.NewBox(widget.Column, 12, 0)
	content.AddClass("dropdown-content")
	content.Append(v.hero(), false)
	content.Append(v.calendar(), false)
	v.Append(content, false)
	v.tick()
	v.follow()
	return v
}

// icon sizes a theme icon; its ink follows the cascade color, and the
// stylesheet's -gtk-icon-size overrides the constructor size.
func (v *calendarView) icon(name string, scale float64) *widget.Icon {
	return widget.NewThemeIcon(name, int(v.px*scale))
}

// label paints with the constructor ink unset (0): the cascade's
// color — a classed rule, or the content box's default — supplies it.
func (v *calendarView) label(text string, scale float64) *widget.Label {
	return widget.NewLabel(v.font, v.px*scale, text, 0)
}

// hero is the clock-hero: HH:MM[:SS][ AM] over "Weekday, Month D, YYYY".
func (v *calendarView) hero() widget.Widget {
	col := widget.NewBox(widget.Column, 4, 0)
	col.AddClass("clock-hero")
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("clock-time-row")
	big := 2.6
	timeLabel := func() *widget.Label {
		l := v.label("", big)
		l.AddClass("clock-time")
		return l
	}
	v.hours, v.minutes, v.seconds = timeLabel(), timeLabel(), timeLabel()
	sep := func() *widget.Label {
		l := v.label(":", big)
		l.AddClass("clock-time", "clock-separator")
		return l
	}
	v.secSep = sep()
	v.ampm = v.label("", 1.1)
	v.ampm.AddClass("clock-ampm")
	for _, w := range []widget.Widget{v.hours, sep(), v.minutes, v.secSep, v.seconds, v.ampm} {
		row.Append(w, false)
	}
	col.Append(centered(row), false)
	date := widget.NewBox(widget.Row, 0, 0)
	date.AddClass("clock-date")
	v.dayName = v.label("", 1)
	v.dayName.AddClass("clock-date-day")
	v.dateRest = v.label("", 1)
	date.Append(v.dayName, false)
	date.Append(v.dateRest, false)
	col.Append(centered(date), false)
	return col
}

// centered pads a row with expanding fillers on both sides.
func centered(w widget.Widget) widget.Widget {
	row := widget.NewBox(widget.Row, 0, 0)
	row.Append(widget.NewBox(widget.Row, 0, 0), true)
	row.Append(w, false)
	row.Append(widget.NewBox(widget.Row, 0, 0), true)
	return row
}

// calendar is the Calendar component: month label, today and month
// navigation, the weekday header, and the 42-day grid.
func (v *calendarView) calendar() widget.Widget {
	col := widget.NewBox(widget.Column, 6, 0)
	col.AddClass("cal-section")
	header := widget.NewBox(widget.Row, 4, 0)
	header.AddClass("cal-header")
	v.monthLabel = v.label("", 1)
	v.monthLabel.AddClass("cal-month")
	header.Append(v.monthLabel, true)
	nav := func(child widget.Widget, onClick func()) *widget.Button {
		b := widget.NewButton(child, 4, 6)
		b.AddClass("cal-nav-btn")
		// buttonBgActive stays: the stylesheet gives .cal-nav-btn a hover
		// but no :active, so the pressed shade keeps its programmatic
		// paint (pickc lets it win).
		b.BgPressed = v.ctx.Style.buttonBgActive
		b.OnClick = onClick
		return b
	}
	today := nav(v.label(i18n.T("cal-today"), 0.85), v.goToToday)
	today.AddClass("cal-today-btn")
	header.Append(today, false)
	header.Append(nav(v.icon("ld-chevron-left-symbolic", 1), func() { v.step(-1) }), false)
	header.Append(nav(v.icon("ld-chevron-right-symbolic", 1), func() { v.step(1) }), false)
	col.Append(header, false)
	v.grid = widget.NewBox(widget.Column, 2, 0)
	v.grid.AddClass("cal-grid")
	col.Append(v.grid, false)
	now := v.now()
	v.today = now
	v.month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	v.weekStart = calendarWeekStart(v.ctx.Config.Clock.CalendarWeekdayStart)
	v.rebuild()
	return col
}

// step moves the displayed month (PrevMonth/NextMonth).
func (v *calendarView) step(months int) {
	v.month = v.month.AddDate(0, months, 0)
	v.rebuild()
}

// goToToday is GoToToday: back to this month, the selection cleared.
func (v *calendarView) goToToday() {
	this := time.Date(v.today.Year(), v.today.Month(), 1, 0, 0, 0, 0, v.today.Location())
	if v.month.Equal(this) {
		return
	}
	v.month, v.selected = this, nil
	v.rebuild()
}

// click is DayClicked: today clears the selection, another day takes
// it.
func (v *calendarView) click(d time.Time) {
	if sameDay(d, v.today) {
		v.selected = nil
	} else {
		v.selected = &d
	}
	v.rebuild()
}

// rebuild is rebuild_grid: the weekday header and the day cells.
func (v *calendarView) rebuild() {
	v.monthLabel.SetText(calendarMonthLabel(v.month))
	v.grid.Clear()
	header := widget.NewBox(widget.Row, 2, 0)
	for col := range 7 {
		wd := time.Weekday((int(v.weekStart) + col) % 7)
		wdLabel := v.label(i18n.T(calendarWeekdayIDs[wd]), 0.8)
		wdLabel.AddClass("cal-weekday")
		setClass(wdLabel, "weekend", wd == time.Saturday || wd == time.Sunday)
		header.Append(centered(wdLabel), true)
	}
	v.grid.Append(header, false)
	cells := calendarGrid(v.month, v.today, v.selected, v.weekStart)
	for row := range 6 {
		week := widget.NewBox(widget.Row, 2, 0)
		for _, cell := range cells[row*7 : row*7+7] {
			week.Append(v.dayCell(cell), true)
		}
		v.grid.Append(week, false)
	}
}

// dayCell is one cal-day: the label fills its cell and the stylesheet
// paints it (today on the accent, the selection on bg-selected, other
// months faded, weekends tinted, hover on the overlay). The cell box
// only forwards the click — labels don't click, and a button here would
// take the hover the label's :hover rule needs.
func (v *calendarView) dayCell(c calendarCell) widget.Widget {
	number := v.label(strconv.Itoa(c.date.Day()), 0.9)
	number.AddClass("cal-day")
	number.SetAlignment(render.AlignCenter)
	setClass(number, "today", c.today)
	setClass(number, "selected", c.selected)
	setClass(number, "other", !c.currentMonth)
	setClass(number, "weekend", c.weekend)
	cell := widget.NewBox(widget.Row, 0, 0)
	cell.Append(number, true)
	if c.currentMonth {
		d := c.date
		cell.SetOnClickWithin(func() { v.click(d) })
	} else {
		cell.SetEnabled(false)
	}
	return cell
}

// tick is TimeTick: the hero's fields and, across midnight, today.
func (v *calendarView) tick() {
	now := v.now()
	clock := v.ctx.Config.Clock
	use12h := calendarIs12h(clock.Format)
	if use12h {
		v.hours.SetText(strftimeOf("%I", now))
	} else {
		v.hours.SetText(strftimeOf("%H", now))
	}
	v.minutes.SetText(strftimeOf("%M", now))
	v.seconds.SetText(strftimeOf("%S", now))
	v.ampm.SetText(strftimeOf("%p", now))
	v.seconds.SetVisible(clock.DropdownShowSeconds)
	v.secSep.SetVisible(clock.DropdownShowSeconds)
	v.ampm.SetVisible(use12h)
	v.dayName.SetText(i18n.T(calendarDayIDs[now.Weekday()]))
	v.dateRest.SetText(i18n.T("cal-clock-date-rest",
		i18n.Str("month", i18n.T(calendarMonthIDs[now.Month()-1])),
		i18n.Str("day", strconv.Itoa(now.Day())),
		i18n.Str("year", strconv.Itoa(now.Year()))))
	if !sameDay(now, v.today) {
		v.today = now
		v.rebuild()
	}
}

func (v *calendarView) follow() {
	if v.ctx.App == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-v.stop:
				return
			case <-ticker.C:
				v.ctx.Invoke(v.tick)
			}
		}
	}()
}

// dropdownClosed implements dropdownCloser.
func (v *calendarView) dropdownClosed() { v.once.Do(func() { close(v.stop) }) }
