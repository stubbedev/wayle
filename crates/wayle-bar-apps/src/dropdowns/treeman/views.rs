//! Imperative render tree for the treeman dropdown.
//!
//! Renders the two pages the popover's stack switches between: the repo list (an
//! accordion of repo cards over worktree rows) and a single worktree's detail
//! page, opened by a row's info button and left by the header's back button.

use std::{cell::RefCell, collections::HashSet, rc::Rc};

use gtk::{pango, prelude::*};
use relm4::{ComponentSender, gtk};
use wayle_treeman::{Bucket, TreemanRepo, TreemanStatus, TreemanWorktree};

use super::{TreemanDropdown, actions::Actions, messages::TreemanDropdownMsg};
use crate::i18n::t;

/// Natural-width cap for any label that can hold an arbitrarily long branch,
/// slug, or path. Purely a measurement bound — see [`cap_natural_width`].
const MAX_LABEL_CHARS: i32 = 20;

/// Everything the render tree needs beyond the status snapshot.
pub struct Ui {
    /// Row action dispatcher (prepare/reset/teardown).
    pub actions: Actions,
    /// Repos the user folded shut, by repo name. Behind an `Rc<RefCell<_>>`
    /// because the toggle happens inside a GTK callback and must survive the
    /// imperative rebuild that every status refresh triggers.
    pub collapsed: Rc<RefCell<HashSet<String>>>,
    /// For navigating into a worktree's detail page.
    pub sender: ComponentSender<TreemanDropdown>,
}

/// Rebuilds the list page from the current status. Read-only: clears every
/// child and repopulates, since worktree changes are infrequent and the list is
/// small.
pub fn render_list(content: &gtk::Box, status: Option<&TreemanStatus>, ui: &Ui) {
    clear(content);

    let Some(status) = status.filter(|s| !s.repos.is_empty()) else {
        content.append(&empty_state());
        return;
    };

    content.append(&summary(status));
    for repo in &status.repos {
        content.append(&repo_card(repo, ui));
    }
}

/// Rebuilds the detail page for one worktree.
pub fn render_detail(content: &gtk::Box, repo: &TreemanRepo, wt: &TreemanWorktree, ui: &Ui) {
    clear(content);
    content.append(&detail_page(repo, wt, ui));
}

fn clear(content: &gtk::Box) {
    while let Some(child) = content.first_child() {
        content.remove(&child);
    }
}

/// Locates a worktree by absolute path, returning it with its owning repo.
pub fn find_worktree<'s>(
    status: &'s TreemanStatus,
    path: &str,
) -> Option<(&'s TreemanRepo, &'s TreemanWorktree)> {
    status.repos.iter().find_map(|repo| {
        repo.worktrees
            .iter()
            .find(|wt| wt.path == path)
            .map(|wt| (repo, wt))
    })
}

fn empty_state() -> gtk::Box {
    let root = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .valign(gtk::Align::Center)
        .vexpand(true)
        .build();
    root.add_css_class("empty-state");

    let icon = gtk::Image::from_icon_name("ld-layers-symbolic");
    icon.add_css_class("icon");
    root.append(&icon);

    let title = gtk::Label::new(Some(&t!("dropdown-treeman-empty-title")));
    title.add_css_class("title");
    root.append(&title);

    let desc = gtk::Label::new(Some(&t!("dropdown-treeman-empty-desc")));
    desc.add_css_class("description");
    desc.set_wrap(true);
    desc.set_justify(gtk::Justification::Center);
    root.append(&desc);

    root
}

/// A row of per-bucket count chips across the top, so overall health reads at a
/// glance before scanning individual repos. Only non-empty buckets show.
fn summary(status: &TreemanStatus) -> gtk::Box {
    let row = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    row.add_css_class("treeman-summary");

    for (count, bucket) in [
        (status.stable, Bucket::Stable),
        (status.up, Bucket::Up),
        (status.down, Bucket::Down),
        (status.failed, Bucket::Failed),
    ] {
        if count > 0 {
            row.append(&stat_chip(count, bucket));
        }
    }

    row
}

fn stat_chip(count: u32, bucket: Bucket) -> gtk::Box {
    let chip = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    chip.add_css_class("treeman-stat");

    let dot = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    dot.set_css_classes(&["status-dot", dot_variant(bucket)]);
    dot.set_valign(gtk::Align::Center);
    chip.append(&dot);

    let label = gtk::Label::new(Some(&format!("{count} {}", bucket_label(bucket))));
    label.add_css_class("treeman-stat-label");
    chip.append(&label);

    chip
}

/// One repo as an accordion: a clickable header that folds its worktree list
/// into a revealer. Collapsed state lives in [`Ui::collapsed`] so it survives
/// the rebuild a status refresh causes.
fn repo_card(repo: &TreemanRepo, ui: &Ui) -> gtk::Box {
    let card = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .build();
    card.set_css_classes(&["card", "treeman-repo"]);

    let expanded = !ui.collapsed.borrow().contains(&repo.repo);

    let list = gtk::Box::new(gtk::Orientation::Vertical, 0);
    for wt in &repo.worktrees {
        list.append(&worktree_row(wt, ui));
    }

    let revealer = gtk::Revealer::new();
    revealer.set_transition_type(gtk::RevealerTransitionType::SlideDown);
    revealer.set_child(Some(&list));
    revealer.set_reveal_child(expanded);

    card.append(&repo_header(repo, expanded, &revealer, ui));
    card.append(&revealer);

    card
}

