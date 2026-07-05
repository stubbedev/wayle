//! `wayle-greeter apply-config`: writes user-chosen greeter settings into the
//! system `runtime.toml`, run as root via polkit/pkexec from wayle-settings.
//!
//! The greeter reads the *system* config (`/etc/wayle/config.toml`), which a
//! normal user cannot write. wayle-settings therefore stages the greeter keys a
//! user edited to a temp file and calls
//! `pkexec wayle-greeter apply-config <staged.toml>`. We copy only the
//! allowlisted `[greeter]` keys into `<config-dir>/runtime.toml` (never touching
//! the admin's hand-written `config.toml`); the greeter overlays that file on
//! next start. Unknown keys and non-greeter tables are dropped, so even invoked
//! with a hostile file this can only ever set greeter options.

use std::path::{Path, PathBuf};

use toml::{Table, Value};

/// Greeter keys wayle-settings is allowed to push to the system config. Matches
/// the `#[serde(rename)]`d field names in `GreeterConfig`.
const ALLOWED_KEYS: &[&str] = &[
    "background-mode",
    "background-image",
    "background-color",
    "show-clock",
    "clock-format",
    "date-format",
    "show-user-list",
    "show-power-buttons",
    "cursor-theme",
    "cursor-size",
];

/// Runs the `apply-config` subcommand from raw args (everything after the
/// `apply-config` token). Returns the process exit code.
///
/// Usage: `wayle-greeter apply-config [--config PATH] <STAGED.toml>`
pub fn run(args: &[String]) -> i32 {
    let mut config_path = PathBuf::from(super::config::DEFAULT_CONFIG);
    let mut staged: Option<PathBuf> = None;
    let mut iter = args.iter();
    while let Some(arg) = iter.next() {
        match arg.as_str() {
            "--config" => match iter.next() {
                Some(p) => config_path = PathBuf::from(p),
                None => return fail("--config requires a path"),
            },
            other => {
                if staged.is_some() {
                    return fail("unexpected extra argument");
                }
                staged = Some(PathBuf::from(other));
            }
        }
    }
    let Some(staged) = staged else {
        return fail("usage: wayle-greeter apply-config [--config PATH] <STAGED.toml>");
    };

    match apply(&staged, &config_path) {
        Ok(dest) => {
            println!("wrote {}", dest.display());
            0
        }
        Err(err) => fail(&err),
    }
}

/// Reads greeter keys from `staged`, merges them into `runtime.toml` beside
/// `config_path`, and writes it. Returns the written path.
fn apply(staged: &Path, config_path: &Path) -> Result<PathBuf, String> {
    let text = std::fs::read_to_string(staged)
        .map_err(|e| format!("cannot read {}: {e}", staged.display()))?;
    let table: Table = toml::from_str(&text).map_err(|e| format!("invalid TOML: {e}"))?;

    // Accept either a `[greeter]` table or the bare keys at top level.
    let source = match table.get("greeter") {
        Some(Value::Table(inner)) => inner.clone(),
        _ => table,
    };
    let mut greeter = Table::new();
    for (key, value) in source {
        if ALLOWED_KEYS.contains(&key.as_str()) {
            greeter.insert(key, value);
        }
    }
    if greeter.is_empty() {
        return Err("no recognised greeter keys in staged file".to_owned());
    }

    let dest = config_path.with_file_name("runtime.toml");
    let mut root: Table = match std::fs::read_to_string(&dest) {
        Ok(existing) => toml::from_str(&existing)
            .map_err(|e| format!("existing {} is invalid: {e}", dest.display()))?,
        Err(_) => Table::new(),
    };
    root.insert("greeter".to_owned(), Value::Table(greeter));

    let rendered = toml::to_string_pretty(&root).map_err(|e| format!("serialize failed: {e}"))?;
    if let Some(parent) = dest.parent() {
        std::fs::create_dir_all(parent)
            .map_err(|e| format!("cannot create {}: {e}", parent.display()))?;
    }
    std::fs::write(&dest, rendered).map_err(|e| format!("cannot write {}: {e}", dest.display()))?;
    Ok(dest)
}

fn fail(message: &str) -> i32 {
    eprintln!("wayle-greeter apply-config: {message}");
    1
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_only_allowlisted_greeter_keys() {
        let dir = std::env::temp_dir().join(format!("wg-apply-{}", std::process::id()));
        let _ = std::fs::create_dir_all(&dir);
        let staged = dir.join("staged.toml");
        std::fs::write(
            &staged,
            "[greeter]\nbackground-color = \"#123456\"\nevil = \"x\"\n\
             [general]\nfoo = 1\n",
        )
        .unwrap();
        let config = dir.join("config.toml");

        let dest = apply(&staged, &config).unwrap();
        let written: Table = toml::from_str(&std::fs::read_to_string(&dest).unwrap()).unwrap();
        let greeter = written.get("greeter").unwrap().as_table().unwrap();
        assert_eq!(
            greeter.get("background-color").unwrap().as_str(),
            Some("#123456")
        );
        assert!(greeter.get("evil").is_none());
        assert!(written.get("general").is_none());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn rejects_file_without_greeter_keys() {
        let dir = std::env::temp_dir().join(format!("wg-apply2-{}", std::process::id()));
        let _ = std::fs::create_dir_all(&dir);
        let staged = dir.join("s.toml");
        std::fs::write(&staged, "[general]\nfoo = 1\n").unwrap();
        assert!(apply(&staged, &dir.join("config.toml")).is_err());
        let _ = std::fs::remove_dir_all(&dir);
    }
}
