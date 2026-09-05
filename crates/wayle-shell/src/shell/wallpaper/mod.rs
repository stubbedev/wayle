//! Native wallpaper rendering.
//!
//! wayle is the wallpaper provider — no `swww`/`awww`. One transparent
//! `Layer::Background` window per monitor holds a `gtk::Stack` of two
//! `gtk::Picture`s; changing the wallpaper decodes the image off the GTK thread
//! and swaps the off-screen picture, flipping the stack with the shared
//! `[animations]` transition for [`AnimSurface::Wallpaper`].
//!
//! The desired image per monitor comes from the reactive
//! [`WallpaperService::monitors`] state; this module watches it and reconciles
//! surfaces (create/update/remove) as monitors and wallpapers change.

use std::{
    cell::{Cell, RefCell},
    collections::HashMap,
    path::{Path, PathBuf},
    rc::Rc,
    sync::Arc,
};

use futures::channel::oneshot;
use gtk4_layer_shell::{Edge, KeyboardMode, Layer, LayerShell};
use relm4::gtk::{self, gdk, glib, prelude::*};
use tracing::warn;
use wayle_config::{
    ConfigService,
    schemas::{
        animations::{AnimSurface, AnimationType},
        wallpaper::FitMode,
    },
};
use wayle_wallpaper::{MonitorState, WallpaperService};

use crate::shell::helpers::monitors::current_monitors;

/// Keeps the wallpaper render task (and its per-monitor surfaces) alive.
pub(crate) struct Wallpaper {
    _task: glib::JoinHandle<()>,
}

impl Wallpaper {
    /// Spawns the render task that mirrors the service's per-monitor state onto
    /// native `Layer::Background` surfaces.
    pub(crate) fn spawn(service: Arc<WallpaperService>, config: Arc<ConfigService>) -> Self {
        let surfaces = Rc::new(RefCell::new(HashMap::<String, Surface>::new()));
        // Last state seen from the service, so a GDK-driven reconcile has
        // something to render even though it carries no state of its own.
        let state = Rc::new(RefCell::new(service.monitors.get()));
        let mut stream = service.monitors.watch();

        let task = {
            let (surfaces, state, config) = (surfaces.clone(), state.clone(), config.clone());
            glib::spawn_future_local(async move {
                use futures::StreamExt;
                while let Some(monitors) = stream.next().await {
                    *state.borrow_mut() = monitors;
                    let monitors = state.borrow();
                    reconcile(&surfaces, &monitors, &config);
                }
            })
        };

        // The wallpaper service and GDK watch Wayland over separate connections,
        // so a hotplugged output (dock unplug/replug) reaches them at different
        // times. Reconciling only on the service's side loses the race whenever
        // it registers first: `reconcile` finds no GDK monitor to anchor a
        // surface to, skips the monitor, and nothing pokes it again — that
        // output stays wallpaper-less until the next config change or cycle
        // tick. Reconcile on GDK's list too, so whichever side arrives last
        // completes the pair.
        if let Some(display) = gdk::Display::default() {
            display.monitors().connect_items_changed(move |_, _, _, _| {
                sync_service_monitors(&service);
                let monitors = state.borrow();
                reconcile(&surfaces, &monitors, &config);
            });
        }

        Self { _task: task }
    }
}

/// One monitor's background surface: a window with a crossfading two-picture stack.
struct Surface {
    stack: gtk::Stack,
    pictures: [gtk::Picture; 2],
    visible: Rc<Cell<usize>>,
    last_path: Rc<RefCell<Option<PathBuf>>>,
    // Held so the window stays mapped for the surface's lifetime.
    _window: gtk::Window,
}

