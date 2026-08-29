//! Round-trips a VPN profile through a live NetworkManager.
//!
//! The profile builders assemble a nested D-Bus dictionary against NM's
//! documented shapes. Unit tests can only check that wayle built what it meant
//! to build — whether NM *accepts* it is a different question, and one that
//! fails at connect time with an unhelpful message when the answer is no.
//!
//! Ignored by default: it needs a running NetworkManager and the polkit
//! permission to modify system connections. Run it deliberately:
//!
//! ```sh
//! cargo test -p wayle-network --test vpn_profiles -- --ignored
//! ```
//!
//! Every profile it creates is deleted again, including on the failure paths
//! that matter — a leftover `wayle-test-*` connection in `nmcli` would be this
//! test's litter.

use std::collections::HashMap;

use wayle_network::{
    NetworkService,
    vpn::{kinds::WIREGUARD, profile},
};

/// Long enough for NM to announce the new profile and the service's watcher to
/// rebuild; short enough that a broken watcher fails the test rather than
/// hanging the suite.
const SETTLE: std::time::Duration = std::time::Duration::from_millis(1500);

fn values(pairs: &[(&str, &str)]) -> HashMap<String, String> {
    pairs
        .iter()
        .map(|(key, value)| (String::from(*key), String::from(*value)))
        .collect()
}

#[tokio::test]
#[ignore = "needs a live NetworkManager and polkit authorization"]
async fn a_wireguard_profile_round_trips_through_networkmanager() {
    let network = NetworkService::new()
        .await
        .expect("NetworkManager is reachable");

    let name = "wayle-test-wireguard";
    let submitted = values(&[
        ("interface", "wgtest0"),
        // A syntactically valid key that is not anyone's: NM validates the
        // base64 length and rejects a placeholder.
        (
            "private-key",
            "6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=",
        ),
        ("address", "10.123.45.2/24"),
        ("dns", "10.123.45.1"),
        (
            "peer-public-key",
            "Kx3AZBHm3vDJXPGRAJfvTvUEHY1c2Jw4qYE9nR6qEXY=",
        ),
        ("peer-endpoint", "vpn.invalid:51820"),
        ("peer-allowed-ips", "10.123.45.0/24"),
        ("peer-keepalive", "25"),
    ]);

    network
        .vpn
        .add(WIREGUARD, name, &submitted)
        .await
        .expect("NetworkManager accepts the WireGuard profile");

    tokio::time::sleep(SETTLE).await;

    let entry = network
        .vpn
        .entries
        .get()
        .into_iter()
        .find(|vpn| vpn.name.get() == name);
    let Some(entry) = entry else {
        panic!("the new profile did not reach the VPN list");
    };
    assert!(entry.wireguard, "a WireGuard profile must read back as one");

    let stored = network
        .vpn
        .settings_of(&entry.uuid)
        .await
        .expect("the saved profile reads back");
    assert_eq!(profile::kind_of(&stored).as_deref(), Some(WIREGUARD));

    let read = profile::read_values(&stored);
    assert_eq!(read.get("interface").map(String::as_str), Some("wgtest0"));

    network
        .vpn
        .remove(&entry.uuid)
        .await
        .expect("the profile deletes");

    tokio::time::sleep(SETTLE).await;
    assert!(
        !network
            .vpn
            .entries
            .get()
            .iter()
            .any(|vpn| vpn.name.get() == name),
        "the deleted profile is still listed"
    );
}

#[tokio::test]
#[ignore = "needs a live NetworkManager with the openconnect plugin installed"]
async fn an_openconnect_profile_round_trips_with_its_secret_flags() {
    let network = NetworkService::new()
        .await
        .expect("NetworkManager is reachable");

    let name = "wayle-test-openconnect";
    network
        .vpn
        .add(
            "org.freedesktop.NetworkManager.openconnect",
            name,
            &values(&[
                ("gateway", "vpn.invalid"),
                ("protocol", "gp"),
                ("wayle-username", "tester"),
            ]),
        )
        .await
        .expect("NetworkManager accepts the openconnect profile");

    tokio::time::sleep(SETTLE).await;

    let entry = network
        .vpn
        .entries
        .get()
        .into_iter()
        .find(|vpn| vpn.name.get() == name)
        .expect("the new profile reached the VPN list");
    assert!(
        !entry.wireguard,
        "a plugin VPN must not read back as WireGuard"
    );

    let stored = network
        .vpn
        .settings_of(&entry.uuid)
        .await
        .expect("the saved profile reads back");
    let read = profile::read_values(&stored);

    assert_eq!(read.get("gateway").map(String::as_str), Some("vpn.invalid"));
    assert_eq!(read.get("protocol").map(String::as_str), Some("gp"));
    // The username is wayle's own key; NM must round-trip a key its plugin
    // does not recognise rather than dropping it.
    assert_eq!(
        read.get("wayle-username").map(String::as_str),
        Some("tester")
    );

    network
        .vpn
        .remove(&entry.uuid)
        .await
        .expect("the profile deletes");
}
