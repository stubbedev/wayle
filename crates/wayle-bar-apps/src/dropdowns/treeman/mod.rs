mod actions;
mod factory;
mod messages;
mod views;
mod watchers;

use std::{cell::RefCell, collections::HashSet, rc::Rc};

use gtk::prelude::*;
use relm4::{gtk, prelude::*};
use wayle_config::schemas::styling::Size;
use wayle_treeman::TreemanStatus;
use wayle_widgets::prelude::*;

pub use self::factory::Factory;
use self::{
    actions::Actions,
    messages::{TreemanDropdownCmd, TreemanDropdownInit, TreemanDropdownMsg},
    views::Ui,
};
use crate::{i18n::t, shell::bar::dropdowns::resolve_dimension};

const BASE_WIDTH: f32 = 440.0;
const BASE_HEIGHT: f32 = 460.0;

/// Stack page names.
const PAGE_LIST: &str = "list";
const PAGE_DETAIL: &str = "detail";

pub struct TreemanDropdown {
    scaled_width: i32,
    scaled_height: i32,
    /// User size overrides from `dropdowns.treeman`, reapplied when the global
    /// scale changes.
    width_override: Option<Size>,
    height_override: Option<Size>,
    /// Pages the popover switches between. Both stay alive across navigation,
    /// so a click never destroys the widget that is still dispatching it.
    stack: gtk::Stack,
    /// Repo list page, rebuilt imperatively on each status change. Cloning a
    /// GTK widget yields another handle to the same object, so mutating this
    /// from `update_cmd` updates the shown widget.
    list: gtk::Box,
    /// Single-worktree page, rebuilt when navigating into it.
    detail_page: gtk::Box,
    /// Latest status, kept so navigation can re-render without waiting for the
    /// next push from the service.
    status: Option<TreemanStatus>,
    /// Absolute path of the worktree whose detail page is showing, if any.
    detail: Option<String>,
    /// In-dropdown transition duration from the animation config, shared by the
    /// stack slide and the popover height tween. `0` when animations are off.
    transition_ms: u32,
    /// Title shown in the header: the branch on the detail page, otherwise the
    /// dropdown's own name.
    title: String,
    /// Render-tree dependencies (actions, accordion state, navigation sender).
    ui: Ui,
}

