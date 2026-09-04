//! `emoji` mode: pick an emoji by name, accept to copy it.
//!
//! # Where the data comes from
//!
//! GTK already ships the whole Unicode emoji list, localized, as a GResource
//! its own emoji chooser reads — so there is no table to vendor and no
//! dependency to add. The resource is `/org/gtk/libgtk/emoji/<lang>.data`
//! holding a `a(aussasasu)`, which per `gtkemojichooser.c` is, per emoji:
//!
//! 0. `au` the codepoints (a `0` stands for a variation selector or a skin
//!    tone modifier, and is what the chooser substitutes into);
//! 1. `s` the English name;
//! 2. `s` the name in the current locale;
//! 3. `as` the English keywords;
//! 4. `as` the keywords in the current locale;
//! 5. `u` the group it belongs to.
//!
//! Both name sets are fed to the matcher: someone on a French desktop still
//! reaches 🍺 by typing "beer", and someone typing "bière" still gets it.
//!
//! The resource is registered by libgtk, which the shell process the engine
//! runs inside has already loaded. A build with no GTK finds nothing and the
//! mode reports itself unavailable rather than showing an empty list.

use async_trait::async_trait;
use tracing::{debug, warn};

use crate::{
    item::Item,
    mode::{Action, ActivateKind, Mode, ModeState},
};

/// Where GTK keeps its emoji list.
const RESOURCE_PREFIX: &str = "/org/gtk/libgtk/emoji/";

/// The GVariant shape of that resource.
const DATA_TYPE: &str = "a(aussasasu)";

/// One emoji, as the mode needs it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Emoji {
    /// The glyph itself.
    pub glyph: String,
    /// What to call it, in the current locale where there is one.
    pub name: String,
    /// Everything the matcher should see beyond the name.
    pub keywords: Vec<String>,
}

/// Assembles the glyph from a codepoint list.
///
/// A `0` in the list is the chooser's placeholder for a variation selector or
/// a skin-tone modifier. With no modifier chosen, GTK substitutes
/// `U+FE0F VARIATION SELECTOR-16`, which is what makes the emoji render as an
/// emoji rather than as monochrome text — so this does the same.
#[must_use]
pub fn glyph_from_codes(codes: &[u32]) -> String {
    codes
        .iter()
        .map(|code| if *code == 0 { 0xfe0f } else { *code })
        .filter_map(char::from_u32)
        .collect()
}

/// Turns one row of GTK's table into an [`Emoji`].
///
/// Both languages' names and keywords go into the keyword list so either
/// reaches the emoji; the localized name is the one shown.
fn row_to_emoji(
    codes: &[u32],
    name_en: &str,
    name: &str,
    keywords_en: &[String],
    keywords: &[String],
) -> Option<Emoji> {
    let glyph = glyph_from_codes(codes);
    if glyph.is_empty() {
        return None;
    }
    // A locale with no translation leaves the localized fields empty.
    let shown = if name.is_empty() { name_en } else { name };
    if shown.is_empty() {
        return None;
    }

    let mut all: Vec<String> = Vec::new();
    // Compared against what is *shown*, not against the localized field: an
    // untranslated locale shows the English name, and repeating it as a
    // keyword would match it twice.
    if !name_en.is_empty() && name_en != shown {
        all.push(name_en.to_owned());
    }
    all.extend(keywords.iter().cloned());
    all.extend(
        keywords_en
            .iter()
            .filter(|keyword| !keywords.contains(keyword))
            .cloned(),
    );

    Some(Emoji {
        glyph,
        name: shown.to_owned(),
        keywords: all,
    })
}

/// Reads GTK's emoji table for the current locale, falling back to English.
///
/// Empty when no GTK resource is registered, which is what a non-GTK build
/// of the engine sees.
#[must_use]
pub fn available() -> Vec<Emoji> {
    let Ok(data_type) = glib::VariantTy::new(DATA_TYPE) else {
        return Vec::new();
    };
    for language in languages() {
        let path = format!("{RESOURCE_PREFIX}{language}.data");
        let Ok(bytes) =
            gio::functions::resources_lookup_data(&path, gio::ResourceLookupFlags::NONE)
        else {
            continue;
        };
        let variant = glib::Variant::from_bytes_with_type(&bytes, data_type);
        let emojis = parse(&variant);
        if !emojis.is_empty() {
            debug!(language, count = emojis.len(), "loaded GTK's emoji table");
            return emojis;
        }
    }
    warn!("no GTK emoji data found; the emoji mode has nothing to show");
    Vec::new()
}

