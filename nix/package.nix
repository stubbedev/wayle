{
  lib,
  rustPlatform,
  pkg-config,
  cmake,
  clang,
  wrapGAppsHook4,
  llvmPackages,
  gtk4,
  gtk4-layer-shell,
  gtksourceview5,
  glib,
  cairo,
  pango,
  gdk-pixbuf,
  graphene,
  libxkbcommon,
  libpulseaudio,
  pipewire,
  fftw,
  systemd,
  wayland,
  gst_all_1,
}:
let
  cargoToml = lib.importTOML ../Cargo.toml;

  # GStreamer plugins the recorder loads at runtime (pipewiresrc, v4l2src,
  # x264enc, opusenc, mp4/matroska/webm mux, compositor, ...).
  gstPlugins = (with gst_all_1; [
    gstreamer
    gst-plugins-base
    gst-plugins-good
    gst-plugins-bad
    gst-plugins-ugly
    gst-libav
  ]) ++ [ pipewire ];
in
rustPlatform.buildRustPackage {
  pname = "wayle";
  version = cargoToml.workspace.package.version;

  # Only the inputs the build actually needs, so editing docs/scripts/nix
  # doesn't bust the build cache.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../Cargo.toml
      ../Cargo.lock
      ../crates
      ../wayle
      ../resources
    ];
  };

  cargoLock.lockFile = ../Cargo.lock;

  nativeBuildInputs = [
    pkg-config
    cmake
    clang
    wrapGAppsHook4
  ];

  buildInputs = [
    gtk4
    gtk4-layer-shell
    gtksourceview5
    glib
    cairo
    pango
    gdk-pixbuf
    graphene
    libxkbcommon
    libpulseaudio
    pipewire
    fftw
    systemd # libudev
    wayland
  ] ++ gstPlugins;

  # wayle-cava's build script runs bindgen, which needs libclang.
  env.LIBCLANG_PATH = "${llvmPackages.libclang.lib}/lib";

  # Make the recorder's GStreamer pipeline find its plugins at runtime.
  preFixup = ''
    gappsWrapperArgs+=(
      --prefix GST_PLUGIN_SYSTEM_PATH_1_0 : "${lib.makeSearchPath "lib/gstreamer-1.0" gstPlugins}"
    )
  '';

  postInstall = ''
    install -Dm0644 resources/com.wayle.settings.desktop -t $out/share/applications
    install -Dm0644 resources/wayle-settings.svg \
      $out/share/icons/hicolor/scalable/apps/wayle-settings.svg
    install -Dm0644 resources/icons/hicolor/scalable/actions/*.svg \
      -t $out/share/icons/hicolor/scalable/actions
    # Reference copy of the systemd user unit; the NixOS/home-manager modules
    # define their own unit with the correct store path.
    install -Dm0644 resources/wayle.service -t $out/share/wayle
  '';

  meta = {
    description = "Wayland desktop shell with a bar, dropdowns, OSD, and recorder (GTK4 + Relm4)";
    homepage = "https://github.com/wayle-rs/wayle";
    license = lib.licenses.mit;
    mainProgram = "wayle";
    platforms = lib.platforms.linux;
  };
}
