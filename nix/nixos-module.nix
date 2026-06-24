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

  # The greetd `default_session` command: a kiosk compositor (cage) hosting the
  # wayle greeter, which on login starts the configured session. cage's `-s`
  # allows VT switching. The greeter reads its theme from /etc/wayle/config.toml
  # (written below from `greeter.settings`).
  greeterCommand =
    let
      cageExe = lib.getExe cfg.greeter.cagePackage;
      greeterExe = lib.getExe' cfg.greeter.package "wayle-greeter";
      envArgs = lib.concatMapStringsSep " " (
        e: "--env ${lib.escapeShellArg e}"
      ) cfg.greeter.session.environment;
    in
    "${cageExe} -s -- ${greeterExe} --config /etc/wayle/config.toml ${envArgs} -- ${cfg.greeter.session.command}";
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
        Enable the backing system services wayle integrates with: UPower
        (battery), BlueZ (bluetooth), and GeoClue (location for the hyprsunset
        auto-schedule), plus wl-clipboard and xdg-utils. Set with `mkDefault` so
        your own settings win. Networking (NetworkManager),
        power-profiles-daemon, and PipeWire are intentionally left out — enable
        those deliberately to avoid clashing with your setup.
      '';
    };

    systemd = {
      enable = lib.mkEnableOption ''
        a systemd user service that runs `wayle shell` in the graphical session.
        Leave this off if you start the bar from your compositor config instead'';

      target = lib.mkOption {
        type = lib.types.str;
        default = "graphical-session.target";
        description = "Systemd target the wayle user service binds to.";
      };
    };

    portal = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          Register wayle as the xdg-desktop-portal backend so screen sharing,
          screenshots, remote control, global shortcuts, appearance settings,
          notifications, wallpaper, idle-inhibit, file/app/print dialogs and the
          rest work on any Wayland compositor. wayle implements every interface
          natively, so this enables `xdg.portal`, adds only wayle to
          `extraPortals`, routes everything to it via `xdg.portal.config.common`
          (with `mkDefault`, so your own routing wins), and defines the
          `xdg-desktop-portal-wayle` user service the frontend activates. No
          xdg-desktop-portal-gtk needed.
        '';
      };
    };

    greeter = {
      enable = lib.mkEnableOption ''
        the wayle greetd greeter: a login screen that shares the desktop/lock
        theme. Enables `services.greetd` with a kiosk compositor (cage) hosting
        the greeter; on login it starts `greeter.session.command`'';

      package = lib.mkOption {
        type = lib.types.package;
        default = cfg.package;
        defaultText = lib.literalExpression "config.programs.wayle.package";
        description = "Package providing the `wayle-greeter` binary.";
      };

      cagePackage = lib.mkOption {
        type = lib.types.package;
        default = pkgs.cage;
        defaultText = lib.literalExpression "pkgs.cage";
        description = "Kiosk compositor that hosts the greeter under greetd.";
      };

      session = {
        command = lib.mkOption {
          type = lib.types.str;
          default = "";
          example = lib.literalExpression ''"''${pkgs.niri}/bin/niri --session"'';
          description = ''
            Command greetd starts as the user's session after a successful
            login (typically the compositor). Required when the greeter is
            enabled.
          '';
        };

        environment = lib.mkOption {
          type = lib.types.listOf lib.types.str;
          default = [ ];
          example = [ "XDG_SESSION_TYPE=wayland" ];
          description = "Extra `KEY=value` environment entries for the session.";
        };
      };

      settings = lib.mkOption {
        type = tomlFormat.type;
        default = { };
        example = lib.literalExpression ''
          {
            styling.appearance = "dark";
            lock.background-mode = "color";
            lock.background-color = "#1e1e2e";
          }
        '';
        description = ''
          Theme/background/clock config written to {file}`/etc/wayle/config.toml`
          and read by the greeter. Uses the full wayle config schema; the
          greeter honours `styling`, `lock.*` (background + clock), and
          `wallpaper`. Leave empty to use the built-in defaults.
        '';
      };
    };
  };

  config = lib.mkIf cfg.enable (lib.mkMerge [
    {
      environment.systemPackages = [ cfg.package ];

      systemd.user.services.wayle = lib.mkIf cfg.systemd.enable {
        description = "Wayle desktop shell";
        partOf = [ cfg.systemd.target ];
        # The recorder captures via the xdg-desktop-portal ScreenCast interface
        # over PipeWire; order after (and weakly want) both so they're up when
        # the shell starts. Wants is weak: a missing unit just logs.
        after = [
          cfg.systemd.target
          "pipewire.service"
          "xdg-desktop-portal.service"
        ];
        wants = [
          "pipewire.service"
          "xdg-desktop-portal.service"
        ];
        wantedBy = [ cfg.systemd.target ];
        serviceConfig = {
          ExecStart = "${lib.getExe cfg.package} shell";
          Restart = "on-failure";
          RestartSec = 3;
          Slice = "session.slice";
        };
      };
    }

    (lib.mkIf cfg.portal.enable {
      xdg.portal = {
        enable = true;
        # Wayle implements every interface natively — no xdg-desktop-portal-gtk.
        extraPortals = [ cfg.package ];
        # Route everything to wayle regardless of XDG_CURRENT_DESKTOP. mkDefault
        # so a user's own xdg.portal.config wins.
        config.common = lib.mkDefault {
          default = [ "wayle" ];
        };
      };

      # The frontend D-Bus-activates this via the .portal's DBusName /
      # SystemdService; it is not wantedBy the session (started on demand).
      #
      # MUST NOT be After/Requires xdg-desktop-portal.service: the frontend
      # activates this backend (StartServiceByName) while it is still itself
      # activating, so ordering the backend after the frontend deadlocks both
      # — the frontend blocks on the backend's interfaces, the backend blocks
      # on the frontend finishing — until 25s D-Bus timeouts break it and the
      # frontend fails. graphical-session.target ordering is enough.
      systemd.user.services.xdg-desktop-portal-wayle = {
        description = "Wayle xdg-desktop-portal backend";
        partOf = [ "graphical-session.target" ];
        after = [ "graphical-session.target" ];
        serviceConfig = {
          Type = "dbus";
          BusName = "org.freedesktop.impl.portal.desktop.wayle";
          ExecStart = "${lib.getExe cfg.package} portal";
          Restart = "on-failure";
          RestartSec = 3;
          Slice = "session.slice";
        };
      };
    })

    (lib.mkIf cfg.greeter.enable {
      assertions = [
        {
          assertion = cfg.greeter.session.command != "";
          message = "programs.wayle.greeter.session.command must be set (the session greetd starts after login).";
        }
      ];

      # greetd runs the kiosk compositor + greeter as the unprivileged greeter
      # user. mkDefault so an explicit greetd config wins.
      services.greetd = {
        enable = true;
        settings.default_session.command = lib.mkDefault greeterCommand;
      };

      # The greeter runs as the greetd user with no $HOME, so its theme config
      # lives system-wide. environment.etc files are world-readable (0444).
      environment.etc."wayle/config.toml" = lib.mkIf (cfg.greeter.settings != { }) {
        source = tomlFormat.generate "wayle-greeter-config.toml" cfg.greeter.settings;
      };

      environment.systemPackages = [ cfg.greeter.cagePackage ];
    })

    (lib.mkIf cfg.autoInstallDependencies {
      services.upower.enable = lib.mkDefault true;
      hardware.bluetooth.enable = lib.mkDefault true;

      # GeoClue backs the hyprsunset auto-schedule's location lookup. The
      # appConfig entry whitelists wayle (DesktopId "wayle") so the daemon
      # serves it without an agent prompt; isSystem skips the per-user agent.
      # If GeoClue is unavailable the schedule falls back to the configured
      # latitude/longitude, so this is purely a convenience.
      services.geoclue2 = {
        enable = lib.mkDefault true;
        appConfig.wayle = {
          desktopID = "wayle";
          isAllowed = true;
          isSystem = true;
        };
      };

      environment.systemPackages =
        lib.optional (pkgs ? wl-clipboard) pkgs.wl-clipboard
        ++ lib.optional (pkgs ? xdg-utils) pkgs.xdg-utils;
    })
  ]);
}
