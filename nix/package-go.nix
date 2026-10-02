{
  lib,
  buildGoModule,
  makeWrapper,
  glib,
  pam,
  pipewire,
  gst_all_1,
}:
let
  # GStreamer plugins the recorder's pipelines load at runtime
  # (pipewiresrc, v4l2src, x264enc, opusenc, mp4/matroska/webm mux,
  # compositor), as in package.nix.
  gstPlugins = (with gst_all_1; [
    gstreamer
    gst-plugins-base
    gst-plugins-good
    gst-plugins-bad
    gst-plugins-ugly
    gst-libav
  ]) ++ [ pipewire ];

  # Only the Go tree, so editing the Rust crates, docs or nix files does
  # not rebuild it. The three locales packages live under crates/ and embed
  # the Fluent files the Rust crates share.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cava
      ../cmd
      ../config
      ../greeter
      ../i18n
      ../internal
      ../portal
      ../resources
      ../service
      ../shell
      ../strftime
      ../styling
      ../third_party
      ../crates/wayle-greeter/locales
      ../crates/wayle-i18n/locales
      ../crates/wayle-shell-core/locales
    ];
  };
in
buildGoModule {
  pname = "wayle-go";
  version = "0.0.0-go";
  inherit src;

  # The module cache's hash: every go.mod change (a gelm bump) changes
  # it; `just go-vendor-hash` recomputes it (a stale one is reused, not
  # reported, the derivation being fixed-output).
  vendorHash = "sha256-Wg1G5GgJ8h5j5Qk+be9ovLkE1XTQc1CV9KM9qxdiKhs=";

  subPackages = [ "cmd/wayle" ];

  # gelm and the purego bindings are pure Go; cgo stays off, as in the
  # devShell.
  env.CGO_ENABLED = "0";
  ldflags = [ "-s" "-w" ];

  # The suite runs in `just go-check` (it needs a compositor and D-Bus).
  doCheck = false;

  nativeBuildInputs = [ makeWrapper ];

  # The binary dlopens libgstreamer and libglib (recorder) and libpam
  # (lock screen) through purego, and the recorder finds its GStreamer
  # plugins on GST_PLUGIN_SYSTEM_PATH_1_0.
  postFixup = ''
    wrapProgram $out/bin/wayle \
      --prefix LD_LIBRARY_PATH : "${lib.makeLibraryPath [ gst_all_1.gstreamer glib pam ]}" \
      --prefix GST_PLUGIN_SYSTEM_PATH_1_0 : "${lib.makeSearchPath "lib/gstreamer-1.0" gstPlugins}"
  '';

  postInstall = ''
    install -Dm0644 resources/icons/hicolor/scalable/actions/*.svg \
      -t $out/share/icons/hicolor/scalable/actions
    install -Dm0644 resources/wayle.service -t $out/share/wayle
  '';

  meta = {
    description = "Wayland desktop shell (the Go rewrite on gelm)";
    homepage = "https://github.com/stubbedev/wayle";
    license = lib.licenses.mit;
    mainProgram = "wayle";
    platforms = lib.platforms.linux;
  };
}
