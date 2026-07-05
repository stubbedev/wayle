//! wayle-greeter: a greetd greeter that shares the wayle lock screen's theme.
//!
//! Runs as the single client of a kiosk compositor (e.g. `cage`) spawned by
//! `greetd`, presents the shared credential box, and drives the login over the
//! greetd IPC socket. On success greetd starts the configured session and
//! replaces this process.

mod app;
mod config;
mod cursor;
mod i18n;
mod session;
mod users;

use relm4::RelmApp;
use tracing::info;
use tracing_subscriber::EnvFilter;

use crate::{
    i18n::t,
    session::{Session, SessionKind},
};

fn main() {
    init_tracing();

    let options = match config::Options::from_args() {
        Ok(options) => options,
        Err(message) => {
            eprintln!("{message}");
            std::process::exit(2);
        }
    };

    let config = config::load(&options.config_path);

    // Sessions discovered from the `wayland-sessions` + `xsessions` dirs, plus
    // the optional explicit `-- <argv>` as a "Custom" entry (or the sole
    // session when none are discovered — e.g. a single-session kiosk).
    let mut sessions = session::discover(&options.session_dirs, SessionKind::Wayland);
    sessions.extend(session::discover(&options.xsession_dirs, SessionKind::X11));
    sessions.sort_by_key(|s| s.name.to_lowercase());
    if !options.command.is_empty() {
        sessions.push(Session {
            id: "custom".to_owned(),
            name: t!("greeter-custom-session"),
            exec: options.command.clone(),
        });
    }
    if sessions.is_empty() {
        eprintln!(
            "no sessions found in {:?} or {:?} and no `-- <argv>` fallback given; \
             nothing to log into",
            options.session_dirs, options.xsession_dirs
        );
        std::process::exit(2);
    }

    let last_session = session::load_last(&options.state_path);
    let last_user = session::load_last(&options.state_path.with_file_name("last-user"));
    info!(count = sessions.len(), "wayle-greeter starting");

    let init = app::GreeterInit {
        config,
        sessions,
        users: users::load(),
        last_session,
        last_user,
        state_path: options.state_path,
        session_env: options.env,
    };

    // Hand GApplication only argv[0]. Our own flags (--config, --sessions,
    // --xsessions, --state, --env) were already consumed by `config::Options`
    // above; without this, RelmApp forwards the full `std::env::args()` to
    // GApplication, which rejects the first unknown option ("Unknown option
    // --config") and aborts before any window is shown.
    let app = RelmApp::new("dev.stubbe.wayle.greeter")
        .with_args(vec![std::env::args().next().unwrap_or_default()]);
    app.run::<app::Greeter>(init);
}

/// Initializes tracing from `RUST_LOG` (defaulting to `info`).
fn init_tracing() {
    let filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));
    tracing_subscriber::fmt().with_env_filter(filter).init();
}