#[relm4::component(pub)]
impl Component for TreemanDropdown {
    type Init = TreemanDropdownInit;
    type Input = TreemanDropdownMsg;
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
                        #[watch]
                        set_label: &model.title,
                    },
                    #[template_child]
                    actions {
                        #[template]
                        GhostIconButton {
                            set_icon_name: "ld-arrow-left-symbolic",
                            set_tooltip_text: Some(&t!("dropdown-treeman-back")),
                            // This button hides itself on click. A focused
                            // widget going invisible moves focus out of the
                            // popover, and an autohide popover closes when it
                            // loses focus — so never take focus on click.
                            set_focus_on_click: false,
                            #[watch]
                            set_visible: model.detail.is_some(),
                            connect_clicked[sender] => move |_| {
                                sender.input(TreemanDropdownMsg::Back);
                            },
                        },
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

                            // Both axes homogeneous, and no `interpolate_size`:
                            // the stack must measure identically on every page.
                            // A popover sizes to its child's natural height —
                            // `set_height_request` is only a floor — so a stack
                            // that measured the visible child would resize the
                            // popover surface on every page switch. Resizing a
                            // mapped popover drops its Wayland popup grab and
                            // closes it mid-click.
                            #[local_ref]
                            stack -> gtk::Stack {
                                set_transition_type: gtk::StackTransitionType::SlideLeftRight,
                                set_transition_duration: model.transition_ms,
                                set_hhomogeneous: true,
                                set_vhomogeneous: true,
                                set_vexpand: true,
                            },
                        },
                    },
                },
            },
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let list = page_box("treeman-list");
        // The stack is vertically homogeneous, so the short detail page would
        // otherwise be stretched down the full height of the repo list.
        let detail_page = page_box("treeman-list");
        detail_page.set_valign(gtk::Align::Start);

        let stack = gtk::Stack::new();
        stack.add_named(&list, Some(PAGE_LIST));
        stack.add_named(&detail_page, Some(PAGE_DETAIL));

        let ui = Ui {
            actions: Actions::new(init.treeman.clone(), init.toast_bus.clone()),
            collapsed: Rc::new(RefCell::new(HashSet::new())),
            sender: sender.clone(),
        };
        let status = init.treeman.status.get();
        views::render_list(&list, status.as_ref(), &ui);
        watchers::spawn(&sender, &init.treeman, &init.config);

        // Reopening a dropdown should land on its root page. It also keeps the
        // size honest: the registry restores a popover's first-seen height
        // request on every popup, which would otherwise fight a detail page
        // that had shrunk the popover before it was closed.
        let closed = sender.clone();
        root.connect_closed(move |_| closed.input(TreemanDropdownMsg::Back));

        let config = init.config.config();
        let scale = config.styling.scale.get().value();
        let transition_ms = config.animations.interaction_duration_ms();
        let size = config.dropdowns.treeman.get();
        let model = Self {
            scaled_width: resolve_dimension(size.width, BASE_WIDTH, scale),
            scaled_height: resolve_dimension(size.height, BASE_HEIGHT, scale),
            width_override: size.width,
            height_override: size.height,
            transition_ms,
            stack: stack.clone(),
            list,
            detail_page,
            status,
            detail: None,
            title: t!("dropdown-treeman-title"),
            ui,
        };

        let stack = &stack;
        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>, root: &Self::Root) {
        self.detail = match msg {
            TreemanDropdownMsg::OpenDetail(path) => Some(path),
            TreemanDropdownMsg::Back => None,
        };
        self.sync_page(root);
    }

    fn update_cmd(
        &mut self,
        msg: Self::CommandOutput,
        _sender: ComponentSender<Self>,
        root: &Self::Root,
    ) {
        match msg {
            TreemanDropdownCmd::ScaleChanged(scale) => {
                self.scaled_width = resolve_dimension(self.width_override, BASE_WIDTH, scale);
                self.scaled_height = resolve_dimension(self.height_override, BASE_HEIGHT, scale);
            }
            TreemanDropdownCmd::StatusChanged(status) => {
                self.status = status;
                views::render_list(&self.list, self.status.as_ref(), &self.ui);
                self.sync_page(root);
            }
        }
    }
}

fn page_box(css_class: &str) -> gtk::Box {
    let page = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .build();
    page.add_css_class(css_class);
    page
}

impl TreemanDropdown {
    /// Points the stack at the right page and resyncs the header title. Drops
    /// back to the list when the detail page's worktree is gone — a teardown
    /// finishing while its own page is open would otherwise strand the user.
    fn sync_page(&mut self, popover: &gtk::Popover) {
        let found = self
            .detail
            .as_deref()
            .zip(self.status.as_ref())
            .and_then(|(path, status)| views::find_worktree(status, path));

        let page = if let Some((repo, wt)) = found {
            self.title = wt.branch.clone();
            views::render_detail(&self.detail_page, repo, wt, &self.ui);
            PAGE_DETAIL
        } else {
            self.detail = None;
            self.title = t!("dropdown-treeman-title");
            PAGE_LIST
        };

        if self.stack.visible_child_name().as_deref() == Some(page) {
            return;
        }
        self.stack.set_visible_child_name(page);

        // Geometry is logged because a popover that resizes while mapped loses
        // its popup grab: if this ever shows the popover height changing across
        // a page switch, that is the bug returning.
        // Popover geometry across a page switch. It must not change: a mapped
        // Wayland popup cannot be resized in place, so GTK would destroy and
        // recreate the surface and the popover would close mid-interaction.
        tracing::debug!(
            page,
            popover = ?(popover.width(), popover.height()),
            natural = ?popover.preferred_size().1.width(),
            visible = popover.is_visible(),
            "treeman page switch"
        );

        // The detail page is short, so let the popover shrink to it; the repo
        // list keeps the base height and scrolls, as every other dropdown does.
        // Never grow past the base height — a long list must not stretch the
        // popover down the screen.
    }
}
