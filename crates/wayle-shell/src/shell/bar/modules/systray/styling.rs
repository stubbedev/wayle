use std::sync::Arc;

#[allow(deprecated)]
use gtk4::prelude::StyleContextExt;
use gtk4::prelude::WidgetExt;
use relm4::gtk;
use wayle_config::{ConfigService, schemas::modules::systray::ICON_BASE_REM};

pub(super) fn init_css_provider(
    widget: &impl WidgetExt,
    config_service: &Arc<ConfigService>,
) -> gtk::CssProvider {
    let provider = gtk::CssProvider::new();

    #[allow(deprecated)]
    widget
        .style_context()
        .add_provider(&provider, gtk::STYLE_PROVIDER_PRIORITY_USER);

    reload_css(&provider, config_service);
    provider
}

pub(super) fn reload_css(provider: &gtk::CssProvider, config_service: &Arc<ConfigService>) {
    let css = build_css(config_service);
    provider.load_from_string(&css);
}

fn build_css(config_service: &Arc<ConfigService>) -> String {
    let full_config = config_service.config();
    let systray_config = &full_config.modules.systray;
    let bar_config = &full_config.bar;

    let bar_scale = bar_config.scale.get().value();

    let item_gap_px = systray_config.item_gap.get().resolve_rem(1.0, bar_scale).round() as i32;
    let icon_size_px =
        systray_config.icon_scale.get().resolve_rem(ICON_BASE_REM, bar_scale).round() as i32;
    let internal_padding_px = systray_config
        .internal_padding
        .get()
        .resolve_rem(1.0, bar_scale)
        .round() as i32;

    format!(
        "* {{ --systray-item-gap-px: {item_gap_px}; --systray-icon-size-px: {icon_size_px}; --systray-internal-padding-px: {internal_padding_px}; }}"
    )
}
