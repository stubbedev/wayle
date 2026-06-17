{
  description = "wayle — a Wayland desktop shell (Rust + GTK4 + Relm4)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    # crane builds the 591 deps as a separate, Cargo.lock-keyed derivation so a
    # source edit only recompiles wayle's own crates. See nix/package.nix.
    crane.url = "github:ipetkov/crane";
  };

  outputs =
    { self, nixpkgs, crane }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems =
        f: nixpkgs.lib.genAttrs systems (system: f (import nixpkgs { inherit system; }));
    in
    {
      packages = forAllSystems (pkgs: rec {
        wayle = pkgs.callPackage ./nix/package.nix { craneLib = crane.mkLib pkgs; };
        default = wayle;
      });

      # Adds `wayle` to a nixpkgs instance: `nixpkgs.overlays = [ wayle.overlays.default ];`
      overlays.default = _final: prev: {
        wayle = prev.callPackage ./nix/package.nix { craneLib = crane.mkLib prev; };
      };

      # NixOS: `imports = [ wayle.nixosModules.default ]; programs.wayle.enable = true;`
      nixosModules.default = import ./nix/nixos-module.nix self;

      # home-manager: `imports = [ wayle.homeManagerModules.default ]; programs.wayle.enable = true;`
      homeManagerModules.default = import ./nix/hm-module.nix self;

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
              mold # fast linker — wired in via RUSTFLAGS below
            ];

            # System libraries linked by the workspace and its -sys crates
            # (gtk4 + layer-shell, gtksourceview5, audio, cava/fftw, udev, …).
            buildInputs = libs;

            # Link with mold via clang instead of the default bfd linker — cuts
            # relink time hard with 591 crates. Scoped to the devShell (every
            # `just` recipe runs `nix develop --command cargo`), so it never
            # leaks into CI or the `nix build` package, which lack mold.
            RUSTFLAGS = "-C linker=clang -C link-arg=-fuse-ld=mold";

            # bindgen (via the cava build script) needs libclang at runtime.
            LIBCLANG_PATH = "${pkgs.llvmPackages.libclang.lib}/lib";

            # The compiled binaries dlopen GTK/glib/etc. at runtime, so `just
            # test` and `just run` need these on the loader path — linking alone
            # (via pkg-config) is not enough.
            LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath libs;
          };
        }
      );
    };
}
