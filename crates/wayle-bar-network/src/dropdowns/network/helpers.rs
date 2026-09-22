use std::{
    cell::Cell,
    cmp::Reverse,
    collections::{HashMap, HashSet},
    rc::Rc,
    sync::Arc,
};

use relm4::gtk;
use wayle_network::core::access_point::{AccessPoint, SecurityType};
use zbus::zvariant::OwnedObjectPath;

/// The reveal toggle's two states.
pub const ICON_EYE: &str = "ld-eye-symbolic";
pub const ICON_EYE_OFF: &str = "ld-eye-off-symbolic";

/// Snapshot of an access point for display in the network list.
#[derive(Debug, Clone)]
pub struct AccessPointSnapshot {
    pub ssid: String,
    pub strength: u8,
    pub security: SecurityType,
    pub object_path: OwnedObjectPath,
    pub known: bool,
    /// Seen by an earlier scan but no longer reported by NetworkManager, which
    /// prunes access points it has not heard from in a while. Its object path
    /// is dead: connecting to it means scanning for it again first.
    pub stale: bool,
}

pub fn signal_strength_icon(strength: u8) -> &'static str {
    match strength {
        0..=19 => "cm-wireless-signal-none-symbolic",
        20..=39 => "cm-wireless-signal-weak-symbolic",
        40..=59 => "cm-wireless-signal-ok-symbolic",
        60..=79 => "cm-wireless-signal-good-symbolic",
        _ => "cm-wireless-signal-excellent-symbolic",
    }
}

pub fn frequency_to_band(freq_mhz: u32) -> Option<&'static str> {
    match freq_mhz {
        2400..=2500 => Some("2.4 GHz"),
        5000..=5900 => Some("5 GHz"),
        5901..=7125 => Some("6 GHz"),
        57000..=71000 => Some("60 GHz"),
        _ => None,
    }
}

pub fn format_wired_speed(speed_mbps: u32) -> String {
    if speed_mbps >= 1000 {
        let gbps = speed_mbps as f64 / 1000.0;
        if speed_mbps.is_multiple_of(1000) {
            format!("{} Gbps", speed_mbps / 1000)
        } else {
            format!("{gbps:.1} Gbps")
        }
    } else {
        format!("{speed_mbps} Mbps")
    }
}

pub fn requires_password(security: SecurityType) -> bool {
    !matches!(security, SecurityType::None | SecurityType::Enterprise)
}

/// Deduplicates access points by SSID (keeping strongest signal),
/// filters out hidden networks and the currently connected SSID,
/// and sorts by signal strength descending.
pub fn sorted_unique_access_points(
    access_points: &[Arc<AccessPoint>],
    connected_ssid: Option<&str>,
    known_ssids: &HashSet<String>,
) -> Vec<AccessPointSnapshot> {
    let mut best_by_ssid: HashMap<String, AccessPointSnapshot> = HashMap::new();

    for ap in access_points {
        let ssid = ap.ssid.get();
        if ssid.is_empty() {
            continue;
        }

        let security = ap.security.get();
        if security == SecurityType::Enterprise {
            continue;
        }

        let ssid_str = ssid.to_string_lossy();
        if connected_ssid.is_some_and(|connected| connected == ssid_str) {
            continue;
        }

        let strength = ap.strength.get();
        let should_replace = best_by_ssid
            .get(&ssid_str)
            .is_none_or(|existing| strength > existing.strength);

        if should_replace {
            best_by_ssid.insert(
                ssid_str.clone(),
                AccessPointSnapshot {
                    ssid: ssid_str.clone(),
                    strength,
                    security,
                    object_path: ap.object_path().clone(),
                    known: known_ssids.contains(&ssid_str),
                    stale: false,
                },
            );
        }
    }

    let mut snapshots: Vec<AccessPointSnapshot> = best_by_ssid.into_values().collect();
    snapshots.sort_by_key(|snapshot| Reverse(snapshot.strength));
    snapshots
}