fn repo_header(
    repo: &TreemanRepo,
    expanded: bool,
    revealer: &gtk::Revealer,
    ui: &Ui,
) -> gtk::Button {
    let row = gtk::Box::new(gtk::Orientation::Horizontal, 0);

    let chevron = gtk::Image::from_icon_name(chevron_icon(expanded));
    chevron.add_css_class("treeman-repo-chevron");
    row.append(&chevron);

    let name = gtk::Label::new(Some(&repo.repo));
    name.add_css_class("treeman-repo-name");
    name.set_xalign(0.0);
    name.set_hexpand(true);
    name.set_ellipsize(pango::EllipsizeMode::End);
    cap_natural_width(&name);
    row.append(&name);

    let count = gtk::Label::new(Some(&repo.total.to_string()));
    count.add_css_class("badge");
    row.append(&count);

    let button = gtk::Button::new();
    button.set_css_classes(&["treeman-repo-header"]);
    button.set_cursor_from_name(Some("pointer"));
    button.set_child(Some(&row));

    let collapsed = ui.collapsed.clone();
    let name = repo.repo.clone();
    let revealer = revealer.clone();
    button.connect_clicked(move |_| {
        let expanded = !revealer.reveals_child();
        revealer.set_reveal_child(expanded);
        chevron.set_icon_name(Some(chevron_icon(expanded)));
        if expanded {
            collapsed.borrow_mut().remove(&name);
        } else {
            collapsed.borrow_mut().insert(name.clone());
        }
    });

    button
}

fn chevron_icon(expanded: bool) -> &'static str {
    if expanded {
        "ld-chevron-down-symbolic"
    } else {
        "ld-chevron-right-symbolic"
    }
}

fn worktree_row(wt: &TreemanWorktree, ui: &Ui) -> gtk::Overlay {
    let bucket = Bucket::parse(&wt.bucket);

    let row = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    row.add_css_class("treeman-wt");

    let dot = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    dot.set_css_classes(&["status-dot", dot_variant(bucket)]);
    dot.set_valign(gtk::Align::Center);
    row.append(&dot);

    let info = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .hexpand(true)
        .build();
    info.add_css_class("treeman-wt-info");

    let line1 = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    let branch = gtk::Label::new(Some(&wt.branch));
    branch.add_css_class("treeman-branch");
    branch.set_xalign(0.0);
    branch.set_ellipsize(pango::EllipsizeMode::End);
    cap_natural_width(&branch);
    line1.append(&branch);
    if wt.is_main {
        line1.append(&main_badge());
    }
    info.append(&line1);

    row.append(&info);
    row.set_tooltip_text(Some(&wt.path));

    let state = gtk::Label::new(Some(&wt.state));
    state.set_css_classes(&["badge", dot_variant(bucket)]);
    state.set_valign(gtk::Align::Center);
    row.append(&state);

    // Actions float over the trailing edge so, unlike an in-flow cluster hidden
    // by opacity, they reserve no width at rest — the branch column keeps the
    // full row. Hover slides them in over the state badge with no reflow.
    let overlay = gtk::Overlay::new();
    overlay.add_css_class("treeman-wt-overlay");
    overlay.set_child(Some(&row));
    if !wt.path.is_empty() {
        let buttons = ui.actions.buttons(&wt.path);
        buttons.prepend(&info_button(&wt.path, ui));
        buttons.set_halign(gtk::Align::End);
        buttons.set_valign(gtk::Align::Center);
        overlay.add_overlay(&buttons);
    }

    overlay
}

/// The leading button of a row's hover cluster: opens the worktree's detail
/// page. Lives here rather than in [`Actions`] because it navigates instead of
/// dispatching a treeman command.
fn info_button(path: &str, ui: &Ui) -> gtk::Button {
    let button = ghost_icon("ld-info-symbolic", &t!("dropdown-treeman-action-info"));

    let sender = ui.sender.clone();
    let path = path.to_owned();
    button.connect_clicked(move |_| {
        sender.input(TreemanDropdownMsg::OpenDetail(path.clone()));
    });

    button
}

/// Caps a label's *natural* width so long text cannot widen its container.
///
/// An ellipsizing label reports the full untruncated string as its natural
/// width, and a wrapping one reports the unwrapped string. Inside a popover
/// that is fatal: the natural width propagates up through the scrolled window
/// (`hscrollbar_policy: Never`) to the popover, and a popover that resizes while
/// mapped loses its Wayland popup grab and closes. `hexpand` keeps the label
/// filling the space it is actually given, so appearance is unchanged.
fn cap_natural_width(label: &gtk::Label) {
    label.set_max_width_chars(MAX_LABEL_CHARS);
    label.set_hexpand(true);
}

