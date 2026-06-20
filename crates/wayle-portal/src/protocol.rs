//! Generated bindings for vendored Wayland protocols.

/// `hyprland-global-shortcuts-v1`: the de-facto wlroots global-shortcuts
/// protocol (used by xdg-desktop-portal-hyprland). The compositor binds keys to
/// `app_id:id` and triggers the registered shortcut objects.
#[allow(non_camel_case_types, unused_imports, clippy::all)]
pub mod hyprland_global_shortcuts_v1 {
    use wayland_client::{self, protocol::*};

    pub mod __interfaces {
        use wayland_client::protocol::__interfaces::*;
        wayland_scanner::generate_interfaces!("protocols/hyprland-global-shortcuts-v1.xml");
    }
    use self::__interfaces::*;

    wayland_scanner::generate_client_code!("protocols/hyprland-global-shortcuts-v1.xml");
}
