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

  # Modules referenced anywhere in the bar layout. Used to install the soft
  # dependencies a module shells out to only when that module is actually used.
  layoutZones = cfg.settings.bar.layout or [ ];
  layoutModules = lib.concatMap (
    zone: (zone.left or [ ]) ++ (zone.center or [ ]) ++ (zone.right or [ ])
  ) layoutZones;
  usesMail = lib.elem "mail" layoutModules;

  # Soft dependencies wayle shells out to, selected from the config. Theme
  # providers are spawned for wallpaper-driven color extraction; wl-clipboard
  # and xdg-utils back copy / open-link actions; notmuch backs the mail module.
  # Missing packages are skipped.
  themeProvider = cfg.settings.styling.theme-provider or "wayle";
  softDeps =
    pkgIfPresent "wl-clipboard"
    ++ pkgIfPresent "xdg-utils"
    ++ lib.optionals (themeProvider == "matugen") (pkgIfPresent "matugen")
    ++ lib.optionals (themeProvider == "wallust") (pkgIfPresent "wallust")
    ++ lib.optionals (themeProvider == "pywal") (pkgIfPresent "pywal")
    ++ lib.optionals usesMail (pkgIfPresent "notmuch");
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
        (theme-provider tools like matugen/wallust/pywal, wl-clipboard and
        xdg-utils, plus notmuch when the `mail` module is in the layout).
        System-level dependencies (NetworkManager, bluez, upower,
        power-profiles-daemon, GeoClue for the hyprsunset auto-schedule,
        pipewire/wireplumber) are not installed by home-manager — enable those
        at the NixOS level.
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
        # The recorder captures via the xdg-desktop-portal ScreenCast interface
        # over PipeWire; order after (and weakly want) both so they're up when
        # the shell starts. Wants is weak: a missing unit just logs, never
        # blocks the shell.
        After = [
          cfg.systemd.target
          "pipewire.service"
          "xdg-desktop-portal.service"
        ];
        Wants = [
          "pipewire.service"
          "xdg-desktop-portal.service"
        ];
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
