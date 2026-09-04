//! Socket lifetime contract for `wayle launcher`.
//!
//! The daemon treats EOF on the session socket as "the client died" and tears
//! the surface down, so the CLI must hold its write half open for the whole
//! session and close it only when it exits. A regression here makes every
//! `-show` session flash open and vanish, with the CLI exiting 1 silently.

use std::{
    error::Error,
    fs,
    io::{BufRead, BufReader, ErrorKind, Read, Write},
    os::unix::net::{UnixListener, UnixStream},
    path::PathBuf,
    process::{Child, Command, Stdio},
    time::Duration,
};

type TestResult = Result<(), Box<dyn Error>>;

/// A fake daemon: the runtime dir the CLI is pointed at, plus its listener.
struct Daemon {
    runtime_dir: PathBuf,
    listener: UnixListener,
}

impl Daemon {
    fn bind(name: &str) -> Result<Self, Box<dyn Error>> {
        // Short path on purpose: a unix socket path must fit in SUN_LEN.
        let runtime_dir =
            std::env::temp_dir().join(format!("wayle-test-{name}-{}", std::process::id()));
        let socket_dir = runtime_dir.join("wayle");
        let _ = fs::remove_dir_all(&runtime_dir);
        fs::create_dir_all(&socket_dir)?;
        let listener = UnixListener::bind(socket_dir.join("launcher.sock"))?;
        Ok(Self {
            runtime_dir,
            listener,
        })
    }

    /// Spawn the CLI against this daemon.
    fn spawn(&self, args: &[&str], stdin: Stdio) -> Result<Child, Box<dyn Error>> {
        Ok(Command::new(env!("CARGO_BIN_EXE_wayle"))
            .args(args)
            .env("XDG_RUNTIME_DIR", &self.runtime_dir)
            .stdin(stdin)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()?)
    }

    /// Accept the session, assert the open frame, and reply `opened`.
    fn accept_session(&self) -> Result<(BufReader<UnixStream>, UnixStream), Box<dyn Error>> {
        let (stream, _) = self.listener.accept()?;
        let mut writer = stream.try_clone()?;
        let mut reader = BufReader::new(stream);
        let mut open = String::new();
        reader.read_line(&mut open)?;
        assert!(
            open.contains("\"type\":\"open\""),
            "first frame was not open: {open}"
        );
        writer.write_all(b"{\"type\":\"opened\"}\n")?;
        reader
            .get_ref()
            .set_read_timeout(Some(Duration::from_millis(750)))?;
        Ok((reader, writer))
    }
}

impl Drop for Daemon {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.runtime_dir);
    }
}

fn read_frame(reader: &mut BufReader<UnixStream>) -> Result<String, Box<dyn Error>> {
    let mut line = String::new();
    reader.read_line(&mut line)?;
    Ok(line)
}

fn reap(child: &mut Child) {
    let _ = child.kill();
    let _ = child.wait();
}

/// Positive: a live `-show` session keeps the socket open in both directions,
/// so the daemon never sees the EOF that would cancel it.
#[test]
fn live_show_session_never_looks_like_a_dead_client() -> TestResult {
    let daemon = Daemon::bind("live")?;
    let mut child = daemon.spawn(&["launcher", "-show", "drun"], Stdio::null())?;
    let (mut reader, _writer) = daemon.accept_session()?;

    let mut byte = [0u8; 1];
    match reader.read(&mut byte) {
        Ok(0) => {
            reap(&mut child);
            return Err(
                "daemon saw EOF while the session was live: the CLI half-closed its socket".into(),
            );
        }
        Ok(_) => {
            reap(&mut child);
            return Err("unexpected client frame during an idle -show session".into());
        }
        Err(error) => assert!(
            matches!(error.kind(), ErrorKind::WouldBlock | ErrorKind::TimedOut),
            "unexpected socket error: {error}"
        ),
    }
    assert!(
        child.try_wait()?.is_none(),
        "CLI exited while the session was still open"
    );

    reap(&mut child);
    Ok(())
}

/// Negative: the socket closes exactly once, when the CLI exits on a terminal
/// frame — that EOF is what the daemon relies on to reap a dead client.
#[test]
fn client_exit_closes_the_socket_and_returns_the_daemon_code() -> TestResult {
    let daemon = Daemon::bind("cancel")?;
    let mut child = daemon.spawn(&["launcher", "-show", "drun"], Stdio::null())?;
    let (mut reader, mut writer) = daemon.accept_session()?;

    writer.write_all(b"{\"type\":\"cancelled\",\"code\":1}\n")?;
    let status = child.wait()?;
    assert_eq!(
        status.code(),
        Some(1),
        "cancel must exit with rofi's code 1"
    );

    let mut byte = [0u8; 1];
    assert_eq!(
        reader.read(&mut byte)?,
        0,
        "socket must reach EOF once the CLI is gone"
    );
    Ok(())
}

/// Positive: `-dmenu` still owns the write half — rows and the EOF marker
/// reach the daemon, which is the one case that legitimately writes.
#[test]
fn dmenu_session_streams_rows_then_signals_eof() -> TestResult {
    let daemon = Daemon::bind("dmenu")?;
    let mut child = daemon.spawn(&["launcher", "-dmenu"], Stdio::piped())?;
    child
        .stdin
        .take()
        .ok_or("stdin was not piped")?
        .write_all(b"alpha\nbravo\n")?;
    let (mut reader, _writer) = daemon.accept_session()?;

    let rows = read_frame(&mut reader)?;
    assert!(
        rows.contains("\"type\":\"rows\"") && rows.contains("alpha") && rows.contains("bravo"),
        "expected a rows frame, got: {rows}"
    );
    let done = read_frame(&mut reader)?;
    assert!(
        done.contains("\"type\":\"rows-done\""),
        "expected rows-done after stdin EOF, got: {done}"
    );

    reap(&mut child);
    Ok(())
}

/// Negative: finishing the rows must not look like the client dying.
///
/// The row pump owns the write half, so returning from it dropped the half
/// and half-closed the socket — the daemon read that FIN as a dead client and
/// tore the menu down the instant the rows arrived. `-dmenu` menus vanished
/// immediately and the CLI exited 1 without a word.
#[test]
fn dmenu_session_stays_open_after_its_rows_are_done() -> TestResult {
    let daemon = Daemon::bind("dmenu-live")?;
    let mut child = daemon.spawn(&["launcher", "-dmenu"], Stdio::piped())?;
    child
        .stdin
        .take()
        .ok_or("stdin was not piped")?
        .write_all(b"alpha\nbravo\n")?;
    let (mut reader, _writer) = daemon.accept_session()?;

    let _rows = read_frame(&mut reader)?;
    let done = read_frame(&mut reader)?;
    assert!(
        done.contains("\"type\":\"rows-done\""),
        "expected rows-done first, got: {done}"
    );

    let mut byte = [0u8; 1];
    match reader.read(&mut byte) {
        Ok(0) => {
            reap(&mut child);
            return Err(
                "daemon saw EOF once the rows were done: the dmenu pump half-closed the socket"
                    .into(),
            );
        }
        Ok(_) => {
            reap(&mut child);
            return Err("unexpected client frame after rows-done".into());
        }
        Err(error) => assert!(
            matches!(error.kind(), ErrorKind::WouldBlock | ErrorKind::TimedOut),
            "unexpected socket error: {error}"
        ),
    }
    assert!(
        child.try_wait()?.is_none(),
        "CLI exited while the dmenu session was still open"
    );

    reap(&mut child);
    Ok(())
}
