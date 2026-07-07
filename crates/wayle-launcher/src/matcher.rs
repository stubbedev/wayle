//! Matching/ranking engine.
//!
//! Fuzzy/normal/prefix matching runs on [`nucleo`] worker threads via
//! query rewriting into fzf pattern syntax; regex/glob fall back to a
//! synchronous linear scan (call [`MatchEngine::set_query`] off the UI
//! thread for those if the list is huge).

use std::sync::Arc;

use nucleo::{
    Config, Nucleo,
    pattern::{CaseMatching, Normalization},
};

use crate::item::{Item, ItemFlags};

/// rofi `-matching` methods.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum MatchMethod {
    /// Tokenized substring matching (rofi default).
    #[default]
    Normal,
    /// fzf-style fuzzy matching.
    Fuzzy,
    /// Tokenized prefix matching.
    Prefix,
    /// Whole query is a regular expression.
    Regex,
    /// Tokenized glob matching (`*token*` per token).
    Glob,
}

/// rofi case handling (`-case-sensitive` / `-case-smart` collapsed).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum CaseMode {
    /// Always case-insensitive.
    #[default]
    Insensitive,
    /// Sensitive only when the query contains an uppercase char.
    Smart,
    /// Always case-sensitive.
    Sensitive,
}

/// rofi `-sorting-method`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum SortMethod {
    /// Match-quality order (nucleo/fzf score).
    #[default]
    Fzf,
    /// Levenshtein distance to the query.
    Levenshtein,
}

/// Matching behavior knobs.
#[derive(Debug, Clone)]
pub struct MatcherOptions {
    /// Matching method.
    pub method: MatchMethod,
    /// Case handling.
    pub case: CaseMode,
    /// Split the query into independently-matched tokens (rofi default on).
    pub tokenize: bool,
    /// Strip accents/normalize Unicode while matching.
    pub normalize: bool,
    /// Token prefix that negates a token (rofi `-matching-negate-char`).
    pub negation_char: char,
    /// Rank results by match quality; off = keep item order (rofi default off).
    pub sort: bool,
    /// Ranking method when `sort` is on.
    pub sort_method: SortMethod,
}

impl Default for MatcherOptions {
    fn default() -> Self {
        Self {
            method: MatchMethod::default(),
            case: CaseMode::default(),
            tokenize: true,
            normalize: true,
            negation_char: '-',
            sort: false,
            sort_method: SortMethod::default(),
        }
    }
}

/// Result of a [`MatchEngine::tick`] call.
#[derive(Debug, Clone, Copy)]
pub struct TickStatus {
    /// Match results changed since the last tick.
    pub changed: bool,
    /// Matching is still in progress; keep ticking.
    pub running: bool,
}

/// The engine: owns a nucleo instance plus the scan fallback.
pub struct MatchEngine {
    nucleo: Nucleo<u32>,
    items: Arc<Vec<Item>>,
    permanent: Vec<u32>,
    options: MatcherOptions,
    query: String,
    rewritten: String,
    /// Cached results for the scan path (regex/glob); None = nucleo path.
    scanned: Option<Vec<u32>>,
}

impl MatchEngine {
    /// Create an engine. `notify` fires (from a worker thread) whenever new
    /// match results may be available — pair it with a main-loop wakeup, then
    /// call [`tick`](Self::tick) + [`matched`](Self::matched).
    pub fn new(options: MatcherOptions, notify: Arc<dyn Fn() + Send + Sync>) -> Self {
        Self {
            nucleo: Nucleo::new(Config::DEFAULT, notify, None, 1),
            items: Arc::new(Vec::new()),
            permanent: Vec::new(),
            options,
            query: String::new(),
            rewritten: String::new(),
            scanned: None,
        }
    }

    /// Current items.
    pub fn items(&self) -> &Arc<Vec<Item>> {
        &self.items
    }

    /// Current matching options.
    pub fn options(&self) -> &MatcherOptions {
        &self.options
    }

    /// Replace the item set (mode load/reload). Resets match state.
    pub fn set_items(&mut self, items: Arc<Vec<Item>>) {
        self.items = items;
        self.permanent = collect_flagged(&self.items, ItemFlags::PERMANENT);
        self.nucleo.restart(true);
        let injector = self.nucleo.injector();
        for (index, item) in self.items.iter().enumerate() {
            #[allow(clippy::cast_possible_truncation)]
            injector.push(index as u32, |_, columns| {
                columns[0] = item.match_text.as_str().into();
            });
        }
        self.rewritten.clear();
        let query = std::mem::take(&mut self.query);
        self.set_query(&query);
    }

