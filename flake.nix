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
      devShells = forAllSystems (pkgs: {
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
          buildInputs = with pkgs; [
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
          ];

          # bindgen (via the cava build script) needs libclang at runtime.
          LIBCLANG_PATH = "${pkgs.llvmPackages.libclang.lib}/lib";
        };
      });
    };
}
