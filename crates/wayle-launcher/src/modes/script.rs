//! script mode: user scripts speaking the rofi-script(5) protocol.
//!
//! The script is executed with no argument to produce the list (one entry
//! per line), and re-executed with the selected entry as `argv[1]` on every
//! interaction. `ROFI_RETV` tells it why it was called (0 init, 1 select,
//! 2 custom, 3 delete, 10..=28 kb-custom-N); `ROFI_INFO`/`ROFI_DATA`/
//! `ROFI_INPUT` carry row info, script-owned state, and the query text.
//! Header lines (`\0key\x1fvalue`) set mode options; row suffixes
//! (`text\0icon\x1fname\x1f...`) set per-row metadata.

use std::path::PathBuf;

use async_trait::async_trait;
use tracing::warn;

use crate::{
    item::{IconSource, Item, ItemFlags},
    mode::{Action, ActivateKind, Mode, ModeState},
};

/// A custom script mode (rofi `name:script`).
pub struct ScriptMode {
    name: String,
    script: PathBuf,
    /// `\0data\x1f...` state carried between invocations (`ROFI_DATA`).
    data: Option<String>,
    /// `\0use-hot-keys\x1ftrue`: route kb-custom-N to the script.
    use_hot_keys: bool,
    /// Original entry texts (what the script receives back), parallel to
    /// the item vec.
    texts: Vec<String>,
    /// Per-row `info` values (`ROFI_INFO`).
    infos: Vec<Option<String>>,
}

impl ScriptMode {
    /// Create a script mode named `name` running `script`.
    pub fn new(name: impl Into<String>, script: impl Into<PathBuf>) -> Self {
        Self {
            name: name.into(),
            script: script.into(),
            data: None,
            use_hot_keys: false,
            texts: Vec::new(),
            infos: Vec::new(),
        }
    }

    /// Run the script and turn its output into the next action/state.
    async fn invoke(&mut self, arg: Option<&str>, retv: i32, info: Option<&str>, input: &str) -> ScriptResult {
        let mut command = tokio::process::Command::new(&self.script);
        if let Some(arg) = arg {
            command.arg(arg);
        }
        command.env("ROFI_RETV", retv.to_string());
        command.env("ROFI_INPUT", input);
        if let Some(info) = info {
            command.env("ROFI_INFO", info);
        }
        if let Some(data) = &self.data {
            command.env("ROFI_DATA", data);
        }
        let output = match command.output().await {
            Ok(output) => output,
            Err(error) => {
                warn!(script = %self.script.display(), %error, "script mode exec failed");
                return ScriptResult::Empty;
            }
        };
        let stdout = String::from_utf8_lossy(&output.stdout).into_owned();
        let parsed = parse_output(&stdout);
        if let Some(data) = &parsed.data {
            self.data = Some(data.clone());
        }
        if parsed.use_hot_keys {
            self.use_hot_keys = true;
        }
        if let Some(mode) = &parsed.switch_mode {
            return ScriptResult::Switch(mode.clone());
        }
        if parsed.rows.is_empty() {
            return ScriptResult::Empty;
        }
        ScriptResult::State(self.build_state(parsed))
    }

    fn build_state(&mut self, parsed: ParsedOutput) -> ModeState {
        let mut items = Vec::with_capacity(parsed.rows.len());
        let mut texts = Vec::with_capacity(parsed.rows.len());
        let mut infos = Vec::with_capacity(parsed.rows.len());
        for (index, row) in parsed.rows.into_iter().enumerate() {
            let mut item = row.item;
            if parsed.urgent.contains(&(index as u32)) {
                item.flags |= ItemFlags::URGENT;
            }
            if parsed.active.contains(&(index as u32)) {
                item.flags |= ItemFlags::ACTIVE;
            }
            infos.push(item.info.clone());
            items.push(item);
            texts.push(row.text);
        }
        self.texts = texts;
        self.infos = infos;
        ModeState {
            items,
            prompt: parsed.prompt.unwrap_or_else(|| self.name.clone()),
            message: parsed.message,
            markup_rows: parsed.markup_rows,
            no_custom: parsed.no_custom,
            use_hot_keys: self.use_hot_keys,
            keep_selection: parsed.keep_selection,
            new_selection: parsed.new_selection,
            keep_filter: parsed.keep_filter,
            ..ModeState::default()
        }
    }

    fn row(&self, index: Option<u32>) -> (Option<&str>, Option<&str>) {
        match index {
            Some(row) => (
                self.texts.get(row as usize).map(String::as_str),
                self.infos
                    .get(row as usize)
                    .and_then(|info| info.as_deref()),
            ),
            None => (None, None),
        }
    }
}

enum ScriptResult {
    /// No output: close the launcher (rofi behavior).
    Empty,
    /// A fresh list.
    State(ModeState),
    /// `\0switch-mode\x1f<name>`.
    Switch(String),
}

