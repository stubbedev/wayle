use gtk::prelude::*;
use relm4::gtk;

use super::PasswordForm;
use crate::dropdowns::network::helpers::{attach_reveal_toggle, reset_reveal_toggle};

impl PasswordForm {
    pub fn build_password_entry() -> gtk::Entry {
        let entry = gtk::Entry::new();
        attach_reveal_toggle(&entry);
        entry
    }

    pub fn reset_entry(&mut self) {
        self.password_entry.set_text("");
        reset_reveal_toggle(&self.password_entry);
    }
}