/// Folds the live access points over the ones already listed, so a network
/// NetworkManager has pruned stays in the list instead of disappearing.
///
/// Live entries always win and come first, strongest first; the remembered
/// rest follow, marked stale and in their previous order. `connected_ssid`
/// is dropped from both halves, the same as the live list does, and each
/// entry's `known` is recomputed so a network forgotten or saved since it was
/// last seen reads correctly.
pub fn merge_with_cache(
    live: Vec<AccessPointSnapshot>,
    cached: &[AccessPointSnapshot],
    connected_ssid: Option<&str>,
    known_ssids: &HashSet<String>,
) -> Vec<AccessPointSnapshot> {
    let live_ssids: HashSet<&str> = live.iter().map(|ap| ap.ssid.as_str()).collect();

    let remembered: Vec<AccessPointSnapshot> = cached
        .iter()
        .filter(|ap| !live_ssids.contains(ap.ssid.as_str()))
        .filter(|ap| connected_ssid != Some(ap.ssid.as_str()))
        .map(|ap| AccessPointSnapshot {
            known: known_ssids.contains(&ap.ssid),
            stale: true,
            ..ap.clone()
        })
        .collect();

    live.into_iter().chain(remembered).collect()
}

/// Turns a masked entry into one the user can peek at, via an eye icon in the
/// entry itself.
///
/// A mistyped WireGuard private key or VPN password is otherwise unrecoverable
/// except by clearing the box and retyping it, and every form in this dropdown
/// that asks for a secret gets the same affordance — the wifi password box
/// three rows up already had it.
pub fn attach_reveal_toggle(entry: &gtk::Entry) {
    use gtk::prelude::*;

    entry.set_icon_from_icon_name(gtk::EntryIconPosition::Secondary, Some(ICON_EYE_OFF));
    entry.set_icon_activatable(gtk::EntryIconPosition::Secondary, true);
    entry.set_icon_sensitive(gtk::EntryIconPosition::Secondary, true);

    let revealed = Rc::new(Cell::new(false));
    let target = entry.clone();
    entry.connect_icon_press(move |_entry, position| {
        if position != gtk::EntryIconPosition::Secondary {
            return;
        }
        let now_revealed = !revealed.get();
        revealed.set(now_revealed);
        target.set_visibility(now_revealed);
        target.set_icon_from_icon_name(
            gtk::EntryIconPosition::Secondary,
            Some(if now_revealed { ICON_EYE } else { ICON_EYE_OFF }),
        );
    });
}

/// Re-masks an entry and puts its reveal toggle back to hidden.
pub fn reset_reveal_toggle(entry: &gtk::Entry) {
    use gtk::prelude::*;

    entry.set_visibility(false);
    entry.set_icon_from_icon_name(gtk::EntryIconPosition::Secondary, Some(ICON_EYE_OFF));
}

#[cfg(test)]
mod tests {
    use super::*;

    fn snapshot(ssid: &str, strength: u8) -> AccessPointSnapshot {
        AccessPointSnapshot {
            ssid: String::from(ssid),
            strength,
            security: SecurityType::Wpa2,
            object_path: OwnedObjectPath::try_from(format!(
                "/org/freedesktop/NetworkManager/AccessPoint/{strength}"
            ))
            .expect("valid object path"),
            known: false,
            stale: false,
        }
    }

    fn ssids(list: &[AccessPointSnapshot]) -> Vec<(&str, bool)> {
        list.iter().map(|ap| (ap.ssid.as_str(), ap.stale)).collect()
    }

    #[test]
    fn a_network_nm_pruned_stays_listed_as_stale() {
        let cached = [snapshot("home", 80), snapshot("cafe", 40)];
        let merged = merge_with_cache(vec![snapshot("home", 75)], &cached, None, &HashSet::new());
        assert_eq!(ssids(&merged), [("home", false), ("cafe", true)]);
    }

    #[test]
    fn a_live_network_is_never_marked_stale_or_duplicated() {
        let cached = [snapshot("home", 80)];
        let merged = merge_with_cache(vec![snapshot("home", 30)], &cached, None, &HashSet::new());
        assert_eq!(ssids(&merged), [("home", false)]);
        // The live reading wins over the remembered one.
        assert_eq!(merged[0].strength, 30);
    }

    #[test]
    fn live_networks_come_before_remembered_ones() {
        let cached = [snapshot("strong-but-gone", 99)];
        let merged = merge_with_cache(vec![snapshot("weak", 10)], &cached, None, &HashSet::new());
        assert_eq!(ssids(&merged), [("weak", false), ("strong-but-gone", true)]);
    }

