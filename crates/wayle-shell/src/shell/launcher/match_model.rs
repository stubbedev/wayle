//! Virtualized [`gio::ListModel`] over the engine's matched rows.
//!
//! Holds the full item vec plus the matched-index list; each refilter swaps
//! the index list and emits one `items_changed`, so `GtkListView` only ever
//! binds the visible rows — no per-keystroke widget churn at 10k+ items.

use std::sync::Arc;

use relm4::gtk::{
    gio,
    gio::{prelude::*, subclass::prelude::*},
    glib,
};
use wayle_launcher::Item;

/// One row handed to the list factory (via [`glib::BoxedAnyObject`]).
#[derive(Debug, Clone)]
pub(super) struct Row {
    /// The matched item.
    pub item: Item,
}

mod imp {
    use std::cell::RefCell;

    use super::*;

    #[derive(Default)]
    pub struct MatchModel {
        pub items: RefCell<Arc<Vec<Item>>>,
        pub matched: RefCell<Vec<u32>>,
    }

    #[glib::object_subclass]
    impl ObjectSubclass for MatchModel {
        const NAME: &'static str = "WayleLauncherMatchModel";
        type Type = super::MatchModel;
        type Interfaces = (gio::ListModel,);
    }

    impl ObjectImpl for MatchModel {}

    impl ListModelImpl for MatchModel {
        fn item_type(&self) -> glib::Type {
            glib::BoxedAnyObject::static_type()
        }

        fn n_items(&self) -> u32 {
            u32::try_from(self.matched.borrow().len()).unwrap_or(u32::MAX)
        }

        fn item(&self, position: u32) -> Option<glib::Object> {
            let matched = self.matched.borrow();
            let item_index = *matched.get(position as usize)?;
            let items = self.items.borrow();
            let item = items.get(item_index as usize)?.clone();
            Some(glib::BoxedAnyObject::new(Row { item }).upcast::<glib::Object>())
        }
    }
}

glib::wrapper! {
    /// See the module docs.
    pub struct MatchModel(ObjectSubclass<imp::MatchModel>)
        @implements gio::ListModel;
}

impl Default for MatchModel {
    fn default() -> Self {
        glib::Object::new()
    }
}

impl MatchModel {
    /// Swap in a new item set + matched indices; emits one `items_changed`.
    pub fn update(&self, items: Arc<Vec<Item>>, matched: Vec<u32>) {
        let old_len = self.imp().n_items();
        let new_len = u32::try_from(matched.len()).unwrap_or(u32::MAX);
        *self.imp().items.borrow_mut() = items;
        *self.imp().matched.borrow_mut() = matched;
        self.items_changed(0, old_len, new_len);
    }

    /// The item-vec index behind a list position.
    pub fn item_index(&self, position: u32) -> Option<u32> {
        self.imp().matched.borrow().get(position as usize).copied()
    }

    /// Number of matched rows.
    pub fn len(&self) -> u32 {
        self.imp().n_items()
    }
}
