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
	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass("dropdown-header")
	header.Append(v.icon("tb-calendar-time-symbolic", 1.2, ctx.Style.fg), false)
	header.Append(v.label(i18n.T("dropdown-calendar-title"), 1.1, ctx.Style.fg), true)
	v.Append(header, false)
	v.Append(v.hero(), false)
	v.Append(v.calendar(), false)
	v.tick()
	v.follow()
	return v
}

func (v *calendarView) tint(token config.CssToken) render.Color {
	return tokenColor(v.ctx.Style.palette, token)
}

func (v *calendarView) icon(name string, scale float64, color render.Color) *widget.Icon {
	icon := widget.NewThemeIcon(name, int(v.px*scale))
	icon.SetTint(color)
	return icon
}

func (v *calendarView) label(text string, scale float64, color render.Color) *widget.Label {
	return widget.NewLabel(v.font, v.px*scale, text, color)
}

// hero is the clock-hero: HH:MM[:SS][ AM] over "Weekday, Month D, YYYY".
func (v *calendarView) hero() widget.Widget {
	col := widget.NewBox(widget.Column, 4, 0)
	col.AddClass("clock-hero")
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("clock-time-row")
	big := 2.6
	v.hours, v.minutes, v.seconds = v.label("", big, v.ctx.Style.fg), v.label("", big, v.ctx.Style.fg), v.label("", big, v.ctx.Style.fg)
	sep := func() *widget.Label { return v.label(":", big, v.tint(config.TokenFgSubtle)) }
	v.secSep = sep()
	v.ampm = v.label("", 1.1, v.tint(config.TokenFgMuted))
	for _, w := range []widget.Widget{v.hours, sep(), v.minutes, v.secSep, v.seconds, v.ampm} {
		row.Append(w, false)
	}
	col.Append(centered(row), false)
	date := widget.NewBox(widget.Row, 0, 0)
	date.AddClass("clock-date")
	v.dayName = v.label("", 1, v.ctx.Style.fg)
	v.dateRest = v.label("", 1, v.tint(config.TokenFgMuted))
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
	v.monthLabel = v.label("", 1, v.ctx.Style.fg)
	header.Append(v.monthLabel, true)
	nav := func(child widget.Widget, onClick func()) *widget.Button {
		b := widget.NewButton(child, 4, 6)
		b.AddClass("cal-nav-btn")
		b.BgHover = v.ctx.Style.buttonBgHover
		b.BgPressed = v.ctx.Style.buttonBgActive
		b.OnClick = onClick
		return b
	}
	today := nav(v.label(i18n.T("cal-today"), 0.85, v.tint(config.TokenFgMuted)), v.goToToday)
	today.AddClass("cal-today-btn")
	header.Append(today, false)
	header.Append(nav(v.icon("ld-chevron-left-symbolic", 1, v.tint(config.TokenFgMuted)), func() { v.step(-1) }), false)
	header.Append(nav(v.icon("ld-chevron-right-symbolic", 1, v.tint(config.TokenFgMuted)), func() { v.step(1) }), false)
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
		color := v.tint(config.TokenFgSubtle)
		if wd == time.Saturday || wd == time.Sunday {
			color = v.tint(config.TokenAccent)
		}
		header.Append(centered(v.label(i18n.T(calendarWeekdayIDs[wd]), 0.8, color)), true)
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

// dayCell is one cal-day with the stylesheet's state colors: today on
// the accent, the selection on bg-selected, other months faded,
// weekends tinted.
func (v *calendarView) dayCell(c calendarCell) widget.Widget {
	fg := v.tint(config.TokenFgMuted)
	var bg render.Color
	switch {
	case c.today:
		fg, bg = v.tint(config.TokenFgOnAccent), v.tint(config.TokenAccent)
	case c.selected:
		fg, bg = v.tint(config.TokenAccent), v.tint(config.TokenBgSelected)
	case !c.currentMonth:
		fg = v.tint(config.TokenBgActive)
	case c.weekend:
		fg = mixColor(v.tint(config.TokenAccent), v.tint(config.TokenFgMuted))
	}
	b := widget.NewButton(centered(v.label(strconv.Itoa(c.date.Day()), 0.9, fg)), 4, 6)
	b.AddClass("cal-day")
	setClass(b, "today", c.today)
	setClass(b, "selected", c.selected)
	setClass(b, "other", !c.currentMonth)
	setClass(b, "weekend", c.weekend)
	b.BgExplicit = true
	b.Bg = bg
	if c.currentMonth {
		b.BgHover = v.tint(config.TokenBgOverlay)
		if c.today {
			b.BgHover = v.tint(config.TokenAccentHover)
		}
		d := c.date
		b.OnClick = func() { v.click(d) }
	} else {
		b.BgHover = bg
		b.SetEnabled(false)
	}
	return b
}

// mixColor is color-mix(in srgb, a 50%, b): the channel average.
func mixColor(a, b render.Color) render.Color {
	avg := func(x, y uint8) uint32 { return (uint32(x) + uint32(y) + 1) / 2 }
	return render.Color(avg(a.A(), b.A())<<24 | avg(a.R(), b.R())<<16 | avg(a.G(), b.G())<<8 | avg(a.B(), b.B()))
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
