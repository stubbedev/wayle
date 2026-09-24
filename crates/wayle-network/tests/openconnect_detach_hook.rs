//! The NetworkManager `vpn-pre-down` hook wayle ships in
//! `resources/90-wayle-openconnect-detach`.
//!
//! It is the one piece of wayle's VPN support that runs as root, so it is
//! exercised here for real: against stand-in `openconnect` processes that
//! record which signal reached them. SIGHUP is the detach that keeps the
//! gateway session alive; anything else, or a signal at the wrong process,
//! is the bug.

use std::{
    error::Error,
    fs,
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
    process::{Child, Command},
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    thread,
    time::{Duration, Instant},
};

type TestResult = Result<(), Box<dyn Error>>;

const HOOK: &str = concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../../resources/90-wayle-openconnect-detach"
);

/// A process whose name is `openconnect`, started with `--interface <iface>`
/// the way the plugin starts the real one, that writes the signal it got to
/// `<dir>/<iface>.signal` and exits.
fn fake_openconnect(dir: &Path, iface: &str) -> Result<Child, Box<dyn Error>> {
    let binary = dir.join("openconnect");
    if !binary.exists() {
        fs::write(
            &binary,
            "#!/bin/sh\n\
             out=\"$1\"; shift\n\
             trap 'echo HUP > \"$out\"; exit 0' HUP\n\
             trap 'echo INT > \"$out\"; exit 0' INT\n\
             trap 'echo TERM > \"$out\"; exit 0' TERM\n\
             while :; do sleep 0.05; done\n",
        )?;
        fs::set_permissions(&binary, fs::Permissions::from_mode(0o755))?;
    }
    let child = Command::new(&binary)
        .arg(dir.join(format!("{iface}.signal")))
        .args(["--protocol", "gp", "--interface", iface, "vpn.example.com"])
        .spawn()?;
    // pgrep has to see it under its own name before the hook runs.
    let deadline = Instant::now() + Duration::from_secs(5);
    while Instant::now() < deadline {
        let comm = fs::read_to_string(format!("/proc/{}/comm", child.id())).unwrap_or_default();
        if comm.trim() == "openconnect" {
            break;
        }
        thread::sleep(Duration::from_millis(20));
    }
    Ok(child)
}

fn run_hook(iface: &str, action: &str) -> TestResult {
    let status = Command::new("sh").args([HOOK, iface, action]).status()?;
    check_status(status)
}

/// Runs the hook with a stand-in `nmcli` first on PATH, reporting `state` as
/// NM's general state and `tuns` as its tun devices, next to a wired device
/// that is not one.
fn run_hook_with_nm(
    dir: &Path,
    state: &str,
    tuns: &[&str],
    iface: &str,
    action: &str,
) -> TestResult {
    let bin = dir.join("bin");
    fs::create_dir_all(&bin)?;
    let devices: String = std::iter::once(String::from("enxfake:ethernet"))
        .chain(tuns.iter().map(|tun| format!("{tun}:tun")))
        .map(|line| format!(" '{line}'"))
        .collect();
    let nmcli = bin.join("nmcli");
    fs::write(
        &nmcli,
        format!(
            "#!/bin/sh\n\
             case \"$*\" in\n\
             *general*) echo '{state}' ;;\n\
             *device*) printf '%s\\n'{devices} ;;\n\
             *) exit 1 ;;\n\
             esac\n"
        ),
    )?;
    fs::set_permissions(&nmcli, fs::Permissions::from_mode(0o755))?;
    let path = format!(
        "{}:{}",
        bin.display(),
        std::env::var("PATH").unwrap_or_default()
    );
    let status = Command::new("sh")
        .args([HOOK, iface, action])
        .env("PATH", path)
        .status()?;
    check_status(status)
}

fn check_status(status: std::process::ExitStatus) -> TestResult {
    if !status.success() {
        return Err(format!("the hook must never fail a disconnect; it exited {status}").into());
    }
    Ok(())
}

/// Waits up to a second for `iface`'s stand-in to record a signal.
fn signal_within(dir: &Path, iface: &str) -> Option<String> {
    let deadline = Instant::now() + Duration::from_secs(1);
    loop {
        let seen = signal_seen(dir, iface);
        if seen.is_some() || Instant::now() >= deadline {
            return seen;
        }
        thread::sleep(Duration::from_millis(20));
    }
}

fn signal_seen(dir: &Path, iface: &str) -> Option<String> {
    fs::read_to_string(dir.join(format!("{iface}.signal")))
        .ok()
        .map(|signal| signal.trim().to_owned())
}

fn scratch(label: &str) -> Result<PathBuf, Box<dyn Error>> {
    let dir = std::env::temp_dir().join(format!("wayle-detach-{label}-{}", std::process::id()));
    let _ = fs::remove_dir_all(&dir);
    fs::create_dir_all(&dir)?;
    Ok(dir)
}

fn stop(mut child: Child) {
    let _ = child.kill();
    let _ = child.wait();
}

