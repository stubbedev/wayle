//! Every icon name the code asks for has to exist in `resources/icons`.
//!
//! A missing icon is not a build error and not a runtime error: GTK asks the
//! theme, gets nothing, and draws the broken-image placeholder or nothing at
//! all. The clipboard launcher mode shipped three names that did not exist
//! (`ld-type`, `ld-file`, `ld-file-question`) and everything compiled and
//! passed.
//!
//! Doc comments are skipped on purpose: the config schemas document example
//! icon names (`ld-gpu-symbolic`, `ld-youtube-symbolic`) that are meant to
//! come from the user's own icon theme, not from wayle's bundled set. So is
//! `tests/`: the fixtures below spell out icon names that exist to be scanned,
//! not to be looked up in the bundle.

use std::{
    collections::BTreeSet,
    fs,
    path::{Path, PathBuf},
};

/// Where the bundled symbolic icons live.
const ICON_DIR: &str = "../resources/icons/hicolor/scalable/actions";

/// The trees whose Rust sources are scanned. `tests` is left out on purpose;
/// see the module docs.
const SOURCE_DIRS: [&str; 2] = ["src", "../crates"];

/// The icon-set prefixes wayle bundles.
const PREFIXES: [&str; 6] = ["ld", "tb", "tbf", "md", "cm", "si"];

/// Names the code asks for that are deliberately not bundled.
const IGNORED: [&str; 1] = [
    // The dashboard's distro logo falls back to this behind `icon_exists`, so
    // a distro with no bundled logo draws nothing instead of a broken image.
    "cm-wayle-symbolic",
];

/// The icon-name literals on one line of Rust, or nothing for a comment.
///
/// Matches `"<prefix>-<name>-symbolic"` for the icon sets wayle bundles.
fn icon_names_in(line: &str) -> Vec<String> {
    let trimmed = line.trim_start();
    if trimmed.starts_with("//") || trimmed.starts_with("#[doc") {
        return Vec::new();
    }

    let mut names = Vec::new();
    let mut rest = line;

    while let Some(open) = rest.find('"') {
        let after = &rest[open + 1..];
        let Some(close) = after.find('"') else {
            break;
        };
        let literal = &after[..close];
        if is_icon_name(literal) {
            names.push(literal.to_owned());
        }
        rest = &after[close + 1..];
    }

    names
}

/// Whether a string literal names one of the bundled icons.
fn is_icon_name(literal: &str) -> bool {
    let Some(prefix) = literal.split('-').next() else {
        return false;
    };
    PREFIXES.contains(&prefix)
        && literal.ends_with("-symbolic")
        && literal.chars().all(|character| {
            character.is_ascii_lowercase()
                || character.is_ascii_digit()
                || character == '-'
                || character == '_'
        })
}

fn manifest_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
}

/// Every icon name referenced from real code, with the file it came from.
fn referenced() -> Vec<(String, PathBuf)> {
    fn walk(dir: &Path, into: &mut Vec<(String, PathBuf)>) {
        let Ok(entries) = fs::read_dir(dir) else {
            return;
        };
        for entry in entries.filter_map(Result::ok) {
            let path = entry.path();
            if path.is_dir() {
                if path.file_name().is_some_and(|name| name == "target") {
                    continue;
                }
                walk(&path, into);
            } else if path.extension().is_some_and(|extension| extension == "rs") {
                let Ok(source) = fs::read_to_string(&path) else {
                    continue;
                };
                for line in source.lines() {
                    for name in icon_names_in(line) {
                        into.push((name, path.clone()));
                    }
                }
            }
        }
    }

    let mut found = Vec::new();
    for dir in SOURCE_DIRS {
        walk(&manifest_dir().join(dir), &mut found);
    }
    found
}

/// Every icon the repository ships.
fn bundled() -> BTreeSet<String> {
    let Ok(entries) = fs::read_dir(manifest_dir().join(ICON_DIR)) else {
        return BTreeSet::new();
    };

    entries
        .filter_map(Result::ok)
        .filter_map(|entry| {
            entry
                .path()
                .file_stem()
                .and_then(|stem| stem.to_str())
                .map(str::to_owned)
        })
        .collect()
}

