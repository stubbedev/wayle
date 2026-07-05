//! Internationalization for wayle-shell runtime labels.

use std::sync::OnceLock;

use i18n_embed::{
    DesktopLanguageRequester, LanguageLoader,
    fluent::{FluentLanguageLoader, fluent_language_loader},
};
use rust_embed::RustEmbed;

#[derive(RustEmbed)]
#[folder = "locales/"]
#[include = "*/wayle-shell-core.ftl"]
struct Localizations;

static LOADER: OnceLock<FluentLanguageLoader> = OnceLock::new();

#[allow(clippy::expect_used)]
pub fn loader() -> &'static FluentLanguageLoader {
    LOADER.get_or_init(|| {
        let loader = fluent_language_loader!();
        loader
            .load_fallback_language(&Localizations)
            .expect("embedded FTL resources are valid");

        let requested = DesktopLanguageRequester::requested_languages();
        let _ = i18n_embed::select(&loader, &Localizations, &requested);

        loader
    })
}

/// Translates a Fluent message id, validated at compile time against the FTL
/// assets of the invoking crate (each caller crate carries an `i18n.toml`
/// pointing at this crate's `locales/`).
#[macro_export]
macro_rules! t {
    ($message_id:literal) => {{
        i18n_embed_fl::fl!($crate::i18n::loader(), $message_id)
    }};
    ($message_id:literal, $($args:tt)*) => {{
        i18n_embed_fl::fl!($crate::i18n::loader(), $message_id, $($args)*)
    }};
}

/// Translates a dynamic (runtime) Fluent message id, without compile-time
/// validation.
#[macro_export]
macro_rules! td {
    ($message_id:expr) => {{ $crate::i18n::loader().get($message_id) }};
}

pub use crate::{t, td};
