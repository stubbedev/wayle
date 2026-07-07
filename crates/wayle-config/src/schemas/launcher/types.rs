use wayle_derive::wayle_enum;

/// Launcher surface position (rofi `-location` 0-8 grid).
#[wayle_enum(default)]
pub enum LauncherLocation {
    /// Centered (rofi location 0).
    #[default]
    Center,
    /// Top-left (1).
    NorthWest,
    /// Top edge (2).
    North,
    /// Top-right (3).
    NorthEast,
    /// Right edge (4).
    East,
    /// Bottom-right (5).
    SouthEast,
    /// Bottom edge (6).
    South,
    /// Bottom-left (7).
    SouthWest,
    /// Left edge (8).
    West,
}

/// Matching method (rofi `-matching`).
#[wayle_enum(default)]
pub enum LauncherMatching {
    /// Tokenized substring matching.
    #[default]
    Normal,
    /// Regular expression.
    Regex,
    /// Glob patterns per token.
    Glob,
    /// fzf-style fuzzy matching.
    Fuzzy,
    /// Tokenized prefix matching.
    Prefix,
}

/// Result sorting method (rofi `-sorting-method`).
#[wayle_enum(default)]
pub enum LauncherSorting {
    /// Levenshtein distance to the query (rofi "normal").
    #[default]
    Levenshtein,
    /// fzf match-quality score.
    Fzf,
}

/// Case handling (collapses rofi `-case-sensitive`/`-case-smart`).
#[wayle_enum(default)]
pub enum LauncherCase {
    /// Always case-insensitive.
    #[default]
    Insensitive,
    /// Sensitive only when the query contains an uppercase char.
    Smart,
    /// Always case-sensitive.
    Sensitive,
}

/// Desktop-entry fields searched by drun (rofi `-drun-match-fields`).
#[wayle_enum]
pub enum LauncherDrunField {
    /// Localized Name.
    Name,
    /// GenericName.
    Generic,
    /// Exec command line.
    Exec,
    /// Categories list.
    Categories,
    /// Comment.
    Comment,
    /// Keywords list.
    Keywords,
}

/// Window fields searched by window mode (rofi `-window-match-fields`).
#[wayle_enum]
pub enum LauncherWindowField {
    /// Window title.
    Title,
    /// Application class/app-id.
    Class,
    /// Window name.
    Name,
    /// Window role.
    Role,
    /// Workspace/desktop name.
    Desktop,
}

/// File sorting in the file browser (rofi filebrowser `sorting-method`).
#[wayle_enum(default)]
pub enum LauncherFileSort {
    /// By file name.
    #[default]
    Name,
    /// By modification time.
    Mtime,
    /// By access time.
    Atime,
    /// By creation time.
    Ctime,
}
