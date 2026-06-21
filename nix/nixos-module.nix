self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.wayle;
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
      systemd.user.services.xdg-desktop-portal-wayle = {
        description = "Wayle xdg-desktop-portal backend";
        partOf = [ "graphical-session.target" ];
        after = [
          "graphical-session.target"
          "xdg-desktop-portal.service"
        ];
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
