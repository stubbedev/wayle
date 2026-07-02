//! Login-user discovery for the greeter's user list.
//!
//! Real (human) accounts are read from `/etc/passwd`: uid in the regular-user
//! range with a real login shell. Each user gets a best-effort avatar from
//! AccountsService (`/var/lib/AccountsService/icons/<name>`, what gdm/sddm
//! use) or `~/.face`, when the greeter user can read them.

use std::path::{Path, PathBuf};

/// Regular-user uid range offered in the user list. Below 1000 = system
/// accounts; 65534 = nobody. 60000+ covers other reserved ranges.
const UID_RANGE: std::ops::RangeInclusive<u32> = 1000..=59999;

/// AccountsService avatar directory (world-readable on typical setups).
const ACCOUNTS_SERVICE_ICONS: &str = "/var/lib/AccountsService/icons";

/// A login account offered in the greeter's user list.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct User {
    /// Unix login name (what greetd authenticates).
    pub name: String,
    /// Display name from the GECOS full-name field (falls back to `name`).
    pub display_name: String,
    /// Readable avatar image, if one was found.
    pub icon: Option<PathBuf>,
}

/// Discovers login users from `/etc/passwd`, with avatars resolved.
#[must_use]
pub fn load() -> Vec<User> {
    let passwd = std::fs::read_to_string("/etc/passwd").unwrap_or_default();
    parse_passwd(&passwd)
        .into_iter()
        .map(|(mut user, home)| {
            user.icon = find_avatar(&user.name, &home);
            user
        })
        .collect()
}

/// Parses passwd `text` into `(user, home_dir)` pairs for the offerable
/// accounts, sorted by display name. The home dir is returned separately so
/// [`load`] can resolve `~/.face` without stashing it in the [`User`].
fn parse_passwd(text: &str) -> Vec<(User, PathBuf)> {
    let mut users: Vec<(User, PathBuf)> = text
        .lines()
        .filter_map(|line| {
            let fields: Vec<&str> = line.split(':').collect();
            let [name, _, uid, _, gecos, home, shell] = fields[..] else {
                return None;
            };
            let uid: u32 = uid.parse().ok()?;
            if !UID_RANGE.contains(&uid) || !is_login_shell(shell) {
                return None;
            }
            let full_name = gecos.split(',').next().unwrap_or("").trim();
            let user = User {
                name: name.to_owned(),
                display_name: if full_name.is_empty() {
                    name.to_owned()
                } else {
                    full_name.to_owned()
                },
                icon: None,
            };
            Some((user, PathBuf::from(home)))
        })
        .collect();
    users.sort_by_key(|(u, _)| u.display_name.to_lowercase());
    users
}

/// Whether `shell` allows an interactive login.
fn is_login_shell(shell: &str) -> bool {
    !shell.is_empty()
        && !shell.ends_with("nologin")
        && !shell.ends_with("/false")
        && shell != "/bin/sync"
}

/// Best-effort avatar lookup: AccountsService icon, then `~/.face`. Only paths
/// the greeter user can actually read are returned (GTK would render a broken
/// image otherwise).
fn find_avatar(name: &str, home: &Path) -> Option<PathBuf> {
    let accounts_icon = Path::new(ACCOUNTS_SERVICE_ICONS).join(name);
    [accounts_icon, home.join(".face")]
        .into_iter()
        .find(|candidate| std::fs::File::open(candidate).is_ok())
}

#[cfg(test)]
mod tests {
    use super::*;

    const PASSWD: &str = "\
root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
alice:x:1000:1000:Alice Wonder,,,:/home/alice:/bin/zsh
bob:x:1001:1001::/home/bob:/bin/bash
svc:x:998:998:service:/var/lib/svc:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
carol:x:1002:1002:Carol:/home/carol:/bin/false
";

    #[test]
    fn keeps_regular_users_with_login_shells() {
        let users = parse_passwd(PASSWD);
        let names: Vec<&str> = users.iter().map(|(u, _)| u.name.as_str()).collect();
        assert_eq!(names, vec!["alice", "bob"]);
    }

    #[test]
    fn display_name_prefers_gecos_full_name() {
        let users = parse_passwd(PASSWD);
        assert_eq!(users[0].0.display_name, "Alice Wonder");
        assert_eq!(users[1].0.display_name, "bob");
    }

    #[test]
    fn home_is_returned_for_avatar_resolution() {
        let users = parse_passwd(PASSWD);
        assert_eq!(users[0].1, PathBuf::from("/home/alice"));
    }

    #[test]
    fn malformed_lines_are_skipped() {
        assert!(parse_passwd("garbage\nno:fields\n").is_empty());
    }
}