#[test]
fn every_referenced_icon_is_bundled() -> Result<(), String> {
    let bundled = bundled();
    assert!(
        !bundled.is_empty(),
        "no bundled icons found; the test is reading the wrong place"
    );

    let referenced = referenced();
    assert!(
        !referenced.is_empty(),
        "no icon references found; the scan is reading the wrong place"
    );

    let mut missing: Vec<String> = referenced
        .iter()
        .filter(|(name, _)| !bundled.contains(name) && !IGNORED.contains(&name.as_str()))
        .map(|(name, path)| {
            let shown = path
                .strip_prefix(manifest_dir())
                .unwrap_or(path)
                .display()
                .to_string();
            format!("{name} ({shown})")
        })
        .collect();
    missing.sort();
    missing.dedup();

    if missing.is_empty() {
        return Ok(());
    }
    Err(format!(
        "{} icon name(s) have no SVG in resources/icons and will not render: {missing:#?}",
        missing.len()
    ))
}

/// The fixtures in this file name icons on purpose; scanning them would report
/// every one of them as missing.
#[test]
fn test_sources_are_not_scanned() {
    let referenced = referenced();
    assert!(!referenced.is_empty(), "the scan found nothing at all");

    let from_tests: Vec<_> = referenced
        .iter()
        .filter(|(_, path)| path.components().any(|part| part.as_os_str() == "tests"))
        .collect();
    assert!(
        from_tests.is_empty(),
        "icon names picked up from test sources: {from_tests:#?}"
    );
}

mod scanning {
    use super::{IGNORED, icon_names_in, is_icon_name};

    #[test]
    fn finds_icon_literals_in_real_code() {
        assert_eq!(
            icon_names_in(r#"    set_icon_name: Some("ld-arrow-left-symbolic"),"#),
            ["ld-arrow-left-symbolic"]
        );
        // More than one on a line, and the other icon sets wayle bundles.
        assert_eq!(
            icon_names_in(r#"a("tb-refresh-symbolic"); b("si-dropbox-symbolic");"#),
            ["tb-refresh-symbolic", "si-dropbox-symbolic"]
        );
    }

    #[test]
    fn skips_comments_and_non_icon_strings() {
        // Config schemas document theme icons wayle does not bundle.
        assert!(icon_names_in(r#"/// icon = "ld-gpu-symbolic""#).is_empty());
        assert!(icon_names_in(r#"// "ld-youtube-symbolic""#).is_empty());
        assert!(icon_names_in(r#"#[doc = "ld-youtube-symbolic"]"#).is_empty());
        // An ordinary string is not an icon name.
        assert!(icon_names_in(r#"let name = "hello";"#).is_empty());
        assert!(icon_names_in("no strings here").is_empty());
    }

    #[test]
    fn only_the_bundled_prefixes_and_shapes_count() {
        assert!(is_icon_name("ld-image-symbolic"));
        assert!(is_icon_name("ld-grid-2x2-symbolic"));
        // Every set wayle bundles, including the Material names, which are the
        // only ones with underscores.
        assert!(is_icon_name("tbf-circle-symbolic"));
        assert!(is_icon_name("cm-wireless-symbolic"));
        assert!(is_icon_name("si-dropbox-symbolic"));
        assert!(is_icon_name("md-battery_android_frame_1-symbolic"));
        // Wrong prefix, wrong suffix, and a format string are all not names
        // this test can check against the bundle.
        assert!(!is_icon_name("gtk-image-symbolic"));
        assert!(!is_icon_name("ld-image"));
        assert!(!is_icon_name("ld-{kind}-symbolic"));
        assert!(!is_icon_name(""));
    }

    /// An ignored name still scans as an icon name; it is excused at the
    /// bundle check, not at the scan, so a typo in it is not silently ignored.
    #[test]
    fn ignored_names_are_still_icon_names() {
        for name in IGNORED {
            assert!(is_icon_name(name), "{name} does not scan as an icon name");
        }
    }
}
