use std::sync::Arc;

#[allow(deprecated)]
use gtk4::prelude::StyleContextExt;
use gtk4::prelude::WidgetExt;
use relm4::gtk;
use wayle_config::{ConfigService, schemas::styling::Size};

const REM_BASE: f32 = 16.0;
const BASE_ICON_SIZE: f32 = 1.25;

fn rem_to_px_rounded(rem: f32, scale: f32) -> i32 {
    (rem * scale * REM_BASE).round() as i32
}

/// Resolves a [`Size`] to rounded pixels at `base_rem`: scale multipliers use
/// the rem base and bar scale, pixel values are taken literally.
fn size_to_px_rounded(size: Size, base_rem: f32, scale: f32) -> i32 {
    match size {
        Size::Scale(value) => rem_to_px_rounded(base_rem * value, scale),
        Size::Px(value) => value.round() as i32,
    }
}

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

    let item_gap_px = size_to_px_rounded(systray_config.item_gap.get(), 1.0, bar_scale);
    let icon_size_px =
        size_to_px_rounded(systray_config.icon_scale.get(), BASE_ICON_SIZE, bar_scale);
    let internal_padding_px =
        size_to_px_rounded(systray_config.internal_padding.get(), 1.0, bar_scale);

    format!(
        "* {{ --systray-item-gap-px: {item_gap_px}; --systray-icon-size-px: {icon_size_px}; --systray-internal-padding-px: {internal_padding_px}; }}"
    )
}
