# The test of the guest module (guest/module.nix) in a VM of the NixOS test framework. The
# node `host` is in the place of the Mac: it has the address of the proxy, and it logs in
# with the key of the VM.
#
# The test is not for the hypervisor and the network of the host (krunkit, gvproxy): it
# checks what the guest does with the files that the host supplies.
{ pkgs, module }:
let
  lib = pkgs.lib;
  constants = import ../constants.nix;
  state = constants.guestStateDir;
  inherit (import (pkgs.path + "/nixos/tests/ssh-keys.nix") pkgs) snakeOilPrivateKey snakeOilPublicKey;

  # The files that the host gives to the first boot (firstBootCredentials in
  # host/supervisor/install.go).
  credentials = {
    env = ''
      hostname="project-vm"
      user="tester"
      proxy_port="23007"
      timezone="Europe/Helsinki"
    '';
    authorized_keys = snakeOilPublicKey + "\n";
    provisioned = "tree-of-the-installation\n";
  };

  # The text in base64. Nix has no function for it.
  toBase64 =
    text:
    let
      alphabet = lib.stringToCharacters "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
      bytes = map lib.strings.charToInt (lib.stringToCharacters text);
      count = lib.length bytes;
      # Three bytes from the position i become four characters.
      group =
        i:
        let
          byte = j: if i + j < count then lib.elemAt bytes (i + j) else 0;
          value = byte 0 * 65536 + byte 1 * 256 + byte 2;
          char = unit: lib.elemAt alphabet (lib.mod (value / unit) 64);
        in
        char 262144
        + char 4096
        + (if i + 1 < count then char 64 else "=")
        + (if i + 2 < count then char 1 else "=");
    in
    lib.concatMapStrings group (lib.genList (i: i * 3) ((count + 2) / 3));

  # One port of the node `host` that answers with its number.
  answer = port: {
    listen = [
      {
        addr = "0.0.0.0";
        inherit port;
      }
    ];
    locations."/".return = "200 'port ${toString port}'";
  };
  address = address: {
    virtualisation.vlans = [ 1 ];
    networking.interfaces.eth1.ipv4.addresses = lib.mkForce [
      {
        inherit address;
        prefixLength = 24;
      }
    ];
  };