impl ScriptResult {
    fn into_action(self) -> Action {
        match self {
            Self::Empty => Action::Close,
            Self::State(state) => Action::Reload(state),
            Self::Switch(mode) => Action::SwitchMode(mode),
        }
    }
}

#[async_trait]
impl Mode for ScriptMode {
    fn name(&self) -> &str {
        &self.name
    }

    async fn load(&mut self) -> ModeState {
        match self.invoke(None, 0, None, "").await {
            ScriptResult::State(state) => state,
            // An initially-empty script yields an empty list; Close only
            // applies to post-selection invocations.
            ScriptResult::Empty | ScriptResult::Switch(_) => ModeState {
                prompt: self.name.clone(),
                ..ModeState::default()
            },
        }
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, input: &str) -> Action {
        let (text, info) = self.row(index);
        let (arg, retv) = match &kind {
            ActivateKind::Default | ActivateKind::Alt => match text {
                Some(text) => (text.to_owned(), 1),
                None => return Action::Nothing,
            },
            ActivateKind::Custom(custom) => (custom.clone(), 2),
            ActivateKind::KbCustom(n) => {
                if !self.use_hot_keys {
                    return Action::Nothing;
                }
                (text.unwrap_or(input).to_owned(), 9 + i32::from(*n))
            }
        };
        let info = info.map(ToOwned::to_owned);
        self.invoke(Some(&arg), retv, info.as_deref(), input)
            .await
            .into_action()
    }

    async fn delete(&mut self, index: u32) -> Action {
        let (Some(text), info) = self.row(Some(index)) else {
            return Action::Nothing;
        };
        let text = text.to_owned();
        let info = info.map(ToOwned::to_owned);
        self.invoke(Some(&text), 3, info.as_deref(), "")
            .await
            .into_action()
    }
}

/// One parsed row: the item plus the original entry text.
pub(crate) struct ParsedRow {
    /// The rendered item.
    pub(crate) item: Item,
    /// Original entry text (what a script/dmenu consumer receives back).
    pub(crate) text: String,
}

#[derive(Default)]
struct ParsedOutput {
    rows: Vec<ParsedRow>,
    prompt: Option<String>,
    message: Option<String>,
    markup_rows: bool,
    no_custom: bool,
    use_hot_keys: bool,
    keep_selection: bool,
    keep_filter: bool,
    new_selection: Option<u32>,
    data: Option<String>,
    switch_mode: Option<String>,
    urgent: Vec<u32>,
    active: Vec<u32>,
}

/// Parse a full script stdout: header option lines + entry rows.
fn parse_output(stdout: &str) -> ParsedOutput {
    let mut parsed = ParsedOutput::default();
    // `\0delim\x1fX` changes the row separator; scan for it first.
    let delim = stdout
        .split('\n')
        .find_map(|line| line.strip_prefix("\0delim\x1f"))
        .and_then(|value| unescape_delim(value.trim_end_matches('\n')))
        .unwrap_or('\n');

    for entry in stdout.split(delim) {
        let entry = entry.strip_suffix('\n').unwrap_or(entry);
        if entry.is_empty() {
            continue;
        }
        if let Some(header) = entry.strip_prefix('\0') {
            apply_header(header, &mut parsed);
        } else {
            parsed.rows.push(parse_row(entry));
        }
    }
    parsed
}

fn apply_header(header: &str, parsed: &mut ParsedOutput) {
    let (key, value) = match header.split_once('\u{1f}') {
        Some((key, value)) => (key, value),
        None => (header, ""),
    };
    let truthy = value == "true";
    match key {
        "prompt" => parsed.prompt = Some(value.to_owned()),
        "message" => parsed.message = Some(value.to_owned()),
        "markup-rows" => parsed.markup_rows = truthy,
        "no-custom" => parsed.no_custom = truthy,
        "use-hot-keys" => parsed.use_hot_keys = truthy,
        "keep-selection" => parsed.keep_selection = truthy,
        "keep-filter" => parsed.keep_filter = truthy,
        "new-selection" => parsed.new_selection = value.parse().ok(),
        "data" => parsed.data = Some(value.to_owned()),
        "switch-mode" => parsed.switch_mode = Some(value.to_owned()),
        "urgent" => parsed.urgent = parse_ranges(value),
        "active" => parsed.active = parse_ranges(value),
        "delim" => {} // handled up front
        "theme" => {
            // No rasi: accepted and ignored (documented wayle divergence).
            tracing::debug!("script mode: theme header ignored");
        }
        other => warn!(header = %other, "unknown script mode option"),
    }
}

/// Parse `text[\0key\x1fvalue\x1fkey\x1fvalue...]` into an item.
pub(crate) fn parse_row(entry: &str) -> ParsedRow {
    let (text, options) = match entry.split_once('\0') {
        Some((text, options)) => (text, Some(options)),
        None => (entry, None),
    };
    let mut item = Item::new(text);
    if let Some(options) = options {
        let mut parts = options.split('\u{1f}');
        while let (Some(key), Some(value)) = (parts.next(), parts.next()) {
            apply_row_option(&mut item, text, key, value);
        }
    }
    ParsedRow {
        item,
        text: text.to_owned(),
    }
}