/// A flat icon-only button sized for a row's hover cluster.
pub fn ghost_icon(icon: &str, tooltip: &str) -> gtk::Button {
    let button = gtk::Button::new();
    button.set_css_classes(&["ghost-icon"]);
    button.set_icon_name(icon);
    button.set_cursor_from_name(Some("pointer"));
    button.set_valign(gtk::Align::Center);
    button.set_tooltip_text(Some(tooltip));
    // Don't park focus inside a page that a click is about to hide (info
    // switches the stack; a teardown drops the detail page). Tab focus still
    // works.
    button.set_focus_on_click(false);
    button
}

/// The full record for one worktree, reached by clicking its row. Every field
/// treeman reports gets its own labelled line, with the path free to wrap
/// instead of being ellipsized into a tooltip.
fn detail_page(repo: &TreemanRepo, wt: &TreemanWorktree, ui: &Ui) -> gtk::Box {
    let bucket = Bucket::parse(&wt.bucket);

    let page = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .build();
    page.add_css_class("treeman-detail");

    let head = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    head.add_css_class("treeman-detail-head");

    let dot = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    dot.set_css_classes(&["status-dot", dot_variant(bucket)]);
    dot.set_valign(gtk::Align::Center);
    head.append(&dot);

    let branch = gtk::Label::new(Some(&wt.branch));
    branch.add_css_class("treeman-detail-branch");
    branch.set_xalign(0.0);
    branch.set_ellipsize(pango::EllipsizeMode::End);
    cap_natural_width(&branch);
    head.append(&branch);

    if wt.is_main {
        head.append(&main_badge());
    }

    let state = gtk::Label::new(Some(&wt.state));
    state.set_css_classes(&["badge", dot_variant(bucket)]);
    state.set_valign(gtk::Align::Center);
    head.append(&state);

    page.append(&head);

    let fields = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .build();
    fields.set_css_classes(&["card", "treeman-detail-fields"]);
    fields.append(&field(
        &t!("dropdown-treeman-detail-repo"),
        &repo.repo,
        false,
    ));
    fields.append(&field(
        &t!("dropdown-treeman-detail-bucket"),
        &bucket_label(bucket),
        false,
    ));
    if !wt.slug.is_empty() {
        fields.append(&field(&t!("dropdown-treeman-detail-slug"), &wt.slug, false));
    }
    if !wt.path.is_empty() {
        fields.append(&field(&t!("dropdown-treeman-detail-path"), &wt.path, true));
    }
    page.append(&fields);

    if !wt.path.is_empty() {
        let row = ui.actions.buttons(&wt.path);
        row.set_halign(gtk::Align::End);
        row.add_css_class("treeman-detail-actions");
        page.append(&row);
    }

    page
}

/// One `key: value` line in the detail page. `wrap` lets long values (the path)
/// break across lines rather than truncate.
fn field(key: &str, value: &str, wrap: bool) -> gtk::Box {
    let row = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    row.add_css_class("treeman-detail-field");

    let key_label = gtk::Label::new(Some(key));
    key_label.add_css_class("treeman-detail-key");
    key_label.set_xalign(0.0);
    key_label.set_valign(gtk::Align::Start);
    row.append(&key_label);

    let value_label = gtk::Label::new(Some(value));
    value_label.add_css_class("treeman-detail-value");
    value_label.set_xalign(0.0);
    cap_natural_width(&value_label);
    if wrap {
        value_label.set_wrap(true);
        value_label.set_wrap_mode(pango::WrapMode::WordChar);
        // A wrapping label still reports the *unwrapped* string as its natural
        // width by default. The scrolled window above has `hscrollbar_policy:
        // Never`, so that natural width propagates to the popover and makes it
        // wider than its size request — and resizing a mapped popover drops its
        // Wayland popup grab and closes it. `NaturalWrapMode::None` pins the
        // natural width to the minimum, so a long path wraps instead of pushing.
        value_label.set_natural_wrap_mode(gtk::NaturalWrapMode::None);
    } else {
        value_label.set_ellipsize(pango::EllipsizeMode::End);
    }
    row.append(&value_label);

    row
}

fn main_badge() -> gtk::Label {
    let badge = gtk::Label::new(Some(&t!("dropdown-treeman-main")));
    badge.set_css_classes(&["treeman-badge", "main"]);
    badge
}

/// Localized bucket name for the summary chips and the detail page.
fn bucket_label(bucket: Bucket) -> String {
    match bucket {
        Bucket::Stable => t!("dropdown-treeman-bucket-stable"),
        Bucket::Up => t!("dropdown-treeman-bucket-up"),
        Bucket::Down => t!("dropdown-treeman-bucket-down"),
        Bucket::Failed => t!("dropdown-treeman-bucket-failed"),
    }
}

/// Maps a bucket to the shared `status-dot` / `badge` colour variant.
fn dot_variant(bucket: Bucket) -> &'static str {
    match bucket {
        Bucket::Stable => "success",
        Bucket::Up => "info",
        Bucket::Down => "warning",
        Bucket::Failed => "error",
    }
}
