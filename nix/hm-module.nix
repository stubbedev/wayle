self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.wayle;
  tomlFormat = pkgs.formats.toml { };

  pkgIfPresent = name: lib.optional (builtins.hasAttr name pkgs) (builtins.getAttr name pkgs);

  # Soft dependencies wayle shells out to, selected from the config. Theme
  # providers are spawned for wallpaper-driven color extraction; wl-clipboard
  # and xdg-utils back copy / open-link actions. Missing packages are skipped.
  themeProvider = cfg.settings.styling.theme-provider or "wayle";
  softDeps =
    pkgIfPresent "wl-clipboard"
    ++ pkgIfPresent "xdg-utils"
    ++ lib.optionals (themeProvider == "matugen") (pkgIfPresent "matugen")
    ++ lib.optionals (themeProvider == "wallust") (pkgIfPresent "wallust")
    ++ lib.optionals (themeProvider == "pywal") (pkgIfPresent "pywal");
in
{
  options.programs.wayle = {
    enable = lib.mkEnableOption "the Wayle desktop shell";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.wayle;
      defaultText = lib.literalExpression "wayle.packages.\${system}.wayle";
      description = "The wayle package to install.";
    };

    autoInstallDependencies = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Install the user-space soft dependencies wayle's config implies
        (theme-provider tools like matugen/wallust/pywal, plus wl-clipboard and
        xdg-utils). System-level dependencies (NetworkManager, bluez, upower,
        power-profiles-daemon, pipewire/wireplumber) are not installed by
        home-manager — enable those at the NixOS level.
      '';
    };

    settings = lib.mkOption {
      type = tomlFormat.type;
      default = { };
      example = lib.literalExpression ''
        {
          bar.layout = [
            {
              monitor = "*";
              left = [ "dashboard" ];
              center = [ "clock" ];
              right = [ "battery" "network" "volume" "systray" ];
            }
          ];
          styling.appearance = "dark";
        }
      '';
      description = ''
        Wayle configuration written to
        {file}`$XDG_CONFIG_HOME/wayle/config.toml`. Leave empty to manage the
        file yourself.
      '';
    };

    systemd = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          Run `wayle shell` from a systemd user service bound to the graphical
          session. Disable if you start the bar from your compositor config.
        '';
      };

      target = lib.mkOption {
        type = lib.types.str;
        default = "graphical-session.target";
        description = "Systemd target the wayle user service binds to.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ] ++ lib.optionals cfg.autoInstallDependencies softDeps;

    xdg.configFile."wayle/config.toml" = lib.mkIf (cfg.settings != { }) {
      source = tomlFormat.generate "wayle-config.toml" cfg.settings;
    };

    systemd.user.services.wayle = lib.mkIf cfg.systemd.enable {
      Unit = {
        Description = "Wayle desktop shell";
        PartOf = [ cfg.systemd.target ];
        After = [ cfg.systemd.target ];
      };
      Service = {
        ExecStart = "${lib.getExe cfg.package} shell";
        Restart = "on-failure";
        RestartSec = 3;
        Slice = "session.slice";
      };
      Install.WantedBy = [ cfg.systemd.target ];
    };
  };
}
