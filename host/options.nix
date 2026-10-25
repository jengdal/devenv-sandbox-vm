# The options of services.sandbox-vm.
{
  pkgs,
  lib,
  config,
  ...
}:
let
  inherit (lib) mkOption mkEnableOption types;

  project = baseNameOf config.devenv.root;
  yaml = pkgs.formats.yaml { };

  # The project name as a valid hostname: only [a-z0-9-], no "-" at the start or the end,
  # a maximum of 63 characters.
  toHostName =
    name:
    let
      clean = lib.stringAsChars (c: if builtins.match "[a-z0-9-]" c != null then c else "-") (
        lib.toLower name
      );
      trim =
        s:
        let
          m = builtins.match "-*(.*[^-])-*" s;
        in
        if m == null then "" else builtins.head m;
    in
    trim (builtins.substring 0 63 (trim clean));

  # The project name as a valid VM name: each character that is not permitted becomes "-".
  toName = lib.stringAsChars (c: if builtins.match "[A-Za-z0-9._-]" c != null then c else "-");
in
{
  options.services.sandbox-vm = {
    enable = mkEnableOption "a NixOS VM for this devenv";
    name = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9._-]*";
      default = "devenv-${toName project}";
      defaultText = lib.literalExpression ''"devenv-<project>"'';
      description = ''
        The name of the VM. It is the SSH host alias and the directory name in
        ~/.devenv-sandbox-vm. It has only letters, digits, `.`, `_` and `-`.

        Two projects in directories with the same name get the same default name. The
        process of the second project then exits with a message, and it does not start the
        VM of the first project. Set this option in one of the projects. If you move a
        project to a different directory, remove the file
        `~/.devenv-sandbox-vm/devenv-<project>/root`.
      '';
    };
    hostName = mkOption {
      type = types.strMatching "[A-Za-z0-9]([A-Za-z0-9.-]{0,62}[A-Za-z0-9])?";
      default = toHostName project;
      defaultText = lib.literalExpression ''"<project>"'';
      description = ''
        The hostname of the guest. The host sets it at run time. If you set
        `networking.hostName` in the NixOS configuration of the guest, the guest uses that value.
      '';
    };
    user = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        The user name for the login. The guest creates the account at its first boot. The
        default is your user name on the host. Thus the account is not in the NixOS
        configuration, and each person on the project can have a different name.

        Set it if your user name on the host is not a valid Linux user name, for example if
        it has capital letters or dots. To set it only for yourself, use `devenv.local.nix`.
        Do not change it for an existing VM: the VM has no account with the new name, so the
        process cannot log in. To use a different name, create a new VM.
      '';
    };
    configDir = mkOption {
      type = types.strMatching "[A-Za-z0-9_.][A-Za-z0-9_./-]*";
      default = "sandbox-vm";
      description = ''
        The directory in the repo that contains the NixOS flake of the guest. The system of
        the VM comes from the committed files in this directory. When these files change,
        run `devenv tasks run sandbox-vm:update` to update the installed system. Data in the
        VM does not change. The task fails if the directory has changes that are not
        committed.
      '';
    };
    configName = mkOption {
      type = types.strMatching "[A-Za-z0-9_][A-Za-z0-9_.-]*";
      default = "devenv-sandbox-vm";
      description = ''
        The prefix of the nixosConfigurations (`prefix` of mkVms). The guest builds
        `<configName>-<uname -m>`.
      '';
    };
    homeManager = mkOption {
      type = types.nullOr (types.strMatching "[^#]+#[A-Za-z0-9@._+-]+");
      default = null;
      example = "github:me/dotfiles#me@sandbox-aarch64-linux";
      description = ''
        Your home-manager configuration for the login user in the VM, as
        `<flake>#<configuration>`: the name in `homeConfigurations` of the flake. The VM
        builds and activates it when the process creates the VM, and at each
        `devenv tasks run sandbox-vm:update`. To apply it at other times, run
        `devenv tasks run sandbox-vm:home`.

        The configuration is yours. Thus set it in `devenv.local.nix`. The VM gets the flake
        through the proxy, and the flake uses its own flake.lock. A private repo or an SSH
        URL needs credentials in the VM.
      '';
    };
    cpus = mkOption {
      type = types.ints.positive;
      default = 4;
      description = "The number of virtual CPUs.";
    };
    memory = mkOption {
      type = types.ints.positive;
      default = 8;
      description = "The memory in GiB.";
    };
    disk = mkOption {
      type = types.ints.positive;
      default = 100;
      description = ''
        The disk size in GiB. The process applies it when it creates the VM. If you change it
        later, an existing VM does not change.
      '';
    };
    installer.iso = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "~/Downloads/nixos-minimal-26.05-aarch64-linux.iso";
      description = ''
        The path of a NixOS installer ISO on the host, for the architecture of the VM. `~/` at
        the start is your home directory. Get the minimal ISO from https://nixos.org/download.

        With null, the host builds a disk image of the system on a Linux builder, and a new
        VM gets a copy. Nix keeps the software of the system in its store on the host. Thus
        Nix downloads only the changed software for a subsequent new VM. The host uses the
        builder only when it creates a VM.

        With a path, a new VM boots the ISO one time and the installer installs the system.
        The VM downloads or builds all software, and the host uses no Linux builder. The
        path is different for each person. Thus set it in `devenv.local.nix`.
      '';
    };
    proxy = {
      allowedDomains = mkOption {
        type = types.nullOr (types.listOf types.str);
        default = null;
        example = [
          "github.com"
          "*.github.com"
          "cache.nixos.org"
        ];
        description = ''
          The host names that the VM can connect to. `*.example.com` matches the subdomains
          of example.com, but not example.com. With null, the VM can connect to all hosts on
          the internet. With a list, the proxy refuses all other hosts. The proxy always
          refuses the host. It also refuses private addresses, except for `allowedLocalDomains`
          and `allowRanges`.
        '';
      };
      deniedDomains = mkOption {
        type = types.listOf types.str;
        default = [ ];
        description = ''
          Host names that the proxy always refuses, also when `allowedDomains` is null. `*.`
          works as in `allowedDomains`.
        '';
      };
      allowedLocalDomains = mkOption {
        type = types.listOf types.str;
        default = [ ];
        example = [ "git.home.example" ];
        description = ''
          Host names that the VM can connect to also when they resolve to a private IP
          address, for example a git server or a cache on your local network. `*.` works as in
          `allowedDomains`. The proxy still refuses this computer.
        '';
      };
      denyRanges = mkOption {
        type = types.listOf types.str;
        default = [ ];
        example = [ "203.0.113.0/24" ];
        description = ''
          Address ranges (CIDR) that the proxy refuses in addition to private addresses. Use
          this for networks with public addresses that only this computer can connect to,
          for example through a VPN.
        '';
      };
      allowRanges = mkOption {
        type = types.listOf types.str;
        default = [ ];
        description = ''
          Address ranges (CIDR) that the proxy permits. This has priority over `denyRanges` and
          over the rule that refuses private addresses. Thus it can open your local network
          to the VM.
        '';
      };
      ipv6 = mkOption {
        type = types.bool;
        default = false;
        description = ''
          Whether the proxy also connects to IPv6 addresses. On a network with public IPv6
          addresses, the proxy cannot know which addresses are computers of your local
          network. Thus with true, the VM can connect to these computers.
        '';
      };
      settings = mkOption {
        type = yaml.type;
        default = { };
        example = {
          connect_timeout = "10s";
        };
        description = ''
          More settings for the configuration file of smokescreen. They have priority over
          the settings that this module sets, but not over `ip` and `acl_file`. They can
          make the proxy less safe.
        '';
      };
      acl = mkOption {
        type = yaml.type;
        default = { };
        description = ''
          More settings for the ACL file of smokescreen. They have priority over the
          settings that this module sets. They can make the proxy less safe.
        '';
      };
    };
    logSize = mkOption {
      type = types.ints.between 1 128;
      default = 20;
      description = ''
        The size limit in MiB for each log of the VM (console, gvproxy and proxy). Each log
        keeps its current file and one older file. Thus a log uses two times this size at
        most.
      '';
    };
    backup = {
      enable = mkEnableOption "backups of the files of the VM with borg";
      repository = mkOption {
        type = types.nullOr (types.strMatching "(/|~/).*");
        default = null;
        example = "/Volumes/Backup/sandbox-vm";
        description = ''
          The directory on the host for the borg repository, as an absolute path. The process
          creates the repository if it does not exist. `~/` at the start is your home
          directory. A relative path is not permitted. With null, the directory is
          `~/.devenv-sandbox-vm-backups/<name>`. The default is not in the directory of the VM.
          Thus the backups stay when you remove the VM.
        '';
      };
      paths = mkOption {
        type = types.listOf (types.strMatching "/.*");
        default = [ "/home" ];
        description = ''
          The directories in the VM that a backup contains. A backup does not go into other
          file systems that the VM mounts below these directories.
        '';
      };
      exclude = mkOption {
        type = types.listOf types.str;
        default = [ ];
        example = [
          "home/*/.cache"
          "node_modules"
        ];
        description = ''
          Patterns for files that a backup does not contain (`--exclude` of GNU tar). The
          names in a backup have no `/` at the start.
        '';
      };
      maxSize = mkOption {
        type = types.nullOr types.ints.positive;
        default = null;
        description = ''
          The size limit in GiB for the data that the VM sends for one backup. A backup that
          is larger fails. The VM controls what it sends, thus without a limit it can fill
          the disk of the host. With null, the limit is the disk size of the VM
          (`services.sandbox-vm.disk`).
        '';
      };
      interval = mkOption {
        type = types.ints.positive;
        default = 24;
        description = ''
          The time in hours between two backups of the sandbox-vm-backup process.
        '';
      };
      compression = mkOption {
        type = types.str;
        default = "zstd";
        description = "The compression of borg (`--compression`).";
      };
      keep = lib.genAttrs [ "hourly" "daily" "weekly" "monthly" "yearly" ] (
        period:
        mkOption {
          type = types.ints.unsigned;
          default =
            {
              daily = 7;
              weekly = 4;
              monthly = 6;
            }
            .${period} or 0;
          description = ''
            The number of ${period} backups to keep (`--keep-${period}` of `borg prune`).
            Each backup deletes the older backups that no rule keeps. If all values are 0, borg
            deletes no backup.
          '';
        }
      );
    };
    expose = mkOption {
      type = types.attrsOf (
        types.coercedTo types.port (port: { guest = port; }) (
          types.submodule (
            { config, ... }:
            {
              options = {
                guest = mkOption {
                  type = types.port;
                  description = "The port in the VM.";
                };
                host = mkOption {
                  type = types.port;
                  default = config.guest;
                  defaultText = lib.literalExpression "guest";
                  description = "The port on 127.0.0.1 of the host.";
                };
              };
            }
          )
        )
      );
      default = { };
      example = lib.literalExpression ''
        {
          web = 3000;
          db = { guest = 5432; host = 15432; };
        }
      '';
      description = ''
        Ports in the VM that the host makes available on its address 127.0.0.1. Each port has a
        name. A number sets the same port on the two sides. The host opens an SSH tunnel into
        the VM for each port. To apply changes while the VM runs, run
        `devenv tasks run sandbox-vm:ports` or enter the shell again.
      '';
    };
    reach = mkOption {
      type = types.attrsOf (
        types.submodule (
          { config, ... }:
          {
            options = {
              host = mkOption {
                type = types.strMatching "[A-Za-z0-9.-]+";
                description = ''
                  The host name or IPv4 address. The host resolves the name and makes the
                  connection.
                '';
              };
              port = mkOption {
                type = types.port;
                description = "The port on that host.";
              };
              guest = mkOption {
                type = types.port;
                default = config.port;
                defaultText = lib.literalExpression "port";
                description = "The port on 127.0.0.1 of the VM.";
              };
            };
          }
        )
      );
      default = { };
      example = lib.literalExpression ''
        {
          internal = { host = "my-internal-service"; port = 8080; };
        }
      '';
      description = ''
        Services that the VM can connect to, as exceptions to its network rules. Each
        service has a name and is one port on one host. The VM gets the service on its
        address 127.0.0.1. The host makes the connection for the VM through an SSH tunnel.
        Thus a service can also be on your local network or on the host itself. To apply
        changes, do the same as for `expose`.
      '';
    };
  };
}