/// Language tags to try, most specific first.
///
/// `fr_CA.UTF-8` is asked for as `fr-ca`, then `fr`, then English — the same
/// widening GTK's chooser does, so a locale with no translation still gets a
/// list rather than nothing.
fn languages() -> Vec<String> {
    let locale = ["LC_ALL", "LC_MESSAGES", "LANG"]
        .into_iter()
        .find_map(|key| std::env::var(key).ok())
        .unwrap_or_default();
    languages_for(&locale)
}

/// The language tags to try for a POSIX locale string.
///
/// Split out from the environment so the widening is testable without
/// mutating process state.
fn languages_for(locale: &str) -> Vec<String> {
    let mut tags = Vec::new();
    let tag = locale
        .split(['.', '@'])
        .next()
        .unwrap_or_default()
        .replace('_', "-")
        .to_lowercase();

    // `C` and `POSIX` name the absence of a locale, not a language.
    if !tag.is_empty() && tag != "c" && tag != "posix" {
        tags.push(tag.clone());
        if let Some((base, _)) = tag.split_once('-') {
            tags.push(base.to_owned());
        }
    }
    if !tags.iter().any(|tag| tag == "en") {
        tags.push(String::from("en"));
    }
    tags
}

/// Reads every row of the table.
fn parse(variant: &glib::Variant) -> Vec<Emoji> {
    let mut emojis = Vec::with_capacity(variant.n_children());
    for index in 0..variant.n_children() {
        let row = variant.child_value(index);
        let Some(codes) = row.try_child_get::<Vec<u32>>(0).ok().flatten() else {
            continue;
        };
        let name_en = row.try_child_get::<String>(1).ok().flatten();
        let name = row.try_child_get::<String>(2).ok().flatten();
        let keywords_en = row
            .try_child_get::<Vec<String>>(3)
            .ok()
            .flatten()
            .unwrap_or_default();
        let keywords = row
            .try_child_get::<Vec<String>>(4)
            .ok()
            .flatten()
            .unwrap_or_default();

        if let Some(emoji) = row_to_emoji(
            &codes,
            name_en.as_deref().unwrap_or_default(),
            name.as_deref().unwrap_or_default(),
            &keywords_en,
            &keywords,
        ) {
            emojis.push(emoji);
        }
    }
    emojis
}

/// The row for one emoji: the glyph, its name, and the keywords behind it.
fn item_for(emoji: &Emoji) -> Item {
    let display = format!("{}  {}", emoji.glyph, emoji.name);
    Item {
        // The keywords are invisible but matchable, which is the whole point
        // of `match_text` being separate.
        match_text: format!("{} {}", emoji.name, emoji.keywords.join(" ")),
        display,
        icon: None,
        // The glyph alone, so an accept copies the emoji and not its name.
        info: Some(emoji.glyph.clone()),
        flags: crate::item::ItemFlags::empty(),
    }
}

/// The `emoji` mode.
pub struct EmojiMode {
    emojis: Vec<Emoji>,
}

impl EmojiMode {
    /// Reads GTK's table. Cheap enough to do per session: it is one resource
    /// lookup and a few thousand rows, and doing it lazily would mean the
    /// first keystroke pays for it.
    #[must_use]
    pub fn new() -> Self {
        Self {
            emojis: available(),
        }
    }
}

impl Default for EmojiMode {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl Mode for EmojiMode {
    fn name(&self) -> &str {
        "emoji"
    }

    async fn load(&mut self) -> ModeState {
        ModeState {
            items: self.emojis.iter().map(item_for).collect(),
            prompt: String::from("emoji"),
            ..ModeState::default()
        }
    }

    async fn activate(&mut self, index: Option<u32>, _kind: ActivateKind, _input: &str) -> Action {
        let Some(emoji) = index.and_then(|index| self.emojis.get(index as usize)) else {
            return Action::Nothing;
        };
        Action::Copy(emoji.glyph.clone())
    }

