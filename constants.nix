# The values that the host module (host/) and the guest module (guest/) must agree on.
{
  # The network that gvproxy creates for a VM is always the same. gvproxy has a static DHCP
  # lease: this MAC address gets 192.168.127.2, and the SSH forward of gvproxy points to that
  # address. The installer uses the lease. The installed system sets the address itself, for
  # the interface with this MAC address.
  #
  # All VMs use the same MAC address and IP address. A conflict is not possible: each VM has
  # its own gvproxy, and thus its own virtual network.
  guestMac = "5a:94:ef:e4:0c:ee";
  guestAddress = "192.168.127.2/24";
  # gvproxy maps this address to the loopback address of the host. The proxy listens there.
  proxyGateway = "192.168.127.254";

  # The vsock port of the agent in the guest that sets the clock.
  timesyncPort = 1234;

  # This file exists only in the guest. The host module finds it and does nothing there.
  guestMarker = "/etc/devenv-sandbox-vm";
  # The directory in the guest for the files that the host sends: settings, the key for the
  # login, and the configuration of the system.
  guestStateDir = "/var/lib/devenv-sandbox-vm";

  # The binary caches for the installation and for the installed system.
  substituters = [
    "https://nix-community.cachix.org"
    "https://devenv.cachix.org"
  ];
  trustedPublicKeys = [
    "nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs="
    "devenv.cachix.org-1:w1cLUi8dv3hnoSPGAuibQv+f9TZLr6cv/Hm9XgU50cw="
  ];
}
