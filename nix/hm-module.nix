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

  # A self-contained launch wrapper for the greeter: `cage -s -- wayle-greeter`
  # with the render env baked in, passing any extra args straight through. Point
  # your (non-NixOS) greetd at this one path — or, for GPU acceleration, wrap it
  # with nixGL: `nixGLIntel wayle-greeter-session --config /etc/wayle/config.toml
  # --state /var/lib/wayle-greeter/last-session`. `renderer = "software"` bakes
  # the driver-free pixman/cairo path so it runs without nixGL at all.
  # cage's own cursor (shown until the greeter maps) follows XCURSOR_*; mirror
  # the greeter's cursor config so both cursors match.
  greeterCursorEnv =
    "XCURSOR_SIZE=${toString (cfg.greeter.settings.greeter.cursor-size or 24)}"
    + lib.optionalString (
      (cfg.greeter.settings.greeter.cursor-theme or "") != ""
    ) " XCURSOR_THEME=${cfg.greeter.settings.greeter.cursor-theme}";

  greeterSession = pkgs.writeShellScriptBin "wayle-greeter-session" ''
    exec env ${greeterCursorEnv} ${lib.optionalString (cfg.greeter.renderer == "software")
      "WLR_RENDERER=pixman WLR_DRM_NO_MODIFIERS=1 GSK_RENDERER=cairo GDK_DISABLE=gl "
    }${lib.getExe cfg.greeter.cagePackage} -s -- \
      ${lib.getExe' cfg.package "wayle-greeter"} "$@"
  '';

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

    portal = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = ''
          Install wayle as the xdg-desktop-portal backend at the user level: the
          `.portal` interface declaration, the D-Bus activation file, a
          `xdg-desktop-portal-wayle` user service, and a generic `portals.conf`
          routing every interface to wayle (it implements them all natively — no
          xdg-desktop-portal-gtk).

          Off by default: on NixOS use the system module's
          `programs.wayle.portal.enable` instead (it routes via
          `xdg.portal.config` without writing a generic `portals.conf`). Enable
          this for non-NixOS / standalone home-manager setups.
        '';
      };
    };

    lock = {
      enable = lib.mkEnableOption ''
        guidance for the wayle session lock's PAM service. Wayle locks the
        session natively via `ext-session-lock-v1`; authenticating the unlock
        needs a PAM service, which home-manager cannot create (PAM lives under
        {file}`/etc/pam.d`, root-owned). Enabling this emits a warning with
        instructions. On NixOS use the system module's `programs.wayle.lock`,
        which provisions the service for you'';

      pamService = lib.mkOption {
        type = lib.types.str;
        default = "wayle";
        description = ''
          Name of the PAM service the unlock authenticates against. Must match
          `lock.pam-service` in your wayle config. The config default is
          `system-auth` (exists on Arch/Fedora); on other distros create
          {file}`/etc/pam.d/<this name>` and point the config at it.
        '';
      };
    };

    greeter = {
      enable = lib.mkEnableOption ''
        wayle greeter tooling: installs the kiosk compositor (cage) and writes a
        themed greeter config. NOTE: greetd is a system service home-manager
        cannot manage — on NixOS use the system module's
        `programs.wayle.greeter`; elsewhere configure greetd yourself'';

      cagePackage = lib.mkOption {
        type = lib.types.package;
        default = pkgs.cage;
        defaultText = lib.literalExpression "pkgs.cage";
        description = "Kiosk compositor that hosts the greeter under greetd.";
      };

      renderer = lib.mkOption {
        type = lib.types.enum [
          "auto"
          "software"
        ];
        default = "auto";
        description = ''
          Render path baked into the {command}`wayle-greeter-session` wrapper.
          `auto` sets nothing (cage's GLES2 and GTK's GL renderer each fall back
          to software on their own); `software` forces the driver-free pixman +
          cairo path, so the wrapper runs without nixGL on a non-NixOS host. Use
          `auto` together with a nixGL wrapper when you want GPU acceleration.
        '';
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
          Theme/background/clock config written to
          {file}`$XDG_CONFIG_HOME/wayle/greeter.toml`. The greeter runs as the
          greetd user, so copy this somewhere that user can read (e.g.
          {file}`/etc/wayle/config.toml`) and point greetd's command at it:
          {command}`cage -s -- wayle-greeter --config /etc/wayle/config.toml --state /var/lib/wayle-greeter/last-session`.
          The greeter discovers Wayland sessions from the standard
          {file}`wayland-sessions` dirs; add `--sessions DIR` to override.
        '';
      };
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages =
      [ cfg.package ]
      ++ lib.optionals cfg.autoInstallDependencies softDeps
      ++ lib.optionals cfg.greeter.enable [
        cfg.greeter.cagePackage
        greeterSession
      ];

    # greetd is system-level; home-manager can only ship the binary + cage and
    # generate a themed config to copy somewhere the greetd user can read.
    warnings =
      lib.optional cfg.greeter.enable ''
        programs.wayle.greeter (home-manager) installs cage + the
        `wayle-greeter-session` wrapper, but cannot configure greetd (a system
        service). On NixOS use programs.wayle.greeter in the system module;
        otherwise enable greetd yourself and point its command at the wrapper:
          wayle-greeter-session --config /etc/wayle/config.toml --state /var/lib/wayle-greeter/last-session
        For GPU acceleration on a non-NixOS host, wrap it with nixGL:
          nixGLIntel wayle-greeter-session --config /etc/wayle/config.toml --state /var/lib/wayle-greeter/last-session
        (or set greeter.renderer = "software" to run driver-free without nixGL).
        The greeter discovers Wayland sessions from the standard
        wayland-sessions dirs and remembers the last pick under --state (make
        that dir writable by the greetd user). It runs as the greetd user, so
        place the config where that user can read it (e.g. /etc/wayle/config.toml).
      ''
      # PAM is root-owned (/etc/pam.d); home-manager cannot create it. Point the
      # consumer at the manual step the system module does automatically.
      ++ lib.optional cfg.lock.enable ''
        programs.wayle.lock (home-manager) cannot create the PAM service the
        unlock authenticates against (/etc/pam.d is root-owned). Create
        /etc/pam.d/${cfg.lock.pamService} yourself — this stack authenticates
        the unlock AND unlocks the GNOME `login` keyring (pam_unix must be
        `required`, not `sufficient`: a sufficient pass short-circuits the
        stack and skips the keyring hook below it; keyring lines are
        `optional`, so without gnome-keyring installed they no-op):
          auth     required   pam_unix.so
          auth     optional   pam_gnome_keyring.so
          account  required   pam_unix.so
          password required   pam_unix.so
          password optional   pam_gnome_keyring.so use_authtok
          session  required   pam_unix.so
          session  optional   pam_gnome_keyring.so auto_start
        (pam_gnome_keyring.so is packaged as libpam-gnome-keyring on
        Debian/Ubuntu, bundled with gnome-keyring on Arch/Fedora.)
        — and set `lock.pam-service = "${cfg.lock.pamService}"` in your wayle
        config. On Arch/Fedora the config default `system-auth` already exists;
        set lock.pam-service to that instead and skip this. On NixOS use the
        system module's programs.wayle.lock, which provisions it for you.
      '';

    xdg.configFile."wayle/greeter.toml" = lib.mkIf (cfg.greeter.enable && cfg.greeter.settings != { }) {
      source = tomlFormat.generate "wayle-greeter-config.toml" cfg.greeter.settings;
    };

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

    # User-level xdg-desktop-portal backend: discovery (.portal), D-Bus
    # activation, routing, and the activated service. The frontend reads these
    # from $XDG_DATA_HOME / $XDG_CONFIG_HOME. Per-key form so these merge with
    # the `wayle/config.toml` entry above.
    xdg.dataFile."xdg-desktop-portal/portals/wayle.portal" = lib.mkIf cfg.portal.enable {
      source = "${cfg.package}/share/xdg-desktop-portal/portals/wayle.portal";
    };
    xdg.dataFile."dbus-1/services/org.freedesktop.impl.portal.desktop.wayle.service" =
      lib.mkIf cfg.portal.enable {
        source = "${cfg.package}/share/dbus-1/services/org.freedesktop.impl.portal.desktop.wayle.service";
      };
    # Generic fallback config (read when no <desktop>-portals.conf matches).
    xdg.configFile."xdg-desktop-portal/portals.conf" = lib.mkIf cfg.portal.enable {
      source = "${cfg.package}/share/wayle/wayle-portals.conf";
    };

    systemd.user.services.xdg-desktop-portal-wayle = lib.mkIf cfg.portal.enable {
      Unit = {
        Description = "Wayle xdg-desktop-portal backend";
        PartOf = [ cfg.systemd.target ];
        # MUST NOT be After/Requires xdg-desktop-portal.service: the frontend
        # activates this backend (StartServiceByName) while still activating
        # itself, so ordering the backend after the frontend deadlocks both
        # until 25s D-Bus timeouts break it and the frontend fails. The session
        # target ordering is enough.
        After = [ cfg.systemd.target ];
      };
      # D-Bus-activated by the frontend via the .portal's DBusName; no WantedBy.
      Service = {
        Type = "dbus";
        BusName = "org.freedesktop.impl.portal.desktop.wayle";
        ExecStart = "${lib.getExe cfg.package} portal";
        Restart = "on-failure";
        RestartSec = 3;
        Slice = "session.slice";
      };
    };
  };
}