/// Creates/updates/removes per-monitor surfaces to match `monitors`.
fn reconcile(
    surfaces: &Rc<RefCell<HashMap<String, Surface>>>,
    monitors: &HashMap<String, MonitorState>,
    config: &Arc<ConfigService>,
) {
    let gdk_monitors: HashMap<String, gdk::Monitor> = current_monitors().into_iter().collect();

    let cfg = config.config();
    let transition = stack_transition(cfg.animations.transition_for(AnimSurface::Wallpaper, false));
    let duration_ms = cfg.animations.duration_for(AnimSurface::Wallpaper, false);

    // Global single-file wallpaper, used for monitors that have no wallpaper of
    // their own yet (e.g. hotplugged after startup, before any cycle tick).
    let fallback = {
        let path = cfg.wallpaper.wallpaper.get();
        (!path.is_empty()).then(|| PathBuf::from(path))
    };

    // Scaling: global `fit-mode`, overridden per monitor by `[[wallpaper.monitors]]`.
    let global_fit = cfg.wallpaper.fit_mode.get();
    let monitor_cfgs: Vec<_> = cfg
        .wallpaper
        .monitors
        .get()
        .into_iter()
        .filter(|m| !m.name.is_empty())
        .collect();
    let fit_overrides: HashMap<String, FitMode> = monitor_cfgs
        .iter()
        .map(|m| (m.name.clone(), m.fit_mode))
        .collect();
    // Same idea for the image itself: a monitor hotplugged after startup has no
    // wallpaper in the service state, so its `[[wallpaper.monitors]]` entry must
    // be consulted before falling back to the global one.
    let wallpaper_overrides: HashMap<String, PathBuf> = monitor_cfgs
        .iter()
        .filter(|m| !m.wallpaper.is_empty())
        .map(|m| (m.name.clone(), PathBuf::from(&m.wallpaper)))
        .collect();

    let mut map = surfaces.borrow_mut();

    // Drop surfaces for monitors that no longer exist.
    map.retain(|connector, _| keeps_surface(connector, monitors, &gdk_monitors));

    for (connector, state) in monitors {
        // Create a surface on first sighting (needs the GDK monitor).
        if !map.contains_key(connector)
            && let Some(monitor) = gdk_monitors.get(connector)
        {
            map.insert(connector.clone(), Surface::new(monitor));
        }

        let Some(surface) = map.get(connector) else {
            continue;
        };
        let wallpaper_override = wallpaper_overrides.get(connector);
        if let Some(path) = state
            .wallpaper
            .as_ref()
            .or(wallpaper_override)
            .or(fallback.as_ref())
        {
            let fit = fit_overrides.get(connector).copied().unwrap_or(global_fit);
            surface.render(path, fit, transition, duration_ms);
        }
    }
}

/// Mirrors GDK's monitor list into the service's registration.
///
/// The service discovers outputs over a Wayland connection of its own, on a
/// thread that stops dispatching for good if that connection ever errors — and
/// its map is what [`reconcile`] iterates, so past that point nothing would ever
/// paint a newly connected monitor. GDK is watching the same outputs anyway.
/// Both service calls are idempotent, so whichever side notices first wins and
/// the other no-ops.
fn sync_service_monitors(service: &WallpaperService) {
    let gdk_monitors: Vec<String> = current_monitors()
        .into_iter()
        .map(|(connector, _)| connector)
        .collect();

    let (added, removed) = monitor_diff(&gdk_monitors, &service.monitor_names());

    for connector in added {
        service.register_monitor(&connector);
    }
    for connector in removed {
        service.unregister_monitor(&connector);
    }
}

/// Connectors to register and to unregister to bring `registered` in line with
/// what GDK reports.
fn monitor_diff(gdk_monitors: &[String], registered: &[String]) -> (Vec<String>, Vec<String>) {
    let added = gdk_monitors
        .iter()
        .filter(|c| !registered.contains(c))
        .cloned()
        .collect();
    let removed = registered
        .iter()
        .filter(|c| !gdk_monitors.contains(c))
        .cloned()
        .collect();

    (added, removed)
}

