{
  lib,
  craneLib,
  rustPlatform,
  pkg-config,
  cmake,
  clang,
  mold,
  wrapGAppsHook4,
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
  pam,
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

  # Only the inputs the build actually needs, so editing docs/scripts/nix
  # doesn't bust the build cache. resources/ stays in because rust-embed
  # compiles it into the binary.
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

  # Args shared by the deps-only layer and the final build. crane keys the deps
  # derivation on Cargo.lock + these inputs (NOT on your source), so editing a
  # crate reuses the cached deps and only recompiles wayle's own crates.
  commonArgs = {
    inherit src;
    pname = "wayle";
    version = cargoToml.workspace.package.version;
    strictDeps = true;

    # CI's `test` job covers the suite. Skipping checks here applies to BOTH
    # the deps layer and the final build, so the cached `wayle-deps` derivation
    # doesn't compile + run unit tests — saves ~4.5 min and shrinks the artifact
    # pushed to the binary cache.
    doCheck = false;

    nativeBuildInputs = [
      pkg-config
      cmake
      clang
      mold
      # Sets LIBCLANG_PATH + BINDGEN_EXTRA_CLANG_ARGS (clang builtin headers and
      # libc) so the bindgen build scripts (wayle-cava, libspa-sys) work in the
      # sandbox. See the libspa-sys note in postConfigure for the rest.
      rustPlatform.bindgenHook
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
      pam # lock screen authenticates via libpam
    ] ++ gstPlugins;

    # libspa-sys defines cast macros like `SPA_ID_INVALID ((uint32_t)0xffffffff)`
    # that bindgen's cexpr can't const-evaluate, so it relies on bindgen's
    # clang-macro-fallback. That fallback writes a `.macro_eval.c` + `.pch` into
    # its build dir, which defaults to the build script's CWD — under crane the
    # crate's vendored manifest dir, a read-only /nix/store path. The write
    # fails silently, the constant is dropped, and libspa fails to compile
    # (`cannot find value SPA_ID_INVALID`). rustPlatform.buildRustPackage dodges
    # this because its cargoSetupHook copies the vendor dir writable; crane uses
    # it in place. So copy the vendor dir writable, repoint cargo at it, and
    # patch libspa-sys to point the fallback at $OUT_DIR (always writable).
    postConfigure = ''
      rwVendor="$NIX_BUILD_TOP/vendor-rw"
      cp -rL --no-preserve=mode,ownership "$cargoVendorDir" "$rwVendor"
      chmod -R u+rwX "$rwVendor"
      for brs in "$rwVendor"/*/libspa-sys-*/build.rs; do
        substituteInPlace "$brs" \
          --replace-fail ".clang_macro_fallback()" \
          ".clang_macro_fallback().clang_macro_fallback_build_dir(std::env::var(\"OUT_DIR\").unwrap())"
      done
      for cfg in $(find . -name config.toml -path '*cargo*' 2>/dev/null); do
        substituteInPlace "$cfg" --replace "$cargoVendorDir" "$rwVendor" || true
      done
    '';

    # Link with mold via clang (matches the devShell). Linking 30 crates + GTK
    # with the default bfd linker is a chunk of the final-crate build time; mold
    # cuts it. Applies to both the deps layer and the final build.
    env.RUSTFLAGS = "-C linker=clang -C link-arg=-fuse-ld=mold";
  };

  # The cached dependency layer. Built once per Cargo.lock; pushed to attic.
  cargoArtifacts = craneLib.buildDepsOnly commonArgs;
in
craneLib.buildPackage (
  commonArgs
  // {
    inherit cargoArtifacts;

    # GTK app wrapping is only needed for the final binary, not the deps layer.
    nativeBuildInputs = commonArgs.nativeBuildInputs ++ [ wrapGAppsHook4 ];

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

      # xdg-desktop-portal backend: the .portal declares which interfaces we
      # implement, the D-Bus service file lets the portal frontend activate
      # `wayle portal`, and the systemd unit + portals.conf are reference copies
      # the modules wire up.
      install -Dm0644 resources/wayle.portal \
        -t $out/share/xdg-desktop-portal/portals
      install -d $out/share/dbus-1/services
      substitute resources/org.freedesktop.impl.portal.desktop.wayle.service \
        $out/share/dbus-1/services/org.freedesktop.impl.portal.desktop.wayle.service \
        --replace-fail /usr/bin/wayle "$out/bin/wayle"
      install -d $out/share/wayle
      substitute resources/xdg-desktop-portal-wayle.service \
        $out/share/wayle/xdg-desktop-portal-wayle.service \
        --replace-fail /usr/bin/wayle "$out/bin/wayle"
      install -Dm0644 resources/wayle-portals.conf -t $out/share/wayle
    '';

    meta = {
      description = "Wayland desktop shell with a bar, dropdowns, OSD, and recorder (GTK4 + Relm4)";
      homepage = "https://github.com/stubbedev/wayle";
      license = lib.licenses.mit;
      mainProgram = "wayle";
      platforms = lib.platforms.linux;
    };
  }
)
