{
  description = "wayle — a Wayland desktop shell (Rust + GTK4 + Relm4)";

  # Always use the self-hosted attic cache for wayle builds (CI, releases, and
  # local dev via `nix build`). Read access is public; pushes are CI/release
  # only (ATTIC_TOKEN). Users are prompted before an untrusted flake's config
  # is honoured, or add the key to trusted-public-keys in nix.conf.
  nixConfig = {
    extra-substituters = [ "https://nix.stubbe.dev/wayle" ];
    extra-trusted-public-keys = [ "wayle:XD2O2h1Mmka+VegRi2JY7ywNbG9al+TUAZp6CObizFU=" ];
  };

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
          # GStreamer plugin packages the recorder dlopens at runtime
          # (pipewiresrc, v4l2src, x264enc, opusenc, mp4/matroska/webm mux,
          # compositor). pipewire ships the pipewiresrc plugin.
          gstPlugins = (with pkgs.gst_all_1; [
            gstreamer
            gst-plugins-base
            gst-plugins-good
            gst-plugins-bad
            gst-plugins-ugly
            gst-libav
          ]) ++ [ pkgs.pipewire ];

          # Native libraries the workspace links and dlopens at runtime.
          libs = (with pkgs; [
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
            pam # libpam, linked by the lock screen's PAM auth
            systemd # provides libudev
          ]) ++ gstPlugins;
          # `nix develop` provides every native dependency `cargo build`,
          # `just check`, and the `release-*` recipes need. The Rust toolchain
          # itself comes from `rustup`, which reads the repo's
          # `rust-toolchain.toml` — the same pin CI honors. nixpkgs' own rust is
          # not used here because Cargo.toml's rust-version is ahead of it; this
          # keeps local and CI on a byte-identical compiler with no skew.
          default = pkgs.mkShell {
            # Build tools. pkg-config + each buildInput below populate
            # PKG_CONFIG_PATH automatically, so `just release-patch` works
            # straight out of `nix develop` with no manual env setup.
            nativeBuildInputs = with pkgs; [
              rustup # honors rust-toolchain.toml; matches CI's pinned toolchain
              pkg-config
              cmake
              clang
              mold # fast linker — wired in via RUSTFLAGS below
            ];

            # System libraries linked by the workspace and its -sys crates
            # (gtk4 + layer-shell, gtksourceview5, audio, cava/fftw, udev, …).
            buildInputs = libs;

            # Link with mold via clang instead of the default bfd linker — cuts
            # relink time hard with 591 crates. Mirrors the same RUSTFLAGS the
            # `nix build` package sets (see nix/package.nix); set here too so
            # plain `cargo build` in the devShell links with mold.
            RUSTFLAGS = "-C linker=clang -C link-arg=-fuse-ld=mold";

            # bindgen (via the cava build script) needs libclang at runtime.
            LIBCLANG_PATH = "${pkgs.llvmPackages.libclang.lib}/lib";

            # The compiled binaries dlopen GTK/glib/etc. at runtime, so `just
            # test` and `just run` need these on the loader path — linking alone
            # (via pkg-config) is not enough.
            LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath libs;

            # The recorder's GStreamer pipeline finds its plugins via this path.
            # Without it `cargo run`/`just run` inside the devShell can't load
            # pipewiresrc/x264enc/etc., so the recorder fails to start (the
            # `nix build` package sets the same var via preFixup).
            GST_PLUGIN_SYSTEM_PATH_1_0 =
              pkgs.lib.makeSearchPath "lib/gstreamer-1.0" gstPlugins;
          };
        in
        {
          inherit default;

          # `nix develop .#css` — same env as the default shell, but with
          # WAYLE_DEV=1 exported, so any `cargo run` (shell or wayle-settings)
          # hot-reloads SCSS from crates/wayle-styling/scss/** on save with no
          # restart. For rapid CSS iteration; `just dev-settings` runs a single
          # session the same way.
          css = default.overrideAttrs (old: {
            WAYLE_DEV = "1";
            shellHook = (old.shellHook or "") + ''
              echo "WAYLE_DEV=1 — SCSS hot-reload on. Edit crates/wayle-styling/scss/**, then: cargo run --bin wayle-settings"
            '';
          });
        }
      );
    };
}