in
pkgs.testers.runNixOSTest {
  name = "devenv-sandbox-vm-guest";
  # A test without an error takes some minutes, also on a builder without KVM. Without this
  # limit, a test that stops waits for one hour.
  globalTimeout = 1800;

  nodes.host = {
    imports = [ (address constants.proxyGateway) ];
    services.nginx.enable = true;
    services.nginx.virtualHosts.first = answer 23007;
    services.nginx.virtualHosts.second = answer 23008;
    networking.firewall.allowedTCPPorts = [
      23007
      23008
    ];
  };

  nodes.guest =
    { nodes, ... }:
    {
    imports = [
      module
      (address (lib.head (lib.splitString "/" constants.guestAddress)))
    ];
    devenv-sandbox-vm.memory.compact.enable = true;
    devenv-sandbox-vm.memory.dropCaches.enable = true;

    # The test framework sets a hostname with the priority of the guest module. The guest
    # of a project has no static hostname.
    networking.hostName = "";
    # The differences between the VM of the test framework and the VM of krunkit: the test
    # framework boots the kernel directly from a disk without partitions, and it uses the
    # console hvc0 for its commands. Thus the guest must not use that console: it gets
    # the kernel parameters of a node without the guest module.
    #
    # The host gives the files as systemd credentials. krunkit has them in OEM strings of
    # the SMBIOS table. QEMU has no such table for each architecture, thus they are kernel
    # parameters here. systemd reads the two sources in the same way.
    boot.growPartition = false;
    boot.kernelPackages = pkgs.linuxPackages;
    boot.kernelParams = lib.mkForce (
      nodes.host.boot.kernelParams
      ++ lib.mapAttrsToList (
        name: text: "systemd.set_credential_binary=devenv-sandbox-vm.${name}:${toBase64 text}"
      ) credentials
    );
  };

  testScript = ''
    state = "${state}"
    key = "/root/key"
    # A command of a node has the channel of the test driver as its input. ssh reads its
    # input, and it can take the text that the driver sends to the shell of the node: the
    # test then waits for an answer that does not come. Thus ssh gets no input (-n). Only
    # the call that sends a file uses ssh_with_input: its input is a pipe.
    ssh_with_input = f"ssh -i {key} -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o BatchMode=yes -o ConnectTimeout=10"
    ssh = f"{ssh_with_input} -n"
    guest_address = "${lib.head (lib.splitString "/" constants.guestAddress)}"

    def wait_for_proxy(port):
        # The fixed local address of the proxy, without the variables of the session. The
        # address opens before the network of the guest is ready, thus the first requests
        # can fail.
        guest.wait_until_succeeds(
            "curl --silent --fail --noproxy '*' --max-time 10 http://127.0.0.1:3128/"
            f" | grep -q '^port {port}$'"
        )

    start_all()
    host.wait_for_unit("nginx.service")
    host.succeed(f"install -m 600 ${snakeOilPrivateKey} {key}")
    guest.wait_for_unit("multi-user.target")

    with subtest("the first boot stores the files of the host"):
        guest.succeed("test -e ${constants.guestMarker}")
        guest.succeed(f"grep -qx 'proxy_port=\"23007\"' {state}/env")
        assert guest.succeed(f"cat {state}/provisioned") == "tree-of-the-installation\n"
        assert guest.succeed(f"stat -c %a {state}/authorized_keys").strip() == "644"

    with subtest("the guest applies the settings of the host"):
        guest.wait_for_unit("devenv-sandbox-vm-settings.service")
        guest.wait_for_unit("devenv-sandbox-vm-user.service")
        assert guest.succeed("hostname").strip() == "project-vm"
        assert guest.succeed("timedatectl show --property Timezone --value").strip() == "Europe/Helsinki"
        assert "wheel" in guest.succeed("id -Gn tester")

    with subtest("the host logs in with its key, and only as the user"):
        guest.wait_for_unit("sshd.service")
        assert host.succeed(f"{ssh} tester@{guest_address} whoami").strip() == "tester"
        host.succeed(f"{ssh} tester@{guest_address} sudo -n true")
        host.fail(f"{ssh} root@{guest_address} true")
        settings = guest.succeed("sshd -T").lower()
        assert "passwordauthentication no" in settings, settings
        assert "permitrootlogin no" in settings, settings
        # The key is not in the home directory of a user.
        guest.fail("test -e /home/tester/.ssh/authorized_keys")
        guest.fail("test -e /root/.ssh/authorized_keys")

    with subtest("the only route out is the proxy"):
        guest.wait_for_unit("devenv-sandbox-vm-proxy.service")
        guest.wait_for_open_port(3128)
        wait_for_proxy(23007)
        assert guest.succeed("ip route show default").strip() == ""
        assert guest.succeed("ip -6 route show default").strip() == ""
        guest.fail("grep -q nameserver /etc/resolv.conf")
        guest.fail("getent hosts example.com")
        # The programs of a session get the fixed local address.
        proxy = guest.succeed("su - tester -c 'echo $http_proxy $https_proxy $NODE_USE_ENV_PROXY'")
        assert proxy.strip() == "http://127.0.0.1:3128 http://127.0.0.1:3128 1", proxy
        # sudo keeps the variables, for example for nixos-rebuild.
        proxy = guest.succeed("su - tester -c 'sudo -n env' | grep -x 'https_proxy=.*'")
        assert proxy.strip() == "https_proxy=http://127.0.0.1:3128", proxy
        # A host alias in ~/.ssh/config.d also goes through the proxy.
        guest.succeed(
            "su - tester -c 'mkdir -p ~/.ssh/config.d"
            " && printf \"Host github-test\\n  HostName github.com\\n\" > ~/.ssh/config.d/test'"
        )
        resolved = guest.succeed("su - tester -c 'ssh -G github-test'")
        assert "\nhostname github.com\n" in resolved, resolved
        assert "\nproxycommand " in resolved, resolved
        # Root does not read the aliases of the login account.
        resolved = guest.succeed("ssh -G github-test")
        assert "\nhostname github-test\n" in resolved, resolved
        # The proxy forward does not run as root.
        user = guest.succeed("ps -o user= -C socat | sort -u").strip()
        assert user not in ("", "root"), user

    with subtest("the settings of a subsequent run of the host"):
        # The port of the proxy changed. The host sends the settings and restarts the
        # services (pushSettings in host/supervisor/update.go).
        host.succeed(
            f"sed s/23007/23008/ ${pkgs.writeText "env" credentials.env} | {ssh_with_input} tester@{guest_address}"
            f" sudo install -D -m 644 /dev/stdin {state}/env"
        )
        host.succeed(
            f"{ssh} tester@{guest_address} 'sudo systemctl restart"
            " devenv-sandbox-vm-settings.service devenv-sandbox-vm-proxy.service;"
            " systemctl is-active --quiet devenv-sandbox-vm-proxy.service'"
        )
        guest.wait_for_open_port(3128)
        wait_for_proxy(23008)

    with subtest("a subsequent boot keeps the files"):
        # The task sandbox-vm:update records the new system. The credentials of the first
        # boot are still there, and the guest must not store them again.
        guest.succeed(f"echo tree-of-the-update > {state}/provisioned")
        guest.shutdown()
        guest.start()
        guest.wait_for_unit("multi-user.target")
        assert guest.succeed(f"cat {state}/provisioned") == "tree-of-the-update\n"
        guest.succeed(f"grep -qx 'proxy_port=\"23008\"' {state}/env")
        guest.wait_for_open_port(3128)
        wait_for_proxy(23008)
        assert guest.succeed("hostname").strip() == "project-vm"
        guest.wait_for_unit("sshd.service")
        assert host.succeed(f"{ssh} tester@{guest_address} whoami").strip() == "tester"

    with subtest("the memory timers run"):
        for name in ["compact-memory", "drop-caches"]:
            guest.succeed(f"systemctl is-active devenv-sandbox-vm-{name}.timer")
            guest.succeed(f"systemctl start devenv-sandbox-vm-{name}.service")
  '';
}
