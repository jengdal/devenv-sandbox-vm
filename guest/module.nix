{
  config,
  modulesPath,
  pkgs,
  lib,
  ...
}:
let
  cfg = config.devenv-sandbox-vm;

  # The values that the host module also has: the network that the host creates for the VM,
  # the port of the clock agent, and the directory for the files that the host sends.
  inherit (import ../constants.nix)
    guestMac
    guestAddress
    proxyGateway
    timesyncPort
    guestMarker
    guestStateDir
    substituters
    trustedPublicKeys
    ;

  # The address of the proxy for programs in the guest.
  proxyPort = 3128;
in
{
  imports = [ (modulesPath + "/profiles/qemu-guest.nix") ];

  options.devenv-sandbox-vm = {
    # The hypervisor gives the memory that the guest frees back to the host, but only free
    # blocks of 2 MiB or larger, and not the page cache. These two timers make more memory
    # free in that way. Each has a cost, thus they are off by default.
    memory.compact = {
      enable = lib.mkEnableOption "a timer that joins fragments of free memory, thus the host gets them back. Each run uses the CPU of the guest for a short time";
      interval = lib.mkOption {
        type = lib.types.str;
        default = "5min";
        description = "The time between two runs, as a systemd time span.";
      };
    };
    memory.dropCaches = {
      enable = lib.mkEnableOption "a timer that empties the page cache, thus the host gets that memory back. The guest then reads files from the disk again, and work with many files is slower after each run";
      interval = lib.mkOption {
        type = lib.types.str;
        default = "15min";
        description = "The time between two runs, as a systemd time span.";
      };
    };
  };

  config = {
    # There is no static hostname and no static time zone by default. The host sends its
    # values (services.sandbox-vm.hostName, and the time zone of the host), and the guest
    # applies them at run time. The time zone is the same as one that you set with
    # timedatectl. To use static values, set networking.hostName or time.timeZone. The
    # service then does not apply that value.
    networking.hostName = lib.mkDefault "";
    systemd.services.devenv-sandbox-vm-settings = {
      description = "Set the hostname and the time zone that the host supplies";
      wantedBy = [ "multi-user.target" ];
      unitConfig.ConditionPathExists = "${guestStateDir}/env";
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      script = ''
        . ${guestStateDir}/env
      ''
      + lib.optionalString (config.networking.hostName == "") ''
        [ -z "''${hostname:-}" ] || ${pkgs.hostname}/bin/hostname "$hostname"
      ''
      + lib.optionalString (config.time.timeZone == null) ''
        [ -z "''${timezone:-}" ] || timedatectl set-timezone "$timezone"
      '';
    };

    # This file identifies this computer as the guest. Thus the host module does nothing here.
    environment.etc.${lib.removePrefix "/etc/" guestMarker}.text = "";

    # The account that the host uses for the login. The host supplies the name
    # (services.sandbox-vm.user; the default is the user name on the host). Thus this file cannot
    # declare the account, because all users of the project use this configuration.
    # Accounts that this service creates stay after a system update, because
    # users.mutableUsers is on.
    systemd.services.devenv-sandbox-vm-user = {
      description = "Create the account that the host uses for the login";
      wantedBy = [ "multi-user.target" ];
      before = [ "sshd.service" ];
      unitConfig.ConditionPathExists = "${guestStateDir}/env";
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      path = [ pkgs.shadow ];
      script = ''
        . ${guestStateDir}/env
        [ -n "''${user:-}" ] || exit 0
        id "$user" >/dev/null 2>&1 \
          || useradd --create-home --groups wheel --shell /run/current-system/sw/bin/bash "$user"
      '';
    };
    assertions = [
      {
        assertion = config.users.mutableUsers;
        message = "devenv-sandbox-vm creates the login account at runtime, which needs users.mutableUsers.";
      }
    ];
    security.sudo.wheelNeedsPassword = lib.mkDefault false;

    # The disk image is the same for all VMs. Thus a VM that the host created from the disk
    # image has no files from the host at its first boot. The host gives the files to that
    # boot as systemd credentials, and this service stores them. It does nothing in a VM
    # that an installer installed, and it does nothing at subsequent boots.
    systemd.services.devenv-sandbox-vm-provision = {
      description = "Store the files that the host supplies at the first boot";
      wantedBy = [ "multi-user.target" ];
      before = [
        "devenv-sandbox-vm-settings.service"
        "devenv-sandbox-vm-user.service"
        "devenv-sandbox-vm-proxy.service"
        "sshd.service"
      ];
      unitConfig.ConditionPathExists = "!${guestStateDir}/authorized_keys";
      serviceConfig = {
        Type = "oneshot";
        ImportCredential = "devenv-sandbox-vm.*";
      };
      # The service runs only while authorized_keys does not exist. Thus it writes that
      # file last: if it stops before the end, it runs again at the subsequent boot.
      script = ''
        from="''${CREDENTIALS_DIRECTORY:-}/devenv-sandbox-vm"
        for name in env provisioned authorized_keys; do
          [ -e "$from.$name" ] || exit 0
        done
        for name in env provisioned authorized_keys; do
          install -D -m 644 "$from.$name" "${guestStateDir}/$name"
        done
      '';
    };

    # The installation writes the key of the host for this VM to this file. Thus the key is
    # not in the committed configuration.
    # The host logs in only with that key, and only to the account of the user.
    services.openssh = {
      enable = true;
      authorizedKeysFiles = [ "${guestStateDir}/authorized_keys" ];
      settings = {
        PasswordAuthentication = lib.mkDefault false;
        KbdInteractiveAuthentication = lib.mkDefault false;
        PermitRootLogin = lib.mkDefault "no";
      };
    };

    # The only connection out of the VM is through the HTTP proxy of the host. The proxy
    # also resolves the host names. The port of the proxy on the host can change between boots.
    # Thus programs use a fixed local address, and this service forwards it to the proxy.
    systemd.services.devenv-sandbox-vm-proxy = {
      description = "Forward connections to the proxy on the host";
      wantedBy = [ "multi-user.target" ];
      before = [ "nix-daemon.service" ];
      unitConfig.ConditionPathExists = "${guestStateDir}/env";
      serviceConfig = {
        EnvironmentFile = "${guestStateDir}/env";
        ExecStart = "${pkgs.socat}/bin/socat TCP-LISTEN:${toString proxyPort},bind=127.0.0.1,fork,reuseaddr TCP:${proxyGateway}:\${proxy_port}";
        Restart = "always";
        DynamicUser = true;
      };
    };
    networking.proxy = {
      default = lib.mkDefault "http://127.0.0.1:${toString proxyPort}";
      noProxy = lib.mkDefault "127.0.0.1,localhost,::1";
    };
    # Node reads the proxy variables only when this variable is set.
    environment.sessionVariables.NODE_USE_ENV_PROXY = lib.mkDefault "1";
    security.sudo.extraConfig = ''
      Defaults env_keep += "http_proxy https_proxy ftp_proxy rsync_proxy all_proxy no_proxy"
      Defaults env_keep += "NODE_USE_ENV_PROXY"
    '';
    # The host aliases of each user in ~/.ssh/config.d, for example for deploy keys. They work
    # also when Home Manager manages ~/.ssh/config. %d is the home directory of the user: the
    # system configuration cannot use ~. A pattern without files is not an error.
    # SSH also goes through the proxy. Thus remotes such as git@github.com work.
    programs.ssh.extraConfig = ''
      Include %d/.ssh/config.d/*

      Host * !localhost !127.* !::1
        ProxyCommand ${pkgs.netcat-openbsd}/bin/nc -X connect -x 127.0.0.1:${toString proxyPort} %h %p
    '';
    # A fixed address and no DHCP. There is no default route and no name server. The proxy is on
    # this network, and no other destination is available. Thus all other connections fail
    # immediately and do not wait for a timeout. A static address makes the start faster, the boot
    # does not wait for a DHCP lease.
    networking.useDHCP = lib.mkDefault false;
    networking.useNetworkd = lib.mkDefault true;
    systemd.network.networks."10-devenv-sandbox-vm" = {
      matchConfig.MACAddress = guestMac;
      address = [ guestAddress ];
      linkConfig.RequiredForOnline = "routable";
    };
    # networkd enables a local resolver, but the resolver has no server to send lookups to.
    # The priority is one step higher than mkDefault, because networkd enables it with
    # mkDefault.
    services.resolved.enable = lib.mkOverride 999 false;
    # NTP cannot go through an HTTP proxy. The host sets the clock.
    services.timesyncd.enable = lib.mkDefault false;

    # The hypervisor tells this agent to set the clock when the host wakes from sleep (--timesync).
    # The agent runs as root, and it can also start commands and write files. Thus it
    # accepts only the command that sets the clock. The hypervisor has a socket on the host
    # for the agent. The sandbox of the hypervisor of a different VM does not permit that
    # socket, and this is the second protection.
    systemd.services.devenv-sandbox-vm-timesync = {
      description = "QEMU guest agent on vsock. Sets the clock after the host wakes from sleep";
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        ExecStart = "${pkgs.qemu_kvm.ga}/bin/qemu-ga --method=vsock-listen --path=3:${toString timesyncPort} --allow-rpcs=guest-set-time";
        Restart = "always";
      };
    };

    # The timers of devenv-sandbox-vm.memory.
    systemd.services.devenv-sandbox-vm-compact-memory = lib.mkIf cfg.memory.compact.enable {
      description = "Join fragments of free memory, thus the host gets them back";
      serviceConfig.Type = "oneshot";
      script = "echo 1 >/proc/sys/vm/compact_memory";
    };
    systemd.timers.devenv-sandbox-vm-compact-memory = lib.mkIf cfg.memory.compact.enable {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = cfg.memory.compact.interval;
        OnUnitActiveSec = cfg.memory.compact.interval;
      };
    };
    systemd.services.devenv-sandbox-vm-drop-caches = lib.mkIf cfg.memory.dropCaches.enable {
      description = "Empty the page cache, thus the host gets that memory back";
      serviceConfig.Type = "oneshot";
      script = ''
        sync
        echo 3 >/proc/sys/vm/drop_caches
      '';
    };
    systemd.timers.devenv-sandbox-vm-drop-caches = lib.mkIf cfg.memory.dropCaches.enable {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = cfg.memory.dropCaches.interval;
        OnUnitActiveSec = cfg.memory.dropCaches.interval;
      };
    };

    # This must agree with the disk layout of the installer (host/supervisor/guest-install.sh).
    boot.loader.systemd-boot = {
      enable = true;
      configurationLimit = lib.mkDefault 10;
    };
    boot.loader.efi.canTouchEfiVariables = false;
    boot.kernelParams = [ "console=hvc0" ];
    fileSystems."/boot" = {
      device = "/dev/disk/by-label/ESP";
      fsType = "vfat";
      options = [ "umask=0077" ];
    };
    fileSystems."/" = {
      device = "/dev/disk/by-label/nixos";
      fsType = "ext4";
      options = [
        "noatime"
        "nodiratime"
        "discard"
      ];
      autoResize = lib.mkDefault true;
    };
    boot.kernelPackages = lib.mkDefault pkgs.linuxPackages_latest;

    # The disk image of this system. Without an installer image, the host builds it on a
    # Linux builder and a new VM gets a copy (host/supervisor/install.go). The layout and
    # the labels are those of the installer (host/supervisor/guest-install.sh).
    system.build.sandboxVmImage = import (modulesPath + "/../lib/make-disk-image.nix") {
      inherit config lib pkgs;
      format = "raw";
      partitionTableType = "efi";
      bootSize = "512M";
      diskSize = "auto";
      copyChannel = false;
    };
    # The disk image is only as large as its files. The host makes the disk of the VM larger,
    # and the first boot makes the file system as large as the disk.
    boot.growPartition = lib.mkDefault true;

    nix.settings = {
      experimental-features = [
        "nix-command"
        "flakes"
      ];
      trusted-users = [
        "root"
        "@wheel"
      ];
      auto-optimise-store = lib.mkDefault true;
      extra-substituters = substituters;
      extra-trusted-public-keys = trustedPublicKeys;
    };

    nix.gc = {
      automatic = lib.mkDefault true;
      dates = lib.mkDefault "weekly";
      options = lib.mkDefault "--delete-older-than 14d";
    };

    environment.systemPackages = [ ];

    programs.direnv = {
      enable = lib.mkDefault true;
      nix-direnv.enable = lib.mkDefault true;
    };

    # Only for a configuration that does not set the value. The template sets it in
    # configuration.nix. Do not change this value: an existing VM must keep the version
    # of its installation.
    system.stateVersion = lib.mkDefault "26.05";
  };
}
