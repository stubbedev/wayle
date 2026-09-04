//! Thumbnails for row icons (rofi `-preview-cmd` and `thumbnail://`).
//!
//! rofi's contract, from `rofi-thumbnails(5)`:
//!
//! - a row asks for a thumbnail by prefixing its icon with `thumbnail://`,
//!   e.g. `echo -en "Name\0icon\x1fthumbnail:///path/to/file"`;
//! - the image is cached as the md5 of the file's URI, under
//!   `$XDG_CACHE_HOME/thumbnails/<size>/`, which is the freedesktop
//!   thumbnail spec — so a thumbnail a file manager already made is reused,
//!   and one made here shows up in the file manager;
//! - with no `-preview-cmd`, an XDG *thumbnailer* for the file's mimetype
//!   produces it: a `.thumbnailer` file naming `MimeType` and an `Exec`
//!   whose `%s`/`%u`/`%i`/`%o` are the size, the URI, the path and the
//!   output;
//! - with `-preview-cmd`, that command produces it instead, with `{input}`,
//!   `{output}` and `{size}` substituted;
//! - a file nothing can thumbnail keeps its mimetype icon.
//!
//! Only the spec's `normal` size (128px) is generated: a launcher row is
//! about 40px tall, so anything larger is downscaled work nobody sees.
// ponytail: one size; add the larger dirs if a preview *pane* ever wants them.

use std::{
    path::{Path, PathBuf},
    process::Stdio,
};

use tracing::{debug, warn};

use crate::template;

/// The freedesktop `normal` thumbnail size, in pixels.
pub const THUMBNAIL_SIZE: u32 = 128;

/// The icon prefix that asks for a thumbnail rather than naming an icon.
pub const THUMBNAIL_SCHEME: &str = "thumbnail://";

/// Where thumbnails live: `$XDG_CACHE_HOME/thumbnails/normal`.
///
/// Deliberately the shared spec directory rather than a wayle-private one —
/// the point of the md5-of-URI naming is that every thumbnailing application
/// hits the same file.
#[must_use]
pub fn cache_directory() -> PathBuf {
    let base = std::env::var_os("XDG_CACHE_HOME")
        .map(PathBuf::from)
        .filter(|path| path.is_absolute())
        .or_else(|| std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".cache")))
        .unwrap_or_else(std::env::temp_dir);
    base.join("thumbnails").join("normal")
}

/// The cache path a file's thumbnail would have, per the freedesktop spec:
/// the md5 of its `file://` URI, as PNG.
#[must_use]
pub fn cache_path(file: &Path) -> Option<PathBuf> {
    let uri = glib::filename_to_uri(file, None).ok()?;
    let digest = glib::compute_checksum_for_string(glib::ChecksumType::Md5, &uri)?;
    Some(cache_directory().join(format!("{digest}.png")))
}

/// An existing thumbnail for `file`, if one is already cached and not older
/// than the file itself.
///
/// The staleness check is what stops an edited image from keeping the
/// picture it used to have.
#[must_use]
pub fn cached(file: &Path) -> Option<PathBuf> {
    let thumbnail = cache_path(file)?;
    let made = std::fs::metadata(&thumbnail).ok()?.modified().ok()?;
    let changed = std::fs::metadata(file).ok()?.modified().ok()?;
    (made >= changed).then_some(thumbnail)
}

/// How a thumbnail gets made: rofi's `-preview-cmd`, or the system's XDG
/// thumbnailers.
#[derive(Debug, Clone, Default)]
pub struct Thumbnailer {
    /// `-preview-cmd`. When set it replaces the XDG thumbnailers entirely,
    /// which is what rofi does — the flag exists precisely to take over for
    /// entry names the system has no thumbnailer for.
    preview_cmd: Option<String>,
}

impl Thumbnailer {
    /// A thumbnailer using `preview_cmd` when given, else the system's.
    #[must_use]
    pub fn new(preview_cmd: Option<String>) -> Self {
        Self {
            preview_cmd: preview_cmd.filter(|command| !command.trim().is_empty()),
        }
    }

