use std::sync::Arc;

use relm4::gtk;
use wayle_config::{
    ConfigService,
    schemas::{
        modules::hyprland_workspaces::{ICON_BASE_REM, LABEL_BASE_REM},
        styling::ThemeProvider,
    },
};
use wayle_widgets::{prelude::BarSettings, styling::resolve_color};

use super::helpers::workspace_id_css_class;

pub fn apply_styling(
    provider: &gtk::CssProvider,
    config_service: &Arc<ConfigService>,
    settings: &BarSettings,
) {
    let config = config_service.config();
    let ws_config = &config.modules.hyprland_workspaces;
    let is_wayle_theme = matches!(config.styling.theme_provider.get(), ThemeProvider::Wayle);

    let active_color = resolve_color(&ws_config.active_color, is_wayle_theme);
    let active_on_other_color =
        resolve_color(&ws_config.active_on_other_monitor_color, is_wayle_theme);
    let occupied_color = resolve_color(&ws_config.occupied_color, is_wayle_theme);
    let empty_color = resolve_color(&ws_config.empty_color, is_wayle_theme);
    let container_bg_color = resolve_color(&ws_config.container_bg_color, is_wayle_theme);
    let border_color = resolve_color(&ws_config.border_color, is_wayle_theme);
    let border_width = settings.border_width.get();

    let bar_scale = config.bar.scale.get().value();
    let is_vertical = settings.is_vertical.get();

    let icon_size_px = ws_config
        .icon_size
        .get()
        .resolve_rem(ICON_BASE_REM, bar_scale)
        .round() as i32;
    let label_size_px = ws_config
        .label_size
        .get()
        .resolve_rem(LABEL_BASE_REM, bar_scale)
        .round() as i32;
    let workspace_padding_px = ws_config
        .workspace_padding
        .get()
        .resolve_rem(1.0, bar_scale)
        .round() as i32;

    let (margin_vertical_px, margin_horizontal_px) = if is_vertical {
        (workspace_padding_px, 0)
    } else {
        (0, workspace_padding_px)
    };

    let mut css = format!(
        ".workspaces {{ \
            --ws-active-color: {active_color}; \
            --ws-active-other-monitor-color: {active_on_other_color}; \
            --ws-occupied-color: {occupied_color}; \
            --ws-empty-color: {empty_color}; \
            --ws-container-bg-color: {container_bg_color}; \
            --ws-border-color: {border_color}; \
            --ws-border-width: {border_width}px; \
            --ws-icon-size-px: {icon_size_px}; \
            --ws-label-size-px: {label_size_px}; \
            --ws-margin-vertical-px: {margin_vertical_px}; \
            --ws-margin-horizontal-px: {margin_horizontal_px}; \
        }}"
    );

    for (workspace_id, style) in &ws_config.workspace_map.get() {
        let Some(color) = style.color.as_ref() else {
            continue;
        };

        let id_class = workspace_id_css_class(i64::from(*workspace_id));
        let color_css = color.to_css();
        css.push_str(&format!(
            ".workspaces .workspace.{id_class} {{ --ws-override-color: {color_css}; }}"
        ));
    }

    provider.load_from_string(&css);
}
