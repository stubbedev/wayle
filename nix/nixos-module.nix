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
          Register wayle as an xdg-desktop-portal backend so screen sharing,
          screenshots, remote control, global shortcuts, appearance settings,
          notifications, wallpaper and idle-inhibit work on any Wayland
          compositor. Enables `xdg.portal`, adds wayle (and
          xdg-desktop-portal-gtk for the generic file/print/account dialogs) to
          `extraPortals`, routes the interfaces in `xdg.portal.config.common`
          (with `mkDefault`, so your own routing wins), and defines the
          `xdg-desktop-portal-wayle` user service the frontend activates.
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
        # wayle provides the compositor-specific interfaces; gtk handles the
        # generic file/print/account/appchooser dialogs wayle delegates to it.
        extraPortals = [
          cfg.package
          pkgs.xdg-desktop-portal-gtk
        ];
        # Route each interface regardless of XDG_CURRENT_DESKTOP. mkDefault so a
        # user's own xdg.portal.config wins.
        config.common = lib.mkDefault {
          default = [
            "wayle"
            "gtk"
          ];
          "org.freedesktop.impl.portal.ScreenCast" = [ "wayle" ];
          "org.freedesktop.impl.portal.RemoteDesktop" = [ "wayle" ];
          "org.freedesktop.impl.portal.Screenshot" = [ "wayle" ];
          "org.freedesktop.impl.portal.GlobalShortcuts" = [ "wayle" ];
          "org.freedesktop.impl.portal.Settings" = [ "wayle" ];
          "org.freedesktop.impl.portal.Notification" = [ "wayle" ];
          "org.freedesktop.impl.portal.Wallpaper" = [ "wayle" ];
          "org.freedesktop.impl.portal.Inhibit" = [ "wayle" ];
          "org.freedesktop.impl.portal.Lockdown" = [ "wayle" ];
          "org.freedesktop.impl.portal.Background" = [ "wayle" ];
          "org.freedesktop.impl.portal.Usb" = [ "wayle" ];
          "org.freedesktop.impl.portal.Clipboard" = [ "wayle" ];
          "org.freedesktop.impl.portal.InputCapture" = [ "wayle" ];
          "org.freedesktop.impl.portal.FileChooser" = [ "wayle" ];
          "org.freedesktop.impl.portal.Email" = [ "wayle" ];
          "org.freedesktop.impl.portal.AppChooser" = [ "gtk" ];
          "org.freedesktop.impl.portal.Print" = [ "gtk" ];
          "org.freedesktop.impl.portal.Account" = [ "gtk" ];
          "org.freedesktop.impl.portal.Access" = [ "gtk" ];
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
