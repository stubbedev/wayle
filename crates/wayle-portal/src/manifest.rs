//! Compile-time consistency guard for the portal manifest files.
//!
//! `wayle.portal` (what the frontend discovers), `wayle-portals.conf` (how it
//! routes), and the set of interfaces the backend actually mounts must agree.
//! These are three hand-edited lists that drift easily; the tests below pin
//! them together so a forgotten entry fails CI instead of silently dropping an
//! interface at runtime.

/// Interfaces the backend mounts in [`crate::run`]. Keep in sync with the
/// `server.at(...)` calls there; the tests assert it matches the resource files.
#[cfg(test)]
const MOUNTED_INTERFACES: &[&str] = &[
    "Settings",
    "Lockdown",
    "ScreenCast",
    "Screenshot",
    "RemoteDesktop",
    "GlobalShortcuts",
    "Notification",
    "Wallpaper",
    "Inhibit",
    "Background",
    "Usb",
    "Clipboard",
    "InputCapture",
    "FileChooser",
    "Email",
    "Access",
    "Account",
    "AppChooser",
    "DynamicLauncher",
    "Print",
];

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use super::MOUNTED_INTERFACES;

    const PORTAL_FILE: &str = include_str!("../../../resources/wayle.portal");
    const PORTALS_CONF: &str = include_str!("../../../resources/wayle-portals.conf");

    const PREFIX: &str = "org.freedesktop.impl.portal.";

    /// Short interface names declared in `wayle.portal`'s `Interfaces=` line.
    fn portal_declared() -> BTreeSet<String> {
        PORTAL_FILE
            .lines()
            .find_map(|line| line.strip_prefix("Interfaces="))
            .unwrap_or("")
            .split(';')
            .filter_map(|iface| iface.trim().strip_prefix(PREFIX))
            .map(str::to_owned)
            .collect()
    }

    /// Short interface names routed to `wayle` in `wayle-portals.conf`.
    fn conf_routed() -> BTreeSet<String> {
        PORTALS_CONF
            .lines()
            .filter_map(|line| line.strip_prefix(PREFIX))
            .filter_map(|rest| rest.split_once('=').map(|(iface, target)| (iface, target.trim())))
            .filter(|(_, target)| *target == "wayle")
            .map(|(iface, _)| iface.to_owned())
            .collect()
    }

    fn mounted() -> BTreeSet<String> {
        MOUNTED_INTERFACES.iter().map(|s| (*s).to_owned()).collect()
    }

    #[test]
    fn portal_file_matches_mounted() {
        assert_eq!(
            portal_declared(),
            mounted(),
            "wayle.portal Interfaces= must match the mounted interfaces"
        );
    }

    #[test]
    fn portals_conf_matches_mounted() {
        assert_eq!(
            conf_routed(),
            mounted(),
            "wayle-portals.conf wayle routes must match the mounted interfaces"
        );
    }

    #[test]
    fn conf_default_is_wayle() {
        assert!(
            PORTALS_CONF.lines().any(|l| l.trim() == "default=wayle"),
            "portals.conf must default to wayle (no gtk delegation)"
        );
        assert!(
            !PORTALS_CONF.contains("=gtk"),
            "portals.conf must not delegate any interface to gtk"
        );
    }
}
