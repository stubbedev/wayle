//! Per-worktree action buttons: prepare / reset / teardown.
//!
//! Each button shells out through [`TreemanService::run_action`], which queues
//! the work with the treeman daemon and returns; the status subscription then
//! refreshes the popover as lifecycle events arrive. Destructive actions gate
//! behind a native confirm dialog; failures surface as a toast.

use std::sync::Arc;

use gtk::prelude::*;
use relm4::gtk;
use wayle_treeman::{Action, TreemanService};

use crate::{
    i18n::t,
    services::{ToastBus, ToastRequest},
};

/// Confirmation copy for a destructive action.
struct Confirm {
    title: String,
    accept: String,
}

/// Handles that a worktree row's buttons need to trigger mutations and report
/// failures. Cheap to clone (an `Arc` and a broadcast sender).
#[derive(Clone)]
pub struct Actions {
    treeman: Arc<TreemanService>,
    toast: ToastBus,
}

impl Actions {
    pub fn new(treeman: Arc<TreemanService>, toast: ToastBus) -> Self {
        Self { treeman, toast }
    }

    /// The trailing action-button cluster for one worktree row.
    pub fn buttons(&self, worktree_path: &str) -> gtk::Box {
        let row = gtk::Box::new(gtk::Orientation::Horizontal, 0);
        row.add_css_class("treeman-actions");

        row.append(&self.button(
            "tb-refresh-symbolic",
            &t!("dropdown-treeman-action-prepare"),
            Action::Prepare,
            worktree_path,
            None,
        ));
        row.append(&self.button(
            "ld-rotate-ccw-symbolic",
            &t!("dropdown-treeman-action-reset"),
            Action::Reset,
            worktree_path,
            Some(Confirm {
                title: t!("dropdown-treeman-confirm-reset-title"),
                accept: t!("dropdown-treeman-confirm-reset-accept"),
            }),
        ));
        row.append(&self.button(
            "ld-trash-2-symbolic",
            &t!("dropdown-treeman-action-teardown"),
            Action::Teardown,
            worktree_path,
            Some(Confirm {
                title: t!("dropdown-treeman-confirm-teardown-title"),
                accept: t!("dropdown-treeman-confirm-teardown-accept"),
            }),
        ));

        row
    }

    fn button(
        &self,
        icon: &str,
        tooltip: &str,
        action: Action,
        worktree_path: &str,
        confirm: Option<Confirm>,
    ) -> gtk::Button {
        let button = gtk::Button::new();
        button.set_css_classes(&["ghost-icon"]);
        button.set_icon_name(icon);
        button.set_cursor_from_name(Some("pointer"));
        button.set_valign(gtk::Align::Center);
        button.set_tooltip_text(Some(tooltip));

        let this = self.clone();
        let path = worktree_path.to_owned();
        button.connect_clicked(move |anchor| match &confirm {
            Some(confirm) => this.confirm_then_run(anchor, confirm, action, path.clone()),
            None => this.run(action, path.clone()),
        });

        button
    }

    /// Shows a modal confirm anchored to the row; runs the action only when the
    /// accept button (index 1) is chosen.
    fn confirm_then_run(
        &self,
        anchor: &gtk::Button,
        confirm: &Confirm,
        action: Action,
        path: String,
    ) {
        let cancel = t!("dropdown-treeman-confirm-cancel");
        let dialog = gtk::AlertDialog::builder()
            .modal(true)
            .message(confirm.title.as_str())
            .detail(path.as_str())
            .build();
        dialog.set_buttons(&[cancel.as_str(), confirm.accept.as_str()]);
        dialog.set_cancel_button(0);
        dialog.set_default_button(0);

        let window = anchor.root().and_downcast::<gtk::Window>();
        let this = self.clone();
        dialog.choose(
            window.as_ref(),
            gtk::gio::Cancellable::NONE,
            move |result| {
                if matches!(result, Ok(1)) {
                    this.run(action, path);
                }
            },
        );
    }

    fn run(&self, action: Action, path: String) {
        let treeman = self.treeman.clone();
        let toast = self.toast.clone();
        relm4::spawn(async move {
            if let Err(err) = treeman.run_action(action, &path).await {
                tracing::warn!(%err, ?action, "treeman action failed");
                toast.publish(ToastRequest {
                    label: Some(format!("{}: {err}", t!("dropdown-treeman-action-failed"))),
                    icon: Some(String::from("tb-alert-triangle-symbolic")),
                    percentage: None,
                    duration_ms: None,
                    preset: None,
                    class: None,
                });
            }
        });
    }
}
