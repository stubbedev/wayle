/// Render the label format, substituting `{{ count }}`.
pub(super) fn format_label(format: &str, count: u32) -> String {
    format.replace("{{ count }}", &count.to_string())
}
