# The host module: services.sandbox-vm. A project imports this directory through devenv.yaml.
#
#   options.nix    the options
#   proxy.nix      the configuration of the proxy
#   scripts.nix    the programs, created from the supervisor (supervisor/) and the shell
#                  scripts in scripts/
#   platform/      the parts that depend on the operating system of the host
{
  pkgs,
  lib,
  config,
  ...
}:
let
  cfg = config.services.sandbox-vm;

  constants = import ../constants.nix;
  # The documentation of the processes and tasks.
  commands = import ./commands.nix;
  # The first sentence of the documentation of a task, for `devenv tasks list`.
  description = name: builtins.head (lib.splitString ". " commands.tasks.${name}) + ".";
  platform = import ./platform { inherit pkgs; };
  proxy = import ./proxy.nix { inherit pkgs lib cfg; };
  scripts = import ./scripts.nix {
    inherit
      pkgs
      lib
      cfg
      constants
      platform
      proxy
      ;
    root = config.devenv.root;
  };
in
{
  imports = [ ./options.nix ];

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = lib.allUnique (lib.mapAttrsToList (_: p: p.host) cfg.expose);
        message = "services.sandbox-vm.expose: two entries use the same port on the host.";
      }
      {
        assertion = lib.allUnique (lib.mapAttrsToList (_: r: r.guest) cfg.reach);
        message = "services.sandbox-vm.reach: two entries use the same port in the VM.";
      }
      {
        # The programs read a forward as fields with spaces between them (scripts/ports.sh).
        assertion = lib.all (name: builtins.match "[A-Za-z0-9_.-]+" name != null) (
          lib.attrNames cfg.expose ++ lib.attrNames cfg.reach
        );
        message = "services.sandbox-vm: a name in expose or reach can have only letters, digits, `.`, `_` and `-`.";
      }
      {
        assertion = !lib.elem ".." (lib.splitString "/" cfg.configDir);
        message = "services.sandbox-vm.configDir must be a directory in the repo, without `..`.";
      }
    ];

    # commands.nix describes each process and task.
    tasks."sandbox-vm:ports" = {
      description = description "sandbox-vm:ports";
      exec = "${scripts.ports}";
      before = [ "devenv:enterShell" ];
    };

    tasks."sandbox-vm:update" = {
      description = description "sandbox-vm:update";
      exec = "${scripts.update}";
    };

    tasks."sandbox-vm:home" = {
      description = description "sandbox-vm:home";
      exec = "${scripts.home}";
    };

    tasks."sandbox-vm:backup" = lib.mkIf cfg.backup.enable {
      description = description "sandbox-vm:backup";
      exec = "${scripts.backup}";
    };
    # borg in the shell, to list and restore backups.
    packages = lib.optional cfg.backup.enable pkgs.borgbackup;
    # The automatic backups. The process is not a part of the sandbox-vm process, and
    # `devenv up` without a name does not start it. It waits for the VM, but it does not start
    # the VM: start the two processes together, `devenv up sandbox-vm sandbox-vm-backup`.
    processes.sandbox-vm-backup = lib.mkIf cfg.backup.enable {
      exec = "${scripts.backupLoop}";
      start.enable = false;
      # The process exits only on a computer where it cannot run.
      restart.on = "never";
      after = [ "devenv:processes:sandbox-vm" ];
    };

    # `devenv up` without a name does not start this process, because that command also
    # runs the project in the VM. On the host, start the process with
    # `devenv up sandbox-vm`.
    processes.sandbox-vm.start.enable = false;
    # If a VM fails to install or start, a second try fails in the same way.
    processes.sandbox-vm.restart.on = "never";
    # The time for the guest to shut down. After this time devenv kills the process, and then
    # the VM continues to run without it. The process powers off a guest that does not
    # shut down in a shorter time (shutdownTimeout in supervisor/run.go).
    processes.sandbox-vm.shutdown.grace = 60;
    # The probe checks the file `ready` that the process creates (scripts/ready.sh).
    processes.sandbox-vm.ready.exec = "${scripts.ready}";
    # The installation of a new VM takes minutes. process-compose stops a process when its
    # probe fails this number of times in sequence. This value prevents that.
    processes.sandbox-vm.ready.period = 2;
    processes.sandbox-vm.ready.failure_threshold = 1000000;

    processes.sandbox-vm.exec = "${scripts.vm}";
  };
}
