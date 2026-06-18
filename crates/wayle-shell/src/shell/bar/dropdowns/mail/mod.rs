mod factory;
mod messages;
mod watchers;

use gtk::prelude::*;
use relm4::{gtk, prelude::*};
use wayle_config::schemas::styling::Size;
use wayle_widgets::prelude::*;

pub(super) use self::factory::Factory;
use self::messages::{MailDropdownCmd, MailDropdownInit};
use crate::{
    i18n::t,
    services::mail::AccountUnread,
    shell::bar::dropdowns::resolve_dimension,
};

const BASE_WIDTH: f32 = 300.0;
const BASE_HEIGHT: f32 = 360.0;
const ROW_ICON_SIZE: i32 = 20;

pub(crate) struct MailDropdown {
    scaled_width: i32,
    scaled_height: i32,
    width_override: Option<Size>,
    height_override: Option<Size>,
    list: gtk::Box,
}

#[relm4::component(pub(crate))]
impl Component for MailDropdown {
    type Init = MailDropdownInit;
    type Input = ();
    type Output = ();
    type CommandOutput = MailDropdownCmd;

    view! {
        #[root]
        gtk::Popover {
            set_css_classes: &["dropdown", "mail-dropdown"],
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
                        set_icon_name: Some("ld-mail-symbolic"),
                    },
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-mail-title"),
                    },
                    #[template_child]
                    actions {
                        set_visible: false,
                    },
                },

                #[template]
                DropdownContent {
                    set_vexpand: true,

                    gtk::ScrolledWindow {
                        set_hscrollbar_policy: gtk::PolicyType::Never,
                        set_vexpand: true,

                        #[local_ref]
                        list_widget -> gtk::Box {},
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
        let list = gtk::Box::builder()
            .orientation(gtk::Orientation::Vertical)
            .spacing(4)
            .css_classes(["mail-dropdown-list"])
            .build();
        rebuild_list(&list, &init.mail.accounts.get());

        let scale = init.config.config().styling.scale.get().value();
        let size = init.config.config().dropdowns.mail.get();
        watchers::spawn(&sender, &init.config, &init.mail);

        let model = Self {
            scaled_width: resolve_dimension(size.width, BASE_WIDTH, scale),
            scaled_height: resolve_dimension(size.height, BASE_HEIGHT, scale),
            width_override: size.width,
            height_override: size.height,
            list,
        };

        let list_widget = &model.list;
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update_cmd(
        &mut self,
        msg: MailDropdownCmd,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            MailDropdownCmd::ScaleChanged(scale) => {
                self.scaled_width = resolve_dimension(self.width_override, BASE_WIDTH, scale);
                self.scaled_height = resolve_dimension(self.height_override, BASE_HEIGHT, scale);
            }
            MailDropdownCmd::AccountsChanged(accounts) => {
                rebuild_list(&self.list, &accounts);
            }
        }
    }
}

/// Repopulates the account list, or shows an empty-state hint when no accounts
/// are configured.
fn rebuild_list(list: &gtk::Box, accounts: &[AccountUnread]) {
    while let Some(child) = list.first_child() {
        list.remove(&child);
    }

    if accounts.is_empty() {
        let empty = gtk::Label::builder()
            .label(t!("dropdown-mail-empty"))
            .wrap(true)
            .css_classes(["mail-dropdown-empty"])
            .build();
        list.append(&empty);
        return;
    }

    for account in accounts {
        list.append(&account_row(account));
    }
}

fn account_row(account: &AccountUnread) -> gtk::Box {
    let row = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .css_classes(["mail-dropdown-row"])
        .build();

    let icon = gtk::Image::from_icon_name(&account.icon);
    icon.set_pixel_size(ROW_ICON_SIZE);

    let name = gtk::Label::builder()
        .label(&account.name)
        .hexpand(true)
        .xalign(0.0)
        .ellipsize(gtk::pango::EllipsizeMode::End)
        .css_classes(["mail-account-name"])
        .build();

    let count = gtk::Label::builder()
        .label(account.count.to_string())
        .css_classes(["mail-account-count"])
        .build();
    if account.count == 0 {
        count.add_css_class("dim");
    }

    row.append(&icon);
    row.append(&name);
    row.append(&count);
    row
}