/// Whether a live surface should be kept for `connector`.
///
/// Both sides have to still know it. Keying only on the service leaves a
/// surface bound to a `gdk::Monitor` GDK has already dropped: it renders
/// nowhere, and because the map still holds the connector, the replugged
/// monitor never gets a fresh surface built for it.
fn keeps_surface<S, G>(
    connector: &str,
    monitors: &HashMap<String, S>,
    gdk_monitors: &HashMap<String, G>,
) -> bool {
    monitors.contains_key(connector) && gdk_monitors.contains_key(connector)
}

impl Surface {
    fn new(monitor: &gdk::Monitor) -> Self {
        let window = gtk::Window::builder().decorated(false).build();
        window.add_css_class("wallpaper-window");
        window.init_layer_shell();
        window.set_namespace(Some("wayle-wallpaper"));
        window.set_layer(Layer::Background);
        window.set_monitor(Some(monitor));
        window.set_keyboard_mode(KeyboardMode::None);
        window.set_exclusive_zone(-1);
        for edge in [Edge::Top, Edge::Bottom, Edge::Left, Edge::Right] {
            window.set_anchor(edge, true);
        }

        let stack = gtk::Stack::new();
        let pictures = [gtk::Picture::new(), gtk::Picture::new()];
        for (i, picture) in pictures.iter().enumerate() {
            picture.set_can_shrink(true);
            stack.add_named(picture, Some(&i.to_string()));
        }

        window.set_child(Some(&stack));
        window.present();

        Self {
            stack,
            pictures,
            visible: Rc::new(Cell::new(0)),
            last_path: Rc::new(RefCell::new(None)),
            _window: window,
        }
    }

    /// Decodes `path` off the GTK thread, then crossfades it in with the shared
    /// transition. No-op when the path is already shown.
    fn render(
        &self,
        path: &Path,
        fit: FitMode,
        transition: gtk::StackTransitionType,
        duration_ms: u32,
    ) {
        if self.last_path.borrow().as_deref() == Some(path) {
            return;
        }
        // Record eagerly so rapid duplicate emissions don't re-decode.
        *self.last_path.borrow_mut() = Some(path.to_path_buf());

        let next = self.visible.get() ^ 1;
        let picture = self.pictures[next].clone();
        let stack = self.stack.clone();
        let visible = self.visible.clone();
        let content_fit = content_fit(fit);
        let path = path.to_path_buf();

        glib::spawn_future_local(async move {
            let (tx, rx) = oneshot::channel();
            let decode_path = path.clone();
            std::thread::spawn(move || {
                let _ = tx.send(decode(&decode_path));
            });

            let decoded = match rx.await {
                Ok(Ok(decoded)) => decoded,
                Ok(Err(err)) => {
                    return warn!(path = %path.display(), %err, "cannot decode wallpaper");
                }
                Err(_) => return,
            };

            let bytes = glib::Bytes::from_owned(decoded.pixels);
            let texture = gdk::MemoryTexture::new(
                decoded.width,
                decoded.height,
                gdk::MemoryFormat::R8g8b8a8,
                &bytes,
                decoded.stride,
            );

            picture.set_content_fit(content_fit);
            picture.set_paintable(Some(&texture));
            stack.set_transition_type(transition);
            stack.set_transition_duration(duration_ms);
            stack.set_visible_child(&picture);
            visible.set(next);
        });
    }
}

/// A decoded RGBA image ready to wrap in a `gdk::MemoryTexture`.
struct Decoded {
    width: i32,
    height: i32,
    stride: usize,
    pixels: Vec<u8>,
}

/// Decodes an image file to RGBA8. Runs on a worker thread.
fn decode(path: &Path) -> Result<Decoded, String> {
    let image = image::open(path).map_err(|e| e.to_string())?.to_rgba8();
    let (width, height) = (image.width(), image.height());
    Ok(Decoded {
        width: width as i32,
        height: height as i32,
        stride: (width * 4) as usize,
        pixels: image.into_raw(),
    })
}

