//! Native wallpaper rendering.
//!
//! wayle is the wallpaper provider — no `swww`/`awww`. One transparent
//! `Layer::Background` window per monitor holds a `gtk::Stack` of two
//! `gtk::Picture`s; changing the wallpaper swaps the off-screen picture and
//! flips the stack (crossfade, or instant when the transition is off).
//!
//! The desired image per monitor comes from the reactive
//! [`WallpaperService::monitors`] state; this module watches it and reconciles
//! surfaces (create/update/remove) as monitors and wallpapers change.

use std::{cell::Cell, collections::HashMap, path::PathBuf, rc::Rc, sync::Arc};

use gtk4_layer_shell::{Edge, KeyboardMode, Layer, LayerShell};
use relm4::gtk::{self, gdk, glib, prelude::*};
use tracing::warn;
use wayle_config::{ConfigService, schemas::wallpaper::WallpaperTransition};
use wayle_wallpaper::{FitMode, MonitorState, WallpaperService};

use crate::shell::helpers::monitors::current_monitors;

/// Keeps the wallpaper render task (and its per-monitor surfaces) alive.
pub(crate) struct Wallpaper {
    _task: glib::JoinHandle<()>,
}

impl Wallpaper {
    /// Spawns the render task that mirrors the service's per-monitor state onto
    /// native `Layer::Background` surfaces.
    pub(crate) fn spawn(service: Arc<WallpaperService>, config: Arc<ConfigService>) -> Self {
        let surfaces = Rc::new(std::cell::RefCell::new(HashMap::<String, Surface>::new()));
        let mut stream = service.monitors.watch();

        let task = glib::spawn_future_local(async move {
            use futures::StreamExt;
            while let Some(monitors) = stream.next().await {
                reconcile(&surfaces, &monitors, &config);
            }
        });

        Self { _task: task }
    }
}

/// One monitor's background surface: a window with a crossfading two-picture stack.
struct Surface {
    stack: gtk::Stack,
    pictures: [gtk::Picture; 2],
    visible: Cell<usize>,
    last_path: Cell<Option<PathBuf>>,
    // Held so the window stays mapped for the surface's lifetime.
    _window: gtk::Window,
}

/// Creates/updates/removes per-monitor surfaces to match `monitors`.
fn reconcile(
    surfaces: &Rc<std::cell::RefCell<HashMap<String, Surface>>>,
    monitors: &HashMap<String, MonitorState>,
    config: &Arc<ConfigService>,
) {
    let gdk_monitors: HashMap<String, gdk::Monitor> = current_monitors().into_iter().collect();
    let (transition_enabled, duration_ms) = transition_settings(config);
    // Global single-file wallpaper, used for monitors that have no wallpaper of
    // their own yet (e.g. hotplugged after startup, before any cycle tick).
    let fallback = {
        let path = config.config().wallpaper.wallpaper.get();
        (!path.is_empty()).then(|| PathBuf::from(path))
    };

    let mut map = surfaces.borrow_mut();

    // Drop surfaces for monitors that no longer exist.
    map.retain(|connector, _| monitors.contains_key(connector));

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
        if let Some(path) = state.wallpaper.as_ref().or(fallback.as_ref()) {
            surface.render(path, state.fit_mode, transition_enabled, duration_ms);
        }
    }
}

/// Resolves `(crossfade_enabled, duration_ms)` from the live config.
fn transition_settings(config: &ConfigService) -> (bool, u32) {
    let wp = &config.config().wallpaper;
    let enabled = matches!(wp.transition.get(), WallpaperTransition::Crossfade);
    let duration_ms = (wp.transition_duration.get().value() * 1000.0) as u32;
    (enabled, duration_ms)
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

        let stack = gtk::Stack::builder()
            .transition_type(gtk::StackTransitionType::Crossfade)
            .build();
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
            visible: Cell::new(0),
            last_path: Cell::new(None),
            _window: window,
        }
    }

    /// Renders `path` with `fit`, crossfading from the current image when enabled.
    fn render(&self, path: &PathBuf, fit: FitMode, transition: bool, duration_ms: u32) {
        let last = self.last_path.take();
        let unchanged = last.as_deref() == Some(path.as_path());
        self.last_path.set(last);
        if unchanged {
            return;
        }

        let texture = match gdk::Texture::from_filename(path) {
            Ok(texture) => texture,
            Err(err) => {
                warn!(path = %path.display(), %err, "cannot load wallpaper image");
                return;
            }
        };

        let next = self.visible.get() ^ 1;
        let picture = &self.pictures[next];
        picture.set_content_fit(content_fit(fit));
        picture.set_paintable(Some(&texture));

        self.stack
            .set_transition_duration(if transition { duration_ms } else { 0 });
        self.stack.set_visible_child(picture);

        self.visible.set(next);
        self.last_path.set(Some(path.clone()));
    }
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