#[test]
fn pre_down_detaches_the_tunnels_openconnect_with_sighup() -> TestResult {
    let dir = scratch("detach")?;
    let mut tunnel = fake_openconnect(&dir, "wayletest0")?;
    // Reaped as soon as it exits, the way the plugin reaps the real one: an
    // unreaped zombie still answers `kill -0`, and would hold the hook in its
    // wait loop for the full timeout.
    let exited = Arc::new(AtomicBool::new(false));
    let reaper = {
        let exited = Arc::clone(&exited);
        thread::spawn(move || {
            let _ = tunnel.wait();
            exited.store(true, Ordering::SeqCst);
        })
    };

    let started = Instant::now();
    run_hook("wayletest0", "vpn-pre-down")?;

    // The hook waits for the detach to finish, so the process is gone by the
    // time it returns: that wait is what keeps the plugin's SIGINT from
    // racing it.
    assert!(
        exited.load(Ordering::SeqCst),
        "the hook returned before openconnect had detached"
    );
    assert!(
        started.elapsed() < Duration::from_secs(3),
        "the hook kept waiting after openconnect was gone"
    );
    assert_eq!(signal_seen(&dir, "wayletest0").as_deref(), Some("HUP"));
    let _ = reaper.join();
    let _ = fs::remove_dir_all(&dir);
    Ok(())
}

#[test]
fn another_tunnels_openconnect_is_left_alone() -> TestResult {
    let dir = scratch("other")?;
    let other = fake_openconnect(&dir, "wayletest1")?;

    run_hook("wayletest2", "vpn-pre-down")?;

    thread::sleep(Duration::from_millis(200));
    assert_eq!(
        signal_seen(&dir, "wayletest1"),
        None,
        "the hook signalled an openconnect running a different interface"
    );
    stop(other);
    let _ = fs::remove_dir_all(&dir);
    Ok(())
}

#[test]
fn any_other_dispatcher_action_is_a_no_op() -> TestResult {
    let dir = scratch("action")?;
    let tunnel = fake_openconnect(&dir, "wayletest3")?;

    // NM asleep, and the tunnel listed: only the actions that tear a tunnel
    // down may act on that, and these do not.
    for action in ["vpn-down", "vpn-up", "down", "up", ""] {
        run_hook_with_nm(&dir, "asleep", &["wayletest3"], "wayletest3", action)?;
    }

    thread::sleep(Duration::from_millis(200));
    assert_eq!(
        signal_seen(&dir, "wayletest3"),
        None,
        "the hook acted on something other than a pre-down"
    );
    stop(tunnel);
    let _ = fs::remove_dir_all(&dir);
    Ok(())
}

#[test]
fn nothing_to_detach_still_succeeds() -> TestResult {
    run_hook("wayletest-absent", "vpn-pre-down")?;
    run_hook("", "vpn-pre-down")
}

#[test]
fn a_device_going_down_for_a_suspend_detaches_every_tunnel() -> TestResult {
    // A suspend takes the tunnels down with their devices and never
    // dispatches vpn-pre-down; the device's pre-down, with NM asleep, is the
    // only warning openconnect gets before the plugin's SIGINT logs it off.
    let dir = scratch("asleep")?;
    // Reaped as they exit, as in the single-tunnel case, so the hook's wait
    // loop sees each one go.
    let reapers: Vec<_> = ["wayletest4", "wayletest5"]
        .into_iter()
        .map(|iface| {
            fake_openconnect(&dir, iface).map(|mut child| thread::spawn(move || child.wait()))
        })
        .collect::<Result<_, _>>()?;
    let unlisted = fake_openconnect(&dir, "wayletest6")?;

    let started = Instant::now();
    run_hook_with_nm(
        &dir,
        "asleep",
        &["wayletest4", "wayletest5"],
        "enxfake",
        "pre-down",
    )?;

    assert_eq!(signal_within(&dir, "wayletest4").as_deref(), Some("HUP"));
    assert_eq!(signal_within(&dir, "wayletest5").as_deref(), Some("HUP"));
    assert!(
        started.elapsed() < Duration::from_secs(3),
        "the hook kept waiting after openconnect was gone"
    );
    assert_eq!(
        signal_seen(&dir, "wayletest6"),
        None,
        "the hook signalled an openconnect on an interface NM does not list"
    );
    for reaper in reapers {
        let _ = reaper.join();
    }
    stop(unlisted);
    let _ = fs::remove_dir_all(&dir);
    Ok(())
}

#[test]
fn a_device_going_down_while_awake_leaves_the_tunnels_alone() -> TestResult {
    let dir = scratch("awake")?;
    let tunnel = fake_openconnect(&dir, "wayletest7")?;

    for state in ["connected", "disconnected", ""] {
        run_hook_with_nm(&dir, state, &["wayletest7"], "enxfake", "pre-down")?;
    }

    thread::sleep(Duration::from_millis(200));
    assert_eq!(
        signal_seen(&dir, "wayletest7"),
        None,
        "the hook detached a tunnel for a device NM took down while awake"
    );
    stop(tunnel);
    let _ = fs::remove_dir_all(&dir);
    Ok(())
}

#[test]
fn a_suspend_with_no_tunnels_still_succeeds() -> TestResult {
    let dir = scratch("asleep-empty")?;
    run_hook_with_nm(&dir, "asleep", &[], "enxfake", "pre-down")?;
    let _ = fs::remove_dir_all(&dir);
    Ok(())
}