/// Maps a [`FitMode`] to the equivalent GTK [`gtk::ContentFit`].
fn content_fit(fit: FitMode) -> gtk::ContentFit {
    match fit {
        FitMode::Fill => gtk::ContentFit::Cover,
        FitMode::Fit => gtk::ContentFit::Contain,
        FitMode::Stretch => gtk::ContentFit::Fill,
        FitMode::Center => gtk::ContentFit::ScaleDown,
    }
}

/// Maps a shared [`AnimationType`] to a `gtk::Stack` transition. `Stack` has no
/// swing/bounce/genie variants, so those fall back to a crossfade.
fn stack_transition(anim: AnimationType) -> gtk::StackTransitionType {
    match anim {
        AnimationType::None => gtk::StackTransitionType::None,
        AnimationType::SlideUp => gtk::StackTransitionType::SlideUp,
        AnimationType::SlideDown => gtk::StackTransitionType::SlideDown,
        AnimationType::SlideLeft => gtk::StackTransitionType::SlideLeft,
        AnimationType::SlideRight => gtk::StackTransitionType::SlideRight,
        AnimationType::Fade
        | AnimationType::SwingUp
        | AnimationType::SwingDown
        | AnimationType::SwingLeft
        | AnimationType::SwingRight
        | AnimationType::Bounce
        | AnimationType::Genie
        | AnimationType::Zoom
        | AnimationType::Rotate
        | AnimationType::Flip => gtk::StackTransitionType::Crossfade,
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use super::{keeps_surface, monitor_diff};

    fn map(names: &[&str]) -> HashMap<String, ()> {
        names.iter().map(|n| ((*n).to_string(), ())).collect()
    }

    fn names(names: &[&str]) -> Vec<String> {
        names.iter().map(|n| (*n).to_string()).collect()
    }

    #[test]
    fn diff_registers_a_monitor_gdk_gained() {
        let (added, removed) = monitor_diff(&names(&["DP-1", "DP-2"]), &names(&["DP-1"]));

        assert_eq!(added, names(&["DP-2"]));
        assert!(removed.is_empty());
    }

    #[test]
    fn diff_unregisters_a_monitor_gdk_lost() {
        let (added, removed) = monitor_diff(&names(&["DP-1"]), &names(&["DP-1", "DP-2"]));

        assert!(added.is_empty());
        assert_eq!(removed, names(&["DP-2"]));
    }

    #[test]
    fn diff_is_empty_when_the_service_already_agrees() {
        // The service's own Wayland watcher normally gets there first; this must
        // then be a no-op rather than churning the registration.
        let (added, removed) = monitor_diff(&names(&["DP-1", "DP-2"]), &names(&["DP-2", "DP-1"]));

        assert!(added.is_empty(), "re-registered {added:?}");
        assert!(removed.is_empty(), "unregistered {removed:?}");
    }

    #[test]
    fn diff_replaces_the_whole_set_on_a_dock_swap() {
        let (added, removed) = monitor_diff(&names(&["HDMI-1"]), &names(&["DP-1", "DP-2"]));

        assert_eq!(added, names(&["HDMI-1"]));
        assert_eq!(removed, names(&["DP-1", "DP-2"]));
    }

    #[test]
    fn keeps_surface_while_both_sides_know_the_monitor() {
        assert!(keeps_surface(
            "DP-1",
            &map(&["DP-1", "DP-2"]),
            &map(&["DP-1"])
        ));
    }

    #[test]
    fn drops_surface_when_gdk_dropped_the_monitor() {
        // Service hasn't unregistered it yet, but the `gdk::Monitor` the surface
        // is anchored to is gone. Keeping it would block the replug rebuild.
        assert!(!keeps_surface("DP-1", &map(&["DP-1"]), &map(&["DP-2"])));
    }

    #[test]
    fn drops_surface_when_service_unregistered_the_monitor() {
        assert!(!keeps_surface("DP-1", &map(&["DP-2"]), &map(&["DP-1"])));
    }

    #[test]
    fn drops_surface_when_neither_side_knows_the_monitor() {
        assert!(!keeps_surface("DP-1", &map(&[]), &map(&[])));
    }
}
