{
  description = "wayle — a Wayland desktop shell (Rust + GTK4 + Relm4)";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems =
        f: nixpkgs.lib.genAttrs systems (system: f (import nixpkgs { inherit system; }));
    in
    {
      devShells = forAllSystems (
        pkgs:
        let
          # Native libraries the workspace links and dlopens at runtime.
          libs = with pkgs; [
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
            systemd # provides libudev
            # GStreamer: the recorder builds against gstreamer-1.0 and loads
            # these plugins at runtime (pipewiresrc, v4l2src, x264enc, opusenc,
            # mp4/matroska/webm mux, compositor).
            gst_all_1.gstreamer
            gst_all_1.gst-plugins-base
            gst_all_1.gst-plugins-good
            gst_all_1.gst-plugins-bad
            gst_all_1.gst-plugins-ugly
            gst_all_1.gst-libav
          ];
        in
        {
        # `nix develop` provides every native dependency `cargo build`,
        # `just check`, and the `release-*` recipes need. The Rust toolchain
        # is intentionally NOT pinned here — Cargo.toml's rust-version is ahead
        # of nixpkgs, so use your own rustup toolchain from PATH.
        default = pkgs.mkShell {
          # Build tools. pkg-config + each buildInput below populate
          # PKG_CONFIG_PATH automatically, so `just release-patch` works
          # straight out of `nix develop` with no manual env setup.
          nativeBuildInputs = with pkgs; [
            pkg-config
            cmake
            clang
          ];

          # System libraries linked by the workspace and its -sys crates
          # (gtk4 + layer-shell, gtksourceview5, audio, cava/fftw, udev, …).
          buildInputs = libs;

          # bindgen (via the cava build script) needs libclang at runtime.
          LIBCLANG_PATH = "${pkgs.llvmPackages.libclang.lib}/lib";

          # The compiled binaries dlopen GTK/glib/etc. at runtime, so `just
          # test` and `just run` need these on the loader path — linking alone
          # (via pkg-config) is not enough.
          LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath libs;
        };
      });
    };
}