    /// The argv that would produce `file`'s thumbnail at `output`, or `None`
    /// when nothing here can make one.
    ///
    /// Separate from running it so the choice is testable without spawning
    /// anything.
    #[must_use]
    pub fn command(&self, file: &Path, output: &Path) -> Option<Vec<String>> {
        if let Some(preview) = &self.preview_cmd {
            return preview_argv(preview, file, output);
        }
        let content_type = content_type_of(file);
        let thumbnailer = find_thumbnailer(&content_type)?;
        thumbnailer.argv(file, output)
    }

    /// Produces `file`'s thumbnail, returning where it landed.
    ///
    /// A cached thumbnail is returned as it is; otherwise the command runs
    /// to completion, because the caller has nothing to show until it has.
    ///
    /// # Errors
    ///
    /// Never returns an error: a file nothing can thumbnail is a normal
    /// outcome, not a failure, and the caller falls back to the mimetype
    /// icon. Failures are logged.
    pub async fn generate(&self, file: &Path) -> Option<PathBuf> {
        if let Some(existing) = cached(file) {
            return Some(existing);
        }
        let output = cache_path(file)?;
        let argv = self.command(file, &output)?;

        if let Some(parent) = output.parent()
            && let Err(error) = tokio::fs::create_dir_all(parent).await
        {
            warn!(%error, "cannot create the thumbnail cache directory");
            return None;
        }

        run(&argv, &output).await
    }
}

/// Runs a thumbnailer and reports whether it actually wrote `output`.
///
/// Exit status alone is not enough: a thumbnailer that succeeds without
/// producing a file leaves the row with nothing to load.
async fn run(argv: &[String], output: &Path) -> Option<PathBuf> {
    let (program, args) = argv.split_first()?;
    let status = tokio::process::Command::new(program)
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .await;
    match status {
        Ok(status) if status.success() && output.is_file() => Some(output.to_path_buf()),
        Ok(status) => {
            debug!(?argv, ?status, "thumbnailer produced nothing");
            None
        }
        Err(error) => {
            debug!(?argv, %error, "thumbnailer could not be run");
            None
        }
    }
}

/// `-preview-cmd` with its placeholders filled in.
fn preview_argv(command: &str, file: &Path, output: &Path) -> Option<Vec<String>> {
    // Shell-split before substituting, then exec'd — never handed to a
    // shell. A file name is attacker-controlled in every mode that lists a
    // directory, and one holding a space or a quote must still be one
    // argument rather than several or none.
    let argv = template::render_argv(command, |key| match key {
        "input" => Some(file.display().to_string()),
        "output" => Some(output.display().to_string()),
        "size" => Some(THUMBNAIL_SIZE.to_string()),
        _ => None,
    });
    if argv.is_empty() {
        warn!(%command, "-preview-cmd does not parse as a command");
        return None;
    }
    Some(argv)
}

/// One installed `.thumbnailer` descriptor.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct XdgThumbnailer {
    /// `Exec`, with its `%s`/`%u`/`%i`/`%o` still in place.
    pub exec: String,
    /// The mimetypes it claims.
    pub mime_types: Vec<String>,
}

impl XdgThumbnailer {
    /// The argv for `file` → `output`.
    #[must_use]
    pub fn argv(&self, file: &Path, output: &Path) -> Option<Vec<String>> {
        let uri = glib::filename_to_uri(file, None)
            .map(|uri| uri.to_string())
            .unwrap_or_else(|_| file.display().to_string());
        let argv = shlex::split(&self.exec)?;
        if argv.is_empty() {
            return None;
        }
        Some(
            argv.into_iter()
                .map(|part| {
                    part.replace("%s", &THUMBNAIL_SIZE.to_string())
                        .replace("%u", &uri)
                        .replace("%i", &file.display().to_string())
                        .replace("%o", &output.display().to_string())
                })
                .collect(),
        )
    }
}