    #[test]
    fn the_connected_network_is_not_resurrected_from_the_cache() {
        let cached = [snapshot("home", 80), snapshot("cafe", 40)];
        let merged = merge_with_cache(vec![], &cached, Some("home"), &HashSet::new());
        assert_eq!(ssids(&merged), [("cafe", true)]);
    }

    #[test]
    fn a_remembered_network_picks_up_saved_state_changes() {
        let mut saved = snapshot("cafe", 40);
        saved.known = true;
        let known: HashSet<String> = HashSet::from([String::from("home")]);
        let merged = merge_with_cache(vec![], &[saved, snapshot("home", 80)], None, &known);
        // Forgotten since it was last seen, and saved since it was last seen.
        assert!(!merged[0].known);
        assert!(merged[1].known);
    }

    #[test]
    fn an_empty_cache_leaves_the_live_list_untouched() {
        let merged = merge_with_cache(vec![snapshot("home", 80)], &[], None, &HashSet::new());
        assert_eq!(ssids(&merged), [("home", false)]);
    }

    #[test]
    fn frequency_2ghz_band() {
        assert_eq!(frequency_to_band(2412), Some("2.4 GHz"));
        assert_eq!(frequency_to_band(2437), Some("2.4 GHz"));
        assert_eq!(frequency_to_band(2484), Some("2.4 GHz"));
    }

    #[test]
    fn frequency_5ghz_band() {
        assert_eq!(frequency_to_band(5180), Some("5 GHz"));
        assert_eq!(frequency_to_band(5745), Some("5 GHz"));
        assert_eq!(frequency_to_band(5825), Some("5 GHz"));
    }

    #[test]
    fn frequency_6ghz_band() {
        assert_eq!(frequency_to_band(5955), Some("6 GHz"));
        assert_eq!(frequency_to_band(6115), Some("6 GHz"));
        assert_eq!(frequency_to_band(7115), Some("6 GHz"));
    }

    #[test]
    fn frequency_unknown_band() {
        assert_eq!(frequency_to_band(0), None);
        assert_eq!(frequency_to_band(900), None);
    }

    #[test]
    fn wired_speed_mbps() {
        assert_eq!(format_wired_speed(100), "100 Mbps");
        assert_eq!(format_wired_speed(10), "10 Mbps");
    }

    #[test]
    fn wired_speed_gbps() {
        assert_eq!(format_wired_speed(1000), "1 Gbps");
        assert_eq!(format_wired_speed(2500), "2.5 Gbps");
        assert_eq!(format_wired_speed(10000), "10 Gbps");
    }

    #[test]
    fn signal_icon_none() {
        assert_eq!(signal_strength_icon(0), "cm-wireless-signal-none-symbolic");
        assert_eq!(signal_strength_icon(19), "cm-wireless-signal-none-symbolic");
    }

    #[test]
    fn signal_icon_weak() {
        assert_eq!(signal_strength_icon(20), "cm-wireless-signal-weak-symbolic");
        assert_eq!(signal_strength_icon(39), "cm-wireless-signal-weak-symbolic");
    }

    #[test]
    fn signal_icon_ok() {
        assert_eq!(signal_strength_icon(40), "cm-wireless-signal-ok-symbolic");
        assert_eq!(signal_strength_icon(59), "cm-wireless-signal-ok-symbolic");
    }

    #[test]
    fn signal_icon_good() {
        assert_eq!(signal_strength_icon(60), "cm-wireless-signal-good-symbolic");
        assert_eq!(signal_strength_icon(79), "cm-wireless-signal-good-symbolic");
    }

    #[test]
    fn signal_icon_excellent() {
        assert_eq!(
            signal_strength_icon(80),
            "cm-wireless-signal-excellent-symbolic"
        );
        assert_eq!(
            signal_strength_icon(100),
            "cm-wireless-signal-excellent-symbolic"
        );
    }

    #[test]
    fn open_network_needs_no_password() {
        assert!(!requires_password(SecurityType::None));
    }

    #[test]
    fn secured_networks_need_password() {
        assert!(requires_password(SecurityType::Wep));
        assert!(requires_password(SecurityType::Wpa));
        assert!(requires_password(SecurityType::Wpa2));
        assert!(requires_password(SecurityType::Wpa3));
    }

    #[test]
    fn enterprise_needs_no_simple_password() {
        assert!(!requires_password(SecurityType::Enterprise));
    }
}