    /// Replace matching options (config/session change). Re-runs the query.
    pub fn set_options(&mut self, options: MatcherOptions) {
        self.options = options;
        self.rewritten.clear();
        let query = std::mem::take(&mut self.query);
        self.set_query(&query);
    }

    /// Update the query. Regex/glob methods scan synchronously here; the
    /// nucleo methods return immediately and deliver via `notify`/`tick`.
    pub fn set_query(&mut self, query: &str) {
        self.query = query.to_owned();
        match self.options.method {
            MatchMethod::Regex => {
                self.scanned = Some(self.scan_regex());
            }
            MatchMethod::Glob => {
                self.scanned = Some(self.scan_glob());
            }
            MatchMethod::Normal | MatchMethod::Fuzzy | MatchMethod::Prefix => {
                self.scanned = None;
                let rewritten = rewrite_query(&self.query, &self.options);
                // Append hint only when the new pattern strictly extends the
                // old one and contains no negation (negations grow matches).
                let append = rewritten.starts_with(&self.rewritten) && !rewritten.contains('!');
                self.nucleo.pattern.reparse(
                    0,
                    &rewritten,
                    case_matching(&self.options, &self.query),
                    normalization(&self.options),
                    append,
                );
                self.rewritten = rewritten;
            }
        }
    }

    /// Drive nucleo.
    pub fn tick(&mut self) -> TickStatus {
        if self.scanned.is_some() {
            return TickStatus {
                changed: false,
                running: false,
            };
        }
        let status = self.nucleo.tick(10);
        TickStatus {
            changed: status.changed,
            running: status.running,
        }
    }

    /// Ranked matched item indices, plus trailing PERMANENT rows.
    pub fn matched(&mut self) -> Vec<u32> {
        let mut out = match &self.scanned {
            Some(scanned) => scanned.clone(),
            None => {
                let snapshot = self.nucleo.snapshot();
                let mut ids: Vec<u32> =
                    snapshot.matched_items(..).map(|item| *item.data).collect();
                if !self.options.sort || self.query.is_empty() {
                    // rofi default: filter, keep list order.
                    ids.sort_unstable();
                }
                ids
            }
        };
        if self.options.sort && self.options.sort_method == SortMethod::Levenshtein {
            let query = &self.query;
            let items = &self.items;
            out.sort_by_key(|&index| levenshtein(query, &items[index as usize].match_text));
        }
        for &index in &self.permanent {
            if !out.contains(&index) {
                out.push(index);
            }
        }
        out
    }
}

fn collect_flagged(items: &[Item], flag: ItemFlags) -> Vec<u32> {
    #[allow(clippy::cast_possible_truncation)]
    items
        .iter()
        .enumerate()
        .filter(|(_, item)| item.flags.contains(flag))
        .map(|(index, _)| index as u32)
        .collect()
}

fn case_matching(options: &MatcherOptions, query: &str) -> CaseMatching {
    match options.case {
        CaseMode::Insensitive => CaseMatching::Ignore,
        CaseMode::Sensitive => CaseMatching::Respect,
        CaseMode::Smart => {
            if query.chars().any(char::is_uppercase) {
                CaseMatching::Respect
            } else {
                CaseMatching::Ignore
            }
        }
    }
}

fn normalization(options: &MatcherOptions) -> Normalization {
    if options.normalize {
        Normalization::Smart
    } else {
        Normalization::Never
    }
}

/// Rewrite a rofi query into nucleo's fzf pattern syntax for the chosen
/// method: normal → `'substring` atoms, prefix → `^prefix` atoms, fuzzy →
/// untouched fzf syntax. Negation-char tokens become `!` atoms. With
/// tokenize off the whole query is one atom (spaces escaped).
fn rewrite_query(query: &str, options: &MatcherOptions) -> String {
    if query.is_empty() {
        return String::new();
    }
    let kind_prefix = match options.method {
        MatchMethod::Normal => "'",
        MatchMethod::Prefix => "^",
        _ => "",
    };
    let rewrite_token = |token: &str| -> String {
        let (negated, body) = match token.strip_prefix(options.negation_char) {
            Some(rest) if !rest.is_empty() => (true, rest),
            _ => (false, token),
        };
        let mut out = String::with_capacity(body.len() + 2);
        if negated {
            out.push('!');
        }
        out.push_str(kind_prefix);
        out.push_str(body);
        out
    };
    if options.tokenize {
        query
            .split_whitespace()
            .map(rewrite_token)
            .collect::<Vec<_>>()
            .join(" ")
    } else {
        rewrite_token(&query.replace(' ', "\\ "))
    }
}