/// Reads `Exec` and `MimeType` out of a `.thumbnailer` file.
///
/// The format is a desktop file with a `[Thumbnailer Entry]` section. A
/// `TryExec` that is not on `$PATH` disqualifies the entry, which is how a
/// descriptor left behind by an uninstalled package stops being chosen.
#[must_use]
pub fn parse_thumbnailer(contents: &str) -> Option<XdgThumbnailer> {
    let mut in_section = false;
    let mut exec = None;
    let mut try_exec = None;
    let mut mime_types = Vec::new();

    for line in contents.lines() {
        let line = line.trim();
        if line.starts_with('[') {
            in_section = line.eq_ignore_ascii_case("[Thumbnailer Entry]");
            continue;
        }
        if !in_section || line.starts_with('#') {
            continue;
        }
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        match key.trim() {
            "Exec" => exec = Some(value.trim().to_owned()),
            "TryExec" => try_exec = Some(value.trim().to_owned()),
            "MimeType" => {
                mime_types = value
                    .split(';')
                    .map(str::trim)
                    .filter(|mime| !mime.is_empty())
                    .map(ToOwned::to_owned)
                    .collect();
            }
            _ => {}
        }
    }

    if let Some(program) = try_exec.filter(|program| !program.is_empty())
        && !executable_exists(&program)
    {
        return None;
    }

    let exec = exec.filter(|exec| !exec.is_empty())?;
    (!mime_types.is_empty()).then_some(XdgThumbnailer { exec, mime_types })
}

fn executable_exists(program: &str) -> bool {
    let path = Path::new(program);
    if path.is_absolute() {
        return path.is_file();
    }
    std::env::var_os("PATH").is_some_and(|paths| {
        std::env::split_paths(&paths).any(|directory| directory.join(program).is_file())
    })
}

/// Where the spec says thumbnailer descriptors live.
fn thumbnailer_directories() -> Vec<PathBuf> {
    let mut directories = Vec::new();
    if let Some(home) = std::env::var_os("XDG_DATA_HOME").map(PathBuf::from) {
        directories.push(home.join("thumbnailers"));
    } else if let Some(home) = std::env::var_os("HOME").map(PathBuf::from) {
        directories.push(home.join(".local/share/thumbnailers"));
    }
    let data_dirs = std::env::var("XDG_DATA_DIRS")
        .unwrap_or_else(|_| String::from("/usr/local/share:/usr/share"));
    for directory in data_dirs.split(':').filter(|part| !part.is_empty()) {
        directories.push(Path::new(directory).join("thumbnailers"));
    }
    directories
}

/// The first installed thumbnailer claiming `content_type`.
fn find_thumbnailer(content_type: &str) -> Option<XdgThumbnailer> {
    for directory in thumbnailer_directories() {
        let Ok(entries) = std::fs::read_dir(&directory) else {
            continue;
        };
        for entry in entries.filter_map(Result::ok) {
            let path = entry.path();
            if path.extension().is_none_or(|ext| ext != "thumbnailer") {
                continue;
            }
            let Ok(contents) = std::fs::read_to_string(&path) else {
                continue;
            };
            if let Some(thumbnailer) = parse_thumbnailer(&contents)
                && thumbnailer
                    .mime_types
                    .iter()
                    .any(|mime| mime == content_type)
            {
                return Some(thumbnailer);
            }
        }
    }
    None
}

/// The file's mimetype, guessed from its name and content.
fn content_type_of(file: &Path) -> String {
    let (content_type, _uncertain) = gio::functions::content_type_guess(Some(file), None);
    content_type.to_string()
}

