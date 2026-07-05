mod bootstrap;

pub(crate) use wayle_shell_core::helpers::layer_shell;
pub(crate) mod monitors;
pub(crate) mod surface_anim;

pub(crate) use bootstrap::{init_css_provider, init_icons, register_app_actions};
