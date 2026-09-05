//! Every locale has to declare the same message ids.
//!
//! A missing id is not a build error and not a runtime error: Fluent falls
//! back to the default locale, so the string silently comes out in English.
//! That is how the French VPN form ended up with 36 of its 53 field labels in
//! English while looking, from the code, entirely translated.

use std::{
    collections::BTreeSet,
    fs,
    path::{Path, PathBuf},
};

/// The message ids a Fluent file declares.
///
/// Only top-level ids count: an attribute line (`.label = …`) belongs to the
/// message above it, a term (`-brand = …`) is private to the file, and a
/// continuation line is indented.
fn message_ids(source: &str) -> BTreeSet<String> {
    source
        .lines()
        .filter_map(|line| {
            if line.starts_with(['#', ' ', '\t', '.', '-']) {
                return None;
            }
            let (id, _) = line.split_once(" =")?;
            let id = id.trim();
            (!id.is_empty()).then(|| id.to_owned())
        })
        .collect()
}

fn locales_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("locales")
}

/// Every locale directory, by name.
fn locales() -> Vec<String> {
    let mut names: Vec<String> = fs::read_dir(locales_dir())
        .expect("locales/ must exist")
        .filter_map(Result::ok)
        .filter(|entry| entry.path().is_dir())
        .map(|entry| entry.file_name().to_string_lossy().into_owned())
        .collect();
    names.sort();
    names
}

/// The ids one locale declares, read from the partials rather than the
/// generated bundle so the test does not depend on a build having run.
fn ids_for(locale: &str) -> BTreeSet<String> {
    fn walk(dir: &Path, into: &mut BTreeSet<String>) {
        let Ok(entries) = fs::read_dir(dir) else {
            return;
        };
        for entry in entries.filter_map(Result::ok) {
            let path = entry.path();
            if path.is_dir() {
                walk(&path, into);
            } else if path
                .file_name()
                .and_then(|name| name.to_str())
                .is_some_and(|name| name.starts_with('_') && name.ends_with(".ftl"))
            {
                let source = fs::read_to_string(&path).expect("readable ftl");
                into.extend(message_ids(&source));
            }
        }
    }

    let mut ids = BTreeSet::new();
    walk(&locales_dir().join(locale), &mut ids);
    ids
}

const REFERENCE: &str = "en-US";

#[test]
fn every_locale_declares_every_reference_id() {
    let reference = ids_for(REFERENCE);
    assert!(
        !reference.is_empty(),
        "the {REFERENCE} locale declared nothing; the test is reading the wrong place"
    );

    for locale in locales() {
        if locale == REFERENCE {
            continue;
        }
        let ids = ids_for(&locale);
        let missing: Vec<&String> = reference.difference(&ids).collect();
        assert!(
            missing.is_empty(),
            "{locale} is missing {} message ids, which will silently render in \
             {REFERENCE}: {missing:?}",
            missing.len()
        );
    }
}

#[test]
fn no_locale_declares_an_id_the_reference_does_not() {
    let reference = ids_for(REFERENCE);

    for locale in locales() {
        if locale == REFERENCE {
            continue;
        }
        let extra: Vec<String> = ids_for(&locale).difference(&reference).cloned().collect();
        // An id no other locale has is a typo or a leftover: nothing looks it
        // up, so it is dead weight that reads as coverage.
        assert!(
            extra.is_empty(),
            "{locale} declares {} message ids {REFERENCE} does not: {extra:?}",
            extra.len()
        );
    }
}

mod parsing {
    use super::message_ids;

    #[test]
    fn reads_top_level_ids_only() {
        let ids = message_ids(
            "### A comment heading\n\
             # A comment\n\
             greeting = Hello\n\
             button = Press\n\
             \x20   .label = Press me\n\
             -brand = Wayle\n\
             continued = one\n\
             \x20   two\n",
        );

        assert!(ids.contains("greeting"));
        assert!(ids.contains("button"));
        assert!(ids.contains("continued"));
        // The attribute belongs to `button`, the term is file-private, and the
        // continuation is part of `continued` — none is a message id.
        assert!(!ids.contains("label"));
        assert!(!ids.contains(".label"));
        assert!(!ids.contains("-brand"));
        assert!(!ids.contains("two"));
        assert_eq!(ids.len(), 3);
    }

    #[test]
    fn a_line_that_declares_nothing_yields_nothing() {
        assert!(message_ids("").is_empty());
        assert!(message_ids("just prose with no equals\n").is_empty());
        // `=` without the space Fluent requires is not a declaration.
        assert!(message_ids("greeting=Hello\n").is_empty());
    }
}