    /// Typed text that matches no emoji is not an emoji.
    fn allows_custom(&self) -> bool {
        false
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_codepoint_list_becomes_its_glyph() {
        // 😀 is a single codepoint.
        assert_eq!(glyph_from_codes(&[0x1f600]), "😀");
        // A flag is a pair of regional indicators.
        assert_eq!(glyph_from_codes(&[0x1f1e9, 0x1f1f0]), "🇩🇰");
    }

    #[test]
    fn a_zero_becomes_the_variation_selector_that_makes_it_render_as_emoji() {
        // GTK's table uses 0 as the placeholder. Dropping it instead would
        // give the monochrome text form of ❤.
        let glyph = glyph_from_codes(&[0x2764, 0]);
        assert_eq!(glyph, "❤\u{fe0f}");
        assert!(glyph.ends_with('\u{fe0f}'));
    }

    #[test]
    fn a_row_with_no_usable_codepoints_is_dropped() {
        // Surrogates and out-of-range values are not characters; a row of
        // them would be an empty, unselectable list entry.
        assert!(row_to_emoji(&[], "grinning", "grinning", &[], &[]).is_none());
        assert!(row_to_emoji(&[0xd800], "bad", "bad", &[], &[]).is_none());
        // A glyph with no name is equally useless: nothing to search by.
        assert!(row_to_emoji(&[0x1f600], "", "", &[], &[]).is_none());
    }

    #[test]
    fn the_localized_name_is_shown_and_english_stays_searchable() {
        let emoji = row_to_emoji(
            &[0x1f37a],
            "beer mug",
            "chope de bière",
            &[String::from("beer")],
            &[String::from("bière")],
        )
        .expect("a complete row");

        assert_eq!(emoji.name, "chope de bière");
        assert!(
            emoji.keywords.contains(&String::from("beer mug")),
            "the English name must stay reachable: {:?}",
            emoji.keywords
        );
        assert!(emoji.keywords.contains(&String::from("bière")));
        assert!(emoji.keywords.contains(&String::from("beer")));
    }

    #[test]
    fn an_untranslated_locale_falls_back_to_the_english_name() {
        let emoji = row_to_emoji(&[0x1f37a], "beer mug", "", &[String::from("beer")], &[])
            .expect("a row with no translation");
        assert_eq!(emoji.name, "beer mug");
        // And the name is not also repeated into the keywords.
        assert!(!emoji.keywords.contains(&String::from("beer mug")));
    }

    #[test]
    fn a_keyword_present_in_both_languages_is_not_listed_twice() {
        let emoji = row_to_emoji(
            &[0x1f600],
            "grin",
            "grin",
            &[String::from("face"), String::from("smile")],
            &[String::from("face")],
        )
        .expect("a complete row");
        assert_eq!(emoji.keywords, ["face", "smile"], "{:?}", emoji.keywords);
    }

    #[test]
    fn the_row_matches_on_keywords_but_shows_only_the_glyph_and_name() {
        let emoji = Emoji {
            glyph: String::from("🍺"),
            name: String::from("beer mug"),
            keywords: vec![String::from("bar"), String::from("drink")],
        };
        let item = item_for(&emoji);

        assert_eq!(item.display, "🍺  beer mug");
        assert!(item.match_text.contains("drink"));
        assert!(
            !item.display.contains("drink"),
            "keywords are for matching, not for reading"
        );
        // Accepting copies the glyph on its own.
        assert_eq!(item.info.as_deref(), Some("🍺"));
    }

    #[test]
    fn the_language_list_widens_before_falling_back_to_english() {
        // A region-specific locale tries its own table, then the language's,
        // then English — otherwise `fr_CA` would find nothing at all, since
        // GTK ships `fr.data` and no `fr-ca.data`.
        assert_eq!(languages_for("fr_CA.UTF-8"), ["fr-ca", "fr", "en"]);
        assert_eq!(languages_for("de_DE@euro"), ["de-de", "de", "en"]);
        assert_eq!(languages_for("fr"), ["fr", "en"]);
    }

    #[test]
    fn a_locale_that_names_no_language_just_asks_for_english() {
        for locale in ["C", "POSIX", "c", ""] {
            assert_eq!(languages_for(locale), ["en"], "{locale}");
        }
        // English asks for English once, not twice.
        assert_eq!(languages_for("en_GB.UTF-8"), ["en-gb", "en"]);
    }
}
