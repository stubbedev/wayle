{
  lib,
  buildGoModule,
  makeWrapper,
  glib,
  pam,
  pipewire,
  libgbm,
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
  pname = "wayle";
  version = "0.0.0-go";
  inherit src;

  # The module cache's hash: every go.mod change (a gelm bump) changes
  # it; `just go-vendor-hash` recomputes it (a stale one is reused, not
  # reported, the derivation being fixed-output).
  vendorHash = "sha256-1EtgTJYlrMwMGj/iSiBUFPsc11uojc0ole/I8YocowQ=";

  subPackages = [
    "cmd/wayle"
    "cmd/wayle-greeter"
    "cmd/wayle-lock"
  ];

  # gelm and the purego bindings are pure Go; cgo stays off, as in the
  # devShell.
  env.CGO_ENABLED = "0";
  ldflags = [ "-s" "-w" ];

  # The suite runs in `just go-check` (it needs a compositor and D-Bus).
  doCheck = false;

  nativeBuildInputs = [ makeWrapper ];

  # The binaries dlopen libgstreamer and libglib (recorder), libpam (lock
  # screen) and libpipewire (the ScreenCast portal) through purego, and
  # the recorder finds its GStreamer plugins on
  # GST_PLUGIN_SYSTEM_PATH_1_0.
  postFixup = ''
    for bin in wayle wayle-greeter wayle-lock; do
      wrapProgram $out/bin/$bin \
        --prefix LD_LIBRARY_PATH : "${lib.makeLibraryPath [ gst_all_1.gstreamer glib pam pipewire libgbm ]}" \
        --prefix GST_PLUGIN_SYSTEM_PATH_1_0 : "${lib.makeSearchPath "lib/gstreamer-1.0" gstPlugins}"
    done
  '';

  # What package.nix installs beside the binaries, which the NixOS and
  # home-manager modules read from the package.
  postInstall = ''
    # Handler for the globalprotectcallback: URI scheme (GlobalProtect
    # SAML sign-in answers).
    install -Dm0644 resources/com.wayle.vpn-sso-callback.desktop -t $out/share/applications
    # Polkit action for `pkexec wayle-greeter apply-config`.
    install -Dm0644 resources/dev.stubbe.wayle.greeter.policy \
      -t $out/share/polkit-1/actions
    install -Dm0644 resources/icons/hicolor/scalable/actions/*.svg \
      -t $out/share/icons/hicolor/scalable/actions
    # Reference copy of the systemd user unit; the modules define their own.
    install -Dm0644 resources/wayle.service -t $out/share/wayle

    # The xdg-desktop-portal backend: the interface declaration, the D-Bus
    # activation file, and the reference unit and portals.conf.
    install -Dm0644 resources/wayle.portal \
      -t $out/share/xdg-desktop-portal/portals
    install -d $out/share/dbus-1/services
    substitute resources/org.freedesktop.impl.portal.desktop.wayle.service \
      $out/share/dbus-1/services/org.freedesktop.impl.portal.desktop.wayle.service \
      --replace-fail /usr/bin/wayle "$out/bin/wayle"
    substitute resources/xdg-desktop-portal-wayle.service \
      $out/share/wayle/xdg-desktop-portal-wayle.service \
      --replace-fail /usr/bin/wayle "$out/bin/wayle"
    install -Dm0644 resources/wayle-portals.conf -t $out/share/wayle
  '';

  meta = {
    description = "Wayland desktop shell (the Go rewrite on gelm)";
    homepage = "https://github.com/stubbedev/wayle";
    license = lib.licenses.mit;
    mainProgram = "wayle";
    platforms = lib.platforms.linux;
  };
}
