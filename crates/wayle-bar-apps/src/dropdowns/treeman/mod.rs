mod actions;
mod factory;
mod messages;
mod watchers;

use gtk::{pango, prelude::*};
use relm4::{gtk, prelude::*};
use wayle_treeman::{Bucket, TreemanStatus};
use wayle_widgets::prelude::*;

pub use self::factory::Factory;
use self::{
    actions::Actions,
    messages::{TreemanDropdownCmd, TreemanDropdownInit},
};
use crate::{i18n::t, shell::bar::dropdowns::resolve_dimension};

const BASE_WIDTH: f32 = 400.0;
const BASE_HEIGHT: f32 = 460.0;

pub struct TreemanDropdown {
    scaled_width: i32,
    scaled_height: i32,
    /// Handle to the scrollable list box, rebuilt imperatively on each status
    /// change. Cloning a GTK widget yields another handle to the same object,
    /// so mutating this from `update_cmd` updates the shown widget.
    content: gtk::Box,
    /// Row action dispatcher (prepare/reset/teardown), captured into each
    /// rebuilt row's button callbacks.
    actions: Actions,
}

#[relm4::component(pub)]
impl Component for TreemanDropdown {
    type Init = TreemanDropdownInit;
    type Input = ();
    type Output = ();
    type CommandOutput = TreemanDropdownCmd;

    view! {
        #[root]
        gtk::Popover {
            set_css_classes: &["dropdown", "treeman-dropdown"],
            set_has_arrow: false,
            #[watch]
            set_width_request: model.scaled_width,
            #[watch]
            set_height_request: model.scaled_height,

            #[template]
            Dropdown {
                #[template]
                DropdownHeader {
                    #[template_child]
                    icon {
                        set_visible: true,
                        set_icon_name: Some("ld-layers-symbolic"),
                    },
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-treeman-title"),
                    },
                },

                #[template]
                DropdownContent {
                    set_vexpand: true,

                    gtk::ScrolledWindow {
                        set_hscrollbar_policy: gtk::PolicyType::Never,
                        set_vexpand: true,
                        set_propagate_natural_height: true,
                        add_css_class: "treeman-scroll",

                        #[wrap(Some)]
                        set_child = &gtk::Box {
                            set_orientation: gtk::Orientation::Vertical,

                            #[local_ref]
                            content -> gtk::Box {
                                set_orientation: gtk::Orientation::Vertical,
                                set_vexpand: true,
                                add_css_class: "treeman-list",
                            },
                        },
                    },
                },
            },
        }
    }

    fn init(
        init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let content = gtk::Box::builder()
            .orientation(gtk::Orientation::Vertical)
            .build();

        let actions = Actions::new(init.treeman.clone(), init.toast_bus.clone());
        render(&content, init.treeman.status.get().as_ref(), &actions);
        watchers::spawn(&sender, &init.treeman, &init.config);

        let scale = init.config.config().styling.scale.get().value();
        let model = Self {
            scaled_width: resolve_dimension(None, BASE_WIDTH, scale),
            scaled_height: resolve_dimension(None, BASE_HEIGHT, scale),
            content: content.clone(),
            actions,
        };

        let content = &content;
        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update_cmd(
        &mut self,
        msg: Self::CommandOutput,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            TreemanDropdownCmd::ScaleChanged(scale) => {
                self.scaled_width = resolve_dimension(None, BASE_WIDTH, scale);
                self.scaled_height = resolve_dimension(None, BASE_HEIGHT, scale);
            }
            TreemanDropdownCmd::StatusChanged(status) => {
                render(&self.content, status.as_ref(), &self.actions);
            }
        }
    }
}

/// Rebuilds the list from the current status. Read-only: clears every child and
/// repopulates, since worktree changes are infrequent and the list is small.
fn render(content: &gtk::Box, status: Option<&TreemanStatus>, actions: &Actions) {
    while let Some(child) = content.first_child() {
        content.remove(&child);
    }

    let Some(status) = status.filter(|s| !s.repos.is_empty()) else {
        content.append(&empty_state());
        return;
    };

    content.append(&summary(status));
    for repo in &status.repos {
        content.append(&repo_card(repo, actions));
    }
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

fn repo_card(repo: &wayle_treeman::TreemanRepo, actions: &Actions) -> gtk::Box {
    let card = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .build();
    card.set_css_classes(&["card", "treeman-repo"]);

    let header = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    header.add_css_class("treeman-repo-header");

    let name = gtk::Label::new(Some(&repo.repo));
    name.add_css_class("treeman-repo-name");
    name.set_xalign(0.0);
    name.set_hexpand(true);
    name.set_ellipsize(pango::EllipsizeMode::End);
    header.append(&name);

    let count = gtk::Label::new(Some(&repo.total.to_string()));
    count.add_css_class("badge");
    header.append(&count);

    card.append(&header);

    for wt in &repo.worktrees {
        card.append(&worktree_row(wt, actions));
    }

    card
}

fn worktree_row(wt: &wayle_treeman::TreemanWorktree, actions: &Actions) -> gtk::Box {
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
    line1.append(&branch);
    if wt.is_main {
        let badge = gtk::Label::new(Some(&t!("dropdown-treeman-main")));
        badge.set_css_classes(&["treeman-badge", "main"]);
        line1.append(&badge);
    }
    info.append(&line1);

    let meta = worktree_meta(wt);
    if !meta.is_empty() {
        let sub = gtk::Label::new(Some(&meta));
        sub.add_css_class("treeman-wt-meta");
        sub.set_xalign(0.0);
        sub.set_ellipsize(pango::EllipsizeMode::Middle);
        sub.set_selectable(true);
        sub.set_tooltip_text(Some(&wt.path));
        info.append(&sub);
    }
    row.append(&info);

    let state = gtk::Label::new(Some(&wt.state));
    state.set_css_classes(&["badge", dot_variant(bucket)]);
    state.set_valign(gtk::Align::Center);
    row.append(&state);

    if !wt.path.is_empty() {
        row.append(&actions.buttons(&wt.path));
    }

    row
}

/// The muted second line of a worktree row: `slug · path`, gracefully dropping
/// either half when absent.
fn worktree_meta(wt: &wayle_treeman::TreemanWorktree) -> String {
    match (wt.slug.is_empty(), wt.path.is_empty()) {
        (false, false) => format!("{} · {}", wt.slug, wt.path),
        (false, true) => wt.slug.clone(),
        (true, false) => wt.path.clone(),
        (true, true) => String::new(),
    }
}

/// Localized bucket name for the summary chips.
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
