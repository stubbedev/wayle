{
  description = "wayle — a Wayland desktop shell (Rust + GTK4 + Relm4)";

  # Always use the self-hosted xilo cache for wayle builds (CI, releases, and
  # local dev via `nix build`). Read access is public; pushes are CI/release
  # only (XILO_TOKEN). Users are prompted before an untrusted flake's config
  # is honoured, or add the key to trusted-public-keys in nix.conf.
  nixConfig = {
    extra-substituters = [ "https://nix.stubbe.dev/c/default/default" ];
    extra-trusted-public-keys = [ "default:6uWvXutL9cXjV3lii+Ur5ff+ArQoG4kMBKNXWrIxhHg=" ];
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

        # The cached 591-crate dependency layer. Not useful to install — it
        # exists so CI can build and push it to the xilo cache as its own store
        # path, which is what lets a fresh machine (or a fresh CI runner) skip
        # the ~30 min deps compile instead of relying on a GitHub Actions
        # /nix/store snapshot.
        wayle-deps = wayle.cargoArtifacts;
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
            libgbm # libgbm, linked by the portal screencast dmabuf path
            libdrm # libdrm, pulled in by gbm/drm-ffi
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
              cargo-nextest # `just test` runner
            ];

            # System libraries linked by the workspace and its -sys crates
            # (gtk4 + layer-shell, gtksourceview5, audio, cava/fftw, udev, …).
            buildInputs = libs;

            # Link with mold via clang instead of the default bfd linker — cuts
            # relink time hard with 591 crates. Mirrors the same RUSTFLAGS the
            # `nix build` package sets (see nix/package.nix); set here too so
            # plain `cargo build` in the devShell links with mold.
            # -Zthreads: parallel rustc frontend — big win on the largest
            # crates (wayle-shell, wayle-config). Nightly-only, so devShell
            # only (rustup honors rust-toolchain.toml); package.nix builds
            # with nixpkgs' stable rustc and must not get this flag.
            RUSTFLAGS = "-C linker=clang -C link-arg=-fuse-ld=mold -Zthreads=8";

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

          # The Go toolchain for the rewrite shell (`nix develop .#go`).
          # go.mod declares `go 1.27.1` and nixpkgs' default go is older,
          # so the pin is load-bearing exactly as in gelm's flake: with
          # the plain `go` package every command would try to download a
          # toolchain; go_1_27 keeps dev and CI offline-identical.
          goShell = pkgs.mkShell {
            packages = with pkgs; [
              go_1_27 # the pinned toolchain, see comment above
              golangci-lint # `just go-lint`, config in .golangci.yml (gelm parity)
              gofumpt # the formatter behind `golangci-lint fmt`
              gopls # Go language server
              delve # Go debugger
              just # task runner (`just go-check`)

              # Runtime deps of the pure-Go stack. gelm needs no native
              # libraries: it speaks the Wayland protocol and rasterizes
              # wl_shm itself, but its font scanner reads fontconfig, and
              # a headless test session has no host fonts to fall back on.
              fontconfig
              sway # headless compositor for the wayle-go test gate (gelm parity)

              inter # config default font-sans
              jetbrains-mono # config default font-mono
              nerd-fonts.jetbrains-mono # the user config's font-sans/font-mono
            ];

            shellHook =
              let
                # The config's defaults (Inter, JetBrains Mono) plus the
                # common Nerd Font, layered over the host fontconfig when
                # one exists: `wayle shell` finds fonts in a bare container
                # or headless session, and sees the same set on a NixOS
                # host as outside the shell.
                extraFontDirs = pkgs.lib.concatMapStrings
                  (font: "  <dir>${font}/share/fonts</dir>\n")
                  [
                    pkgs.inter
                    pkgs.jetbrains-mono
                    pkgs.nerd-fonts.jetbrains-mono
                  ];
              in
              ''
                # gelm is pure Go by design; keep accidental cgo out.
                export CGO_ENABLED=0

                fontconf="''${TMPDIR:-/tmp}/wayle-go-fonts.conf"
                cat > "$fontconf" <<EOF
                <?xml version="1.0"?>
                <!DOCTYPE fontconfig SYSTEM "fonts.dtd">
                <fontconfig>
                  <include ignore_missing="yes">/etc/fonts/fonts.conf</include>
                ${extraFontDirs}  <cachedir>''${XDG_CACHE_HOME:-$HOME/.cache}/fontconfig</cachedir>
                </fontconfig>
                EOF
                export FONTCONFIG_FILE="$fontconf"
                echo "wayle Go shell: CGO off, $(go version | cut -d' ' -f3)"
              '';
          };
        in
        {
          inherit default;

          # `nix develop .#go` — the Go rewrite shell: pinned toolchain,
          # lint/format stack, fonts, and a headless-capable sway. See
          # GO-REWRITE.md.
          go = goShell;

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