impl MatchEngine {
    fn case_sensitive_scan(&self) -> bool {
        match self.options.case {
            CaseMode::Insensitive => false,
            CaseMode::Sensitive => true,
            CaseMode::Smart => self.query.chars().any(char::is_uppercase),
        }
    }

    fn scan_regex(&self) -> Vec<u32> {
        if self.query.is_empty() {
            return all_indices(&self.items);
        }
        let built = regex::RegexBuilder::new(&self.query)
            .case_insensitive(!self.case_sensitive_scan())
            .build();
        let Ok(re) = built else {
            // Invalid regex while typing (e.g. trailing '['): match nothing,
            // same as rofi.
            return Vec::new();
        };
        #[allow(clippy::cast_possible_truncation)]
        self.items
            .iter()
            .enumerate()
            .filter(|(_, item)| re.is_match(&item.match_text))
            .map(|(index, _)| index as u32)
            .collect()
    }

    fn scan_glob(&self) -> Vec<u32> {
        if self.query.is_empty() {
            return all_indices(&self.items);
        }
        let case_sensitive = self.case_sensitive_scan();
        let tokens: Vec<&str> = if self.options.tokenize {
            self.query.split_whitespace().collect()
        } else {
            vec![self.query.as_str()]
        };
        let mut patterns = Vec::with_capacity(tokens.len());
        for token in tokens {
            let (negated, body) = match token.strip_prefix(self.options.negation_char) {
                Some(rest) if !rest.is_empty() => (true, rest),
                _ => (false, token),
            };
            // Wrap in `*...*` for substring semantics, but avoid creating
            // `**` (the glob crate rejects it outside a path component).
            let leading = if body.starts_with('*') { "" } else { "*" };
            let trailing = if body.ends_with('*') { "" } else { "*" };
            match glob::Pattern::new(&format!("{leading}{body}{trailing}")) {
                Ok(pattern) => patterns.push((negated, pattern)),
                Err(_) => return Vec::new(),
            }
        }
        let glob_options = glob::MatchOptions {
            case_sensitive,
            require_literal_separator: false,
            require_literal_leading_dot: false,
        };
        #[allow(clippy::cast_possible_truncation)]
        self.items
            .iter()
            .enumerate()
            .filter(|(_, item)| {
                patterns.iter().all(|(negated, pattern)| {
                    pattern.matches_with(&item.match_text, glob_options) != *negated
                })
            })
            .map(|(index, _)| index as u32)
            .collect()
    }
}

fn all_indices(items: &[Item]) -> Vec<u32> {
    #[allow(clippy::cast_possible_truncation)]
    (0..items.len() as u32).collect()
}

/// Levenshtein distance, two-row DP.
fn levenshtein(a: &str, b: &str) -> usize {
    let a: Vec<char> = a.chars().collect();
    let b: Vec<char> = b.chars().collect();
    if a.is_empty() {
        return b.len();
    }
    let mut prev: Vec<usize> = (0..=b.len()).collect();
    let mut current = vec![0usize; b.len() + 1];
    for (i, ca) in a.iter().enumerate() {
        current[0] = i + 1;
        for (j, cb) in b.iter().enumerate() {
            let cost = usize::from(ca != cb);
            current[j + 1] = (prev[j] + cost).min(prev[j + 1] + 1).min(current[j] + 1);
        }
        std::mem::swap(&mut prev, &mut current);
    }
    prev[b.len()]
}

#[cfg(test)]
mod tests {
    use super::*;

    fn items(texts: &[&str]) -> Arc<Vec<Item>> {
        Arc::new(texts.iter().map(|t| Item::new(*t)).collect())
    }

    fn engine(options: MatcherOptions) -> MatchEngine {
        MatchEngine::new(options, Arc::new(|| {}))
    }

    /// Drive nucleo until it settles, then collect matches.
    fn settled_matches(engine: &mut MatchEngine) -> Vec<u32> {
        for _ in 0..100 {
            let _ = engine.tick();
            std::thread::sleep(std::time::Duration::from_millis(1));
        }
        engine.matched()
    }

