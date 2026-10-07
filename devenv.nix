{
  pkgs,
  lib,
  ...
}:
let
  # GStreamer plugins the recorder dlopens at runtime (pipewiresrc, x264enc,
  # opusenc, muxes) — the full set the Rust shell shipped.
  gstPlugins = (with pkgs.gst_all_1; [
    gstreamer
    gst-plugins-base
    gst-plugins-good
    gst-plugins-bad
    gst-plugins-ugly
    gst-libav
  ]) ++ [ pkgs.pipewire ];

  # Native libraries the Rust workspace links and dlopens (GTK4 + layer-shell,
  # gtksourceview5, audio, cava/fftw, udev, gbm/drm).
  rustLibs = (with pkgs; [
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
    pam
    systemd
    libgbm
    libdrm
  ]) ++ gstPlugins;

  # Everything the compiled binaries dlopen: the Rust libs plus the go-test
  # extras (libei for the EIS server tests).
  runtimeLibs = rustLibs ++ [ pkgs.libei ];
in
{
  # Go rewrite toolchain. go.mod pins 1.27, ahead of nixpkgs' default go —
  # the pin is load-bearing (gelm parity): with the plain `go` package every
  # command would try to download a toolchain.
  languages.go = {
    enable = true;
    package = pkgs.go_1_27;
  };

  # Rust toolchain comes from rustup, which honors the repo's
  # rust-toolchain.toml (dated nightly, byte-identical to CI; nixpkgs' rust is
  # behind Cargo.toml's rust-version). devenv's languages.rust only offers a
  # drifting latest-nightly, so the pin lives here instead.
  packages =
    (with pkgs; [
      rustup

      pkg-config
      cmake
      clang
      mold
      cargo-nextest

      # Go lint/format/debug stack (config in .golangci.yml).
      golangci-lint
      gofumpt
      gopls
      delve
      grass-sass

      # Task runner.
      just

      # Headless test-session deps: fontconfig reads a generated config (see
      # enterShell), sway/dbus/pipewire/libei for the go-test gate.
      fontconfig
      sway
      dbus
      pipewire
      libei
    ])
    ++ rustLibs
    ++ (with pkgs; [
      inter
      jetbrains-mono
      nerd-fonts.jetbrains-mono
    ]);

  env = {
    # Link with mold via clang — cuts relink time hard with 591 crates;
    # -Zthreads is nightly-only, matching the rustup toolchain above.
    RUSTFLAGS = "-C linker=clang -C link-arg=-fuse-ld=mold -Zthreads=8";

    # bindgen (cava build script) needs libclang at runtime.
    LIBCLANG_PATH = "${pkgs.llvmPackages.libclang.lib}/lib";

    # Binaries dlopen GTK/glib/pipewire/gstreamer at runtime — linking via
    # pkg-config alone is not enough for `just test`/`just run`.
    LD_LIBRARY_PATH = lib.makeLibraryPath runtimeLibs;
    GST_PLUGIN_SYSTEM_PATH_1_0 = lib.makeSearchPath "lib/gstreamer-1.0" gstPlugins;

    # gelm is pure Go by design; keep accidental cgo out (`just go-race`
    # re-enables it per-invocation).
    CGO_ENABLED = "0";
  };

  enterShell = ''
    # rustup resolves rust-toolchain.toml and the pinned toolchain's own bin
    # (cargo/rustc/rustfmt/clippy-driver) onto PATH.
    export PATH="$(dirname "$("${lib.getExe pkgs.rustup}" which cargo)"):$PATH"

    # Layer the config's default fonts (Inter, JetBrains Mono, Nerd Font)
    # over the host fontconfig so the headless go-test gate finds fonts in a
    # bare session and sees the same set on a NixOS host.
    fontconf="''${TMPDIR:-/tmp}/wayle-go-fonts.conf"
    cat > "$fontconf" <<EOF
    <?xml version="1.0"?>
    <!DOCTYPE fontconfig SYSTEM "fonts.dtd">
    <fontconfig>
      <include ignore_missing="yes">/etc/fonts/fonts.conf</include>
      <dir>${pkgs.inter}/share/fonts</dir>
      <dir>${pkgs.jetbrains-mono}/share/fonts</dir>
      <dir>${pkgs.nerd-fonts.jetbrains-mono}/share/fonts</dir>
      <cachedir>''${XDG_CACHE_HOME:-$HOME/.cache}/fontconfig</cachedir>
    </fontconfig>
    EOF
    export FONTCONFIG_FILE="$fontconf"
  '';
}
