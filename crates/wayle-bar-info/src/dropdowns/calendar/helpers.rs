use chrono::{DateTime, Datelike, Local, Weekday};

use crate::i18n::t;

pub fn is_12h_format(format_str: &str) -> bool {
    format_str.contains("%I") || format_str.contains("%p")
}

pub fn hours_text(now: &DateTime<Local>, use_12h: bool) -> String {
    if use_12h {
        now.format("%I").to_string()
    } else {
        now.format("%H").to_string()
    }
}

pub fn minutes_text(now: &DateTime<Local>) -> String {
    now.format("%M").to_string()
}

pub fn seconds_text(now: &DateTime<Local>) -> String {
    now.format("%S").to_string()
}

pub fn ampm_text(now: &DateTime<Local>) -> String {
    now.format("%p").to_string()
}

pub fn format_date_rest(months: &[String; 12], now: &DateTime<Local>) -> String {
    let month_idx = usize::try_from(now.month0()).unwrap_or_default();
    t!(
        "cal-clock-date-rest",
        month = months.get(month_idx).cloned().unwrap_or_default(),
        day = now.day().to_string(),
        year = now.year().to_string()
    )
}

pub fn day_names_array() -> [String; 7] {
    [
        t!("cal-day-sunday"),
        t!("cal-day-monday"),
        t!("cal-day-tuesday"),
        t!("cal-day-wednesday"),
        t!("cal-day-thursday"),
        t!("cal-day-friday"),
        t!("cal-day-saturday"),
    ]
}

pub fn weekdays_array(week_start: Weekday) -> [String; 7] {
    // Base order is Sunday-first (matches num_days_from_sunday indexing).
    let base = [
        t!("cal-weekday-sun"),
        t!("cal-weekday-mon"),
        t!("cal-weekday-tue"),
        t!("cal-weekday-wed"),
        t!("cal-weekday-thu"),
        t!("cal-weekday-fri"),
        t!("cal-weekday-sat"),
    ];
    // Rotate left by the start day's Sunday-offset so week_start lands at col 0.
    let rot = usize::try_from(week_start.num_days_from_sunday()).unwrap_or_default();
    std::array::from_fn(|i| {
        base.get(rot.saturating_add(i) % 7)
            .cloned()
            .unwrap_or_default()
    })
}

pub fn months_array() -> [String; 12] {
    [
        t!("cal-month-january"),
        t!("cal-month-february"),
        t!("cal-month-march"),
        t!("cal-month-april"),
        t!("cal-month-may"),
        t!("cal-month-june"),
        t!("cal-month-july"),
        t!("cal-month-august"),
        t!("cal-month-september"),
        t!("cal-month-october"),
        t!("cal-month-november"),
        t!("cal-month-december"),
    ]
}