    #[test]
    fn rewrite_normal_tokens_to_substring_atoms() {
        let options = MatcherOptions::default();
        assert_eq!(rewrite_query("fire fox", &options), "'fire 'fox");
    }

    #[test]
    fn rewrite_prefix_tokens() {
        let options = MatcherOptions {
            method: MatchMethod::Prefix,
            ..Default::default()
        };
        assert_eq!(rewrite_query("fire fox", &options), "^fire ^fox");
    }

    #[test]
    fn rewrite_negation_char() {
        let options = MatcherOptions::default();
        assert_eq!(rewrite_query("fire -fox", &options), "'fire !'fox");
    }

    #[test]
    fn rewrite_no_tokenize_escapes_spaces() {
        let options = MatcherOptions {
            tokenize: false,
            ..Default::default()
        };
        assert_eq!(rewrite_query("fire fox", &options), "'fire\\ fox");
    }

    #[test]
    fn fuzzy_passthrough() {
        let options = MatcherOptions {
            method: MatchMethod::Fuzzy,
            ..Default::default()
        };
        assert_eq!(rewrite_query("ffx -web", &options), "ffx !web");
    }

    #[test]
    fn normal_matching_filters_and_keeps_order() {
        let mut engine = engine(MatcherOptions::default());
        engine.set_items(items(&["Firefox", "Files", "Terminal", "fire pit"]));
        engine.set_query("fire");
        assert_eq!(settled_matches(&mut engine), vec![0, 3]);
    }

    #[test]
    fn negation_excludes() {
        let mut engine = engine(MatcherOptions::default());
        engine.set_items(items(&["Firefox", "fire pit", "Files"]));
        engine.set_query("fi -fox");
        assert_eq!(settled_matches(&mut engine), vec![1, 2]);
    }

    #[test]
    fn smart_case_respects_uppercase() {
        let options = MatcherOptions {
            case: CaseMode::Smart,
            ..Default::default()
        };
        let mut engine = engine(options);
        engine.set_items(items(&["firefox", "FireFox"]));
        engine.set_query("FireF");
        assert_eq!(settled_matches(&mut engine), vec![1]);
    }

    #[test]
    fn regex_scan() {
        let options = MatcherOptions {
            method: MatchMethod::Regex,
            ..Default::default()
        };
        let mut engine = engine(options);
        engine.set_items(items(&["Firefox", "Files", "Terminal"]));
        engine.set_query("^fi.*x$");
        assert_eq!(engine.matched(), vec![0]);
        engine.set_query("[invalid");
        assert_eq!(engine.matched(), Vec::<u32>::new());
    }

    #[test]
    fn glob_scan_with_negation() {
        let options = MatcherOptions {
            method: MatchMethod::Glob,
            ..Default::default()
        };
        let mut engine = engine(options);
        engine.set_items(items(&["Firefox", "fire pit", "Files"]));
        engine.set_query("fi* -fox");
        assert_eq!(engine.matched(), vec![1, 2]);
    }

    #[test]
    fn permanent_rows_survive_filter() {
        let mut engine = engine(MatcherOptions::default());
        let mut list: Vec<Item> = vec![Item::new("Firefox"), Item::new("Files")];
        let mut quit = Item::new("Quit");
        quit.flags |= ItemFlags::PERMANENT;
        list.push(quit);
        engine.set_items(Arc::new(list));
        engine.set_query("fire");
        assert_eq!(settled_matches(&mut engine), vec![0, 2]);
    }

    #[test]
    fn levenshtein_distance() {
        assert_eq!(levenshtein("kitten", "sitting"), 3);
        assert_eq!(levenshtein("", "abc"), 3);
        assert_eq!(levenshtein("abc", "abc"), 0);
    }

    #[test]
    fn levenshtein_sort_orders_by_distance() {
        let options = MatcherOptions {
            sort: true,
            sort_method: SortMethod::Levenshtein,
            ..Default::default()
        };
        let mut engine = engine(options);
        engine.set_items(items(&["firefight", "fire", "firefox"]));
        engine.set_query("fire");
        assert_eq!(settled_matches(&mut engine), vec![1, 2, 0]);
    }

    #[test]
    fn empty_query_matches_all_in_order() {
        let mut engine = engine(MatcherOptions::default());
        engine.set_items(items(&["b", "a", "c"]));
        engine.set_query("");
        assert_eq!(settled_matches(&mut engine), vec![0, 1, 2]);
    }
}