/// The icon-theme name for a file with no thumbnail — the fallback rofi
/// falls back to.
#[must_use]
pub fn mime_icon(file: &Path) -> String {
    let content_type = content_type_of(file);
    gio::functions::content_type_get_generic_icon_name(&content_type).map_or_else(
        || String::from("text-x-generic-symbolic"),
        |name| name.to_string(),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_cache_path_is_the_md5_of_the_file_uri() {
        // The value the spec fixes, so a thumbnail a file manager already
        // made is the one that gets loaded.
        let path = cache_path(Path::new("/home/user/Photos/me.png")).unwrap();
        let uri = "file:///home/user/Photos/me.png";
        let digest = glib::compute_checksum_for_string(glib::ChecksumType::Md5, uri).unwrap();
        assert_eq!(
            path.file_name().unwrap().to_string_lossy(),
            format!("{digest}.png")
        );
        assert!(
            path.parent().unwrap().ends_with("thumbnails/normal"),
            "{path:?}"
        );
    }

    #[test]
    fn preview_cmd_placeholders_are_filled_and_the_argv_is_not_a_shell_line() {
        let thumbnailer = Thumbnailer::new(Some(String::from(
            "mkthumb \"{input}\" \"{output}\" \"{size}\"",
        )));
        let argv = thumbnailer
            .command(Path::new("/tmp/a b.raw"), Path::new("/cache/x.png"))
            .unwrap();
        assert_eq!(argv, ["mkthumb", "/tmp/a b.raw", "/cache/x.png", "128"]);
    }

    #[test]
    fn a_file_name_cannot_smuggle_a_command_into_preview_cmd() {
        // Directory listings are attacker-controlled in every browsing mode.
        let thumbnailer = Thumbnailer::new(Some(String::from("mkthumb '{input}' '{output}'")));
        let argv = thumbnailer
            .command(
                Path::new("/tmp/x; rm -rf ~/.png"),
                Path::new("/cache/x.png"),
            )
            .unwrap();
        assert_eq!(
            argv,
            ["mkthumb", "/tmp/x; rm -rf ~/.png", "/cache/x.png"],
            "the whole name is one argument"
        );
    }

    #[test]
    fn a_broken_preview_cmd_produces_nothing_rather_than_half_a_command() {
        let thumbnailer = Thumbnailer::new(Some(String::from("mkthumb '{input}")));
        assert!(
            thumbnailer
                .command(Path::new("/tmp/a"), Path::new("/cache/x.png"))
                .is_none()
        );
        // A blank command is no command at all, and must not shadow the
        // system thumbnailers by being "set".
        assert!(
            Thumbnailer::new(Some(String::from("   ")))
                .preview_cmd
                .is_none()
        );
        assert!(Thumbnailer::new(None).preview_cmd.is_none());
    }

    #[test]
    fn a_thumbnailer_descriptor_yields_its_exec_and_mimetypes() {
        let parsed = parse_thumbnailer(
            "[Thumbnailer Entry]\n\
             TryExec=/bin/sh\n\
             Exec=gdk-pixbuf-thumbnailer -s %s %u %o\n\
             MimeType=image/svg+xml;image/png;\n",
        )
        .expect("a complete descriptor parses");
        assert_eq!(parsed.mime_types, ["image/svg+xml", "image/png"]);

        let argv = parsed
            .argv(Path::new("/tmp/logo.svg"), Path::new("/cache/x.png"))
            .unwrap();
        assert_eq!(
            argv,
            [
                "gdk-pixbuf-thumbnailer",
                "-s",
                "128",
                "file:///tmp/logo.svg",
                "/cache/x.png"
            ]
        );
    }

    #[test]
    fn a_descriptor_missing_what_it_needs_is_not_used() {
        // No Exec: nothing to run.
        assert!(parse_thumbnailer("[Thumbnailer Entry]\nMimeType=image/png;\n").is_none());
        // No MimeType: nothing it claims, so it would never be chosen anyway
        // and must not be returned as a catch-all.
        assert!(parse_thumbnailer("[Thumbnailer Entry]\nExec=thumb %i %o\n").is_none());
        // Keys outside the section are not the section's.
        assert!(
            parse_thumbnailer("Exec=thumb %i %o\nMimeType=image/png;\n").is_none(),
            "a key before [Thumbnailer Entry] does not belong to it"
        );
        // A TryExec that is not installed disqualifies the entry.
        assert!(
            parse_thumbnailer(
                "[Thumbnailer Entry]\n\
                 TryExec=/nonexistent/thumbnailer\n\
                 Exec=thumb %i %o\n\
                 MimeType=image/png;\n"
            )
            .is_none()
        );
    }

    #[test]
    fn a_file_with_no_thumbnail_still_has_an_icon_name() {
        let icon = mime_icon(Path::new("/tmp/notes.txt"));
        assert!(!icon.is_empty());
    }
}