fn apply_row_option(item: &mut Item, text: &str, key: &str, value: &str) {
    let truthy = value == "true";
    match key {
        // Comma-separated icon values are fallbacks; take the first.
        "icon" => {
            let first = value.split(',').next().unwrap_or(value);
            item.icon = Some(if first.starts_with('/') {
                IconSource::File(PathBuf::from(first))
            } else {
                IconSource::Name(first.to_owned())
            });
        }
        "display" => item.display = value.to_owned(),
        "meta" => item.match_text = format!("{text} {value}"),
        "info" => item.info = Some(value.to_owned()),
        "nonselectable" if truthy => item.flags |= ItemFlags::NONSELECTABLE,
        "permanent" if truthy => item.flags |= ItemFlags::PERMANENT,
        "urgent" if truthy => item.flags |= ItemFlags::URGENT,
        "active" if truthy => item.flags |= ItemFlags::ACTIVE,
        _ => {}
    }
}

/// dmenu-style index list: `1,3-5,8`.
pub(crate) fn parse_ranges(raw: &str) -> Vec<u32> {
    let mut out = Vec::new();
    for part in raw.split(',') {
        let part = part.trim();
        if let Some((start, end)) = part.split_once('-') {
            if let (Ok(start), Ok(end)) = (start.trim().parse::<u32>(), end.trim().parse::<u32>()) {
                out.extend(start..=end);
            }
        } else if let Ok(index) = part.parse::<u32>() {
            out.push(index);
        }
    }
    out
}

fn unescape_delim(value: &str) -> Option<char> {
    match value {
        "\\n" => Some('\n'),
        "\\0" => Some('\0'),
        "\\t" => Some('\t'),
        other => other.chars().next(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_headers_and_rows() {
        let output = "\0prompt\u{1f}pick\n\0message\u{1f}hello\n\0markup-rows\u{1f}true\nfirst\nsecond\0icon\u{1f}firefox\u{1f}info\u{1f}payload\n";
        let parsed = parse_output(output);
        assert_eq!(parsed.prompt.as_deref(), Some("pick"));
        assert_eq!(parsed.message.as_deref(), Some("hello"));
        assert!(parsed.markup_rows);
        assert_eq!(parsed.rows.len(), 2);
        assert_eq!(parsed.rows[0].text, "first");
        assert_eq!(
            parsed.rows[1].item.icon,
            Some(IconSource::Name("firefox".into()))
        );
        assert_eq!(parsed.rows[1].item.info.as_deref(), Some("payload"));
    }

    #[test]
    fn row_options_meta_display_nonselectable() {
        let row = parse_row("entry\0display\u{1f}Pretty\u{1f}meta\u{1f}hidden words\u{1f}nonselectable\u{1f}true");
        assert_eq!(row.text, "entry");
        assert_eq!(row.item.display, "Pretty");
        assert_eq!(row.item.match_text, "entry hidden words");
        assert!(row.item.flags.contains(ItemFlags::NONSELECTABLE));
    }

    #[test]
    fn urgent_active_ranges_apply() {
        assert_eq!(parse_ranges("0,2-4"), vec![0, 2, 3, 4]);
    }

    #[test]
    fn data_and_switch_mode_headers() {
        let parsed = parse_output("\0data\u{1f}state123\n\0switch-mode\u{1f}drun\nrow\n");
        assert_eq!(parsed.data.as_deref(), Some("state123"));
        assert_eq!(parsed.switch_mode.as_deref(), Some("drun"));
    }

    #[tokio::test]
    async fn script_round_trip() {
        let dir = std::env::temp_dir().join(format!("wayle-script-test-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let script = dir.join("mode.sh");
        std::fs::write(
            &script,
            "#!/bin/sh\nif [ \"$ROFI_RETV\" = 0 ]; then\n  printf '\\0prompt\\x1ftest\\n'\n  echo one\n  echo two\nelif [ \"$1\" = one ]; then\n  echo picked-one\nfi\n",
        )
        .unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&script, std::fs::Permissions::from_mode(0o755)).unwrap();

        let mut mode = ScriptMode::new("test", &script);
        let state = mode.load().await;
        assert_eq!(state.prompt, "test");
        assert_eq!(state.items.len(), 2);

        // Selecting "one" reloads with the script's new output.
        let action = mode.activate(Some(0), ActivateKind::Default, "").await;
        let Action::Reload(state) = action else {
            unreachable!("expected reload");
        };
        assert_eq!(state.items[0].display, "picked-one");

        // Selecting "two" produces no output → close.
        let mut mode = ScriptMode::new("test", &script);
        let _ = mode.load().await;
        let action = mode.activate(Some(1), ActivateKind::Default, "").await;
        assert!(matches!(action, Action::Close));

        std::fs::remove_dir_all(&dir).ok();
    }
}
