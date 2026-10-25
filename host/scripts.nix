# Creates the programs of the host module from the supervisor (./supervisor, Go) and the
# shell scripts in ./scripts.
#
# The programs contain no Nix code. Each program here is a small wrapper script. It puts the
# options of services.sandbox-vm in environment variables. Then it starts the supervisor or
# a shell script. scripts/lib.sh describes the variables.
{
  pkgs,
  lib,
  cfg,
  root,
  constants,
  platform,
  proxy,
}:
let
  # The supervisor. It also puts borg in a sandbox for the backups.
  supervisorPackage = pkgs.callPackage ./supervisor.nix { };
  supervisor = lib.getExe supervisorPackage;

  # The store paths that a program in a sandbox uses, one path in each line. Its sandbox
  # permits only these paths of the Nix store.
  storePaths = roots: "${pkgs.closureInfo { rootPaths = roots; }}/store-paths";

  # One line for each forward. scripts/ports.sh describes the fields.
  forwards = lib.concatStrings (
    lib.mapAttrsToList (
      name: p:
      "L ${name} 127.0.0.1:${toString p.host}:127.0.0.1:${toString p.guest} ${toString p.host}"
      + " 127.0.0.1:${toString p.host} -> port ${toString p.guest} in the VM\n"
    ) cfg.expose
    ++ lib.mapAttrsToList (
      name: r:
      "R ${name} 127.0.0.1:${toString r.guest}:${r.host}:${toString r.port} -"
      + " port ${toString r.guest} in the VM -> ${r.host}:${toString r.port}\n"
    ) cfg.reach
  );

  # The command in the guest that writes the backup as a tar stream. tar exits with status 1
  # when a file changed while tar read it. That is normal in a running system, and it is not
  # a failure.
  backupCommand =
    "sudo tar -c --one-file-system --numeric-owner "
    + lib.concatMapStrings (e: lib.escapeShellArg "--exclude=${e}" + " ") cfg.backup.exclude
    + "-C / -- "
    + lib.escapeShellArgs (
      map (p: if p == "/" then "." else lib.removePrefix "/" p) cfg.backup.paths
    )
    + " || [ $? = 1 ]";

  # The variables for the programs that apply the home-manager configuration.
  homeEnv = {
    SVM_HOME_MANAGER = lib.optionalString (cfg.homeManager != null) cfg.homeManager;
  };

  # The variables for all programs.
  commonEnv = {
    SVM_NAME = cfg.name;
    SVM_HOSTNAME = cfg.hostName;
    SVM_USER = if cfg.user == null then "" else cfg.user;
    SVM_ROOT = root;
    SVM_CONFIG_DIR = cfg.configDir;
    SVM_CONFIG_NAME = cfg.configName;
    SVM_CPUS = cfg.cpus;
    SVM_MEMORY_MIB = cfg.memory * 1024;
    SVM_DISK_GB = cfg.disk;
    SVM_LOG_BYTES = cfg.logSize * 1024 * 1024;
    SVM_FORWARDS = forwards;
    SVM_GUEST_MARKER = constants.guestMarker;
    SVM_GUEST_STATE = constants.guestStateDir;
    SVM_GUEST_MAC = constants.guestMac;
    SVM_PROXY_GATEWAY = constants.proxyGateway;
    SVM_TIMESYNC_PORT = constants.timesyncPort;
  }
  # Empty on a host that is not supported and in the guest. Then the programs exit at their
  # start.
  // lib.optionalAttrs platform.supported {
    SVM_SUPERVISOR = supervisor;
    # The console of the VM goes through this program to s6-log, and the output of the
    # commands of the guest goes through it to the terminal.
    SVM_CONSOLE_FILTER = "${supervisorPackage}/bin/console-filter";
  };

  # On a host that is not supported, the programs exit before they use these variables. Thus
  # the packages are not necessary there. This is also true for the clone in the guest.
  vmEnv = lib.optionalAttrs platform.supported {
    # The installer ISO that a new VM boots one time. Empty: the host builds a disk image of
    # the system on a Linux builder.
    SVM_INSTALLER_ISO = lib.optionalString (cfg.installer.iso != null) cfg.installer.iso;
    SVM_GUEST_ARCH = platform.guestArch;
    # The binary caches, for the build of the disk image on the host.
    SVM_SUBSTITUTERS = lib.concatStringsSep " " constants.substituters;
    SVM_TRUSTED_PUBLIC_KEYS = lib.concatStringsSep " " constants.trustedPublicKeys;
    # The same for nixos-install in the installer.
    SVM_NIX_OPTIONS = lib.concatStringsSep " " [
      ''--option extra-substituters "${lib.concatStringsSep " " constants.substituters}"''
      ''--option extra-trusted-public-keys "${lib.concatStringsSep " " constants.trustedPublicKeys}"''
    ];
    SVM_SMOKESCREEN = lib.getExe proxy.package;
    SVM_SMOKESCREEN_CONFIG = "${proxy.config}";
    SVM_SMOKESCREEN_STORE_PATHS = storePaths [
      proxy.package
      proxy.config
    ];
    SVM_GVPROXY = "${pkgs.gvproxy}/bin/gvproxy";
    SVM_GVPROXY_STORE_PATHS = storePaths [ pkgs.gvproxy ];
    SVM_S6_LOG = "${pkgs.s6}/bin/s6-log";
    SVM_S6_LOG_STORE_PATHS = storePaths [ pkgs.s6 ];
    SVM_CONSOLE_FILTER_STORE_PATHS = storePaths [ supervisorPackage ];
    SVM_PORTS = "${ports}";
    # The program that applies the home-manager configuration to a new VM. Empty: none.
    SVM_HOME_PROGRAM = lib.optionalString (cfg.homeManager != null) "${home}";
    # krunkit finds its EFI firmware in its store path.
    SVM_KRUNKIT = "${platform.krunkit}/bin/krunkit";
    SVM_KRUNKIT_STORE_PATHS = storePaths [ platform.krunkit ];
  };

  backupEnv = lib.optionalAttrs platform.supported {
    SVM_BORG = "${pkgs.borgbackup}/bin/borg";
    SVM_BORG_STORE_PATHS = storePaths [ pkgs.borgbackup ];
    SVM_BACKUP_REPO = if cfg.backup.repository == null then "" else cfg.backup.repository;
    SVM_BACKUP_COMMAND = backupCommand;
    SVM_BACKUP_MAX_BYTES =
      (if cfg.backup.maxSize == null then cfg.disk else cfg.backup.maxSize) * 1024 * 1024 * 1024;
    SVM_BACKUP_COMPRESSION = cfg.backup.compression;
    SVM_BACKUP_KEEP = lib.concatStringsSep " " (
      lib.mapAttrsToList (n: v: "--keep-${n} ${toString v}") (
        lib.filterAttrs (_: v: v > 0) cfg.backup.keep
      )
    );
    SVM_BACKUP_INTERVAL_HOURS = cfg.backup.interval;
  };

  # A program is a file and not inline text in a process definition: process-compose
  # replaces $VAR in its configuration and cannot parse shell expansions.
  program =
    {
      name,
      # A shell script in ./scripts, or a list: a program and its arguments.
      script,
      env ? { },
      runtimeInputs ? [ ],
    }:
    pkgs.writeShellScript "devenv-sandbox-vm${lib.optionalString (name != "") "-${name}"}" ''
      ${lib.concatStrings (
        lib.mapAttrsToList (n: v: "export ${n}=${lib.escapeShellArg (toString v)}\n") (commonEnv // env)
      )}
      ${lib.optionalString (runtimeInputs != [ ]) "PATH=${lib.makeBinPath runtimeInputs}:$PATH"}
      exec ${
        if lib.isList script then
          lib.escapeShellArgs script
        else
          "${pkgs.runtimeShell} ${./scripts}/${script}"
      } "$@"
    '';

  # The sandbox-vm:home task. The sandbox-vm process starts it after it creates a VM.
  home = program {
    name = "home";
    script = "home.sh";
    env = homeEnv;
  };

  # The sandbox-vm:ports task. The sandbox-vm process also starts it.
  ports = program {
    name = "ports";
    script = "ports.sh";
  };
in
{
  inherit ports home;
  # The sandbox-vm process.
  vm = program {
    name = "";
    script =
      if platform.supported then
        [
          supervisor
          "run"
        ]
      else
        "host-only.sh";
    env = vmEnv;
    # ssh, ssh-keygen, nix and pkill are the programs of the host: they are not in
    # this list.
    runtimeInputs = lib.optionals platform.supported (platform.runtimeInputs ++ [ pkgs.git ]);
  };
  # The probe of the sandbox-vm process.
  ready = program {
    name = "ready";
    script = "ready.sh";
  };
  # The sandbox-vm:update task.
  update = program {
    name = "update";
    script = "update.sh";
    env = homeEnv;
    runtimeInputs = [ pkgs.git ];
  };
  # The sandbox-vm:backup task.
  backup = program {
    name = "backup";
    script = "backup.sh";
    env = backupEnv;
    runtimeInputs = [ pkgs.coreutils ];
  };
  # The sandbox-vm-backup process.
  backupLoop = program {
    name = "backup-loop";
    script = "backup-loop.sh";
    env = backupEnv;
    runtimeInputs = [
      pkgs.coreutils
      pkgs.findutils
    ];
  };
}
