# The network of the VM

The VM has a fixed address on its own private network. This network has no route to other networks.
The only route out of the VM is an HTTP proxy on the host. Programs in the VM find the proxy through
the variables `http_proxy` and `https_proxy`. The VM sets these variables for the full system. They
point to `127.0.0.1:3128`.

These connections work:

- HTTP and HTTPS from programs that use the proxy variables: curl, git, Nix, devenv. For Node, the
  VM sets `NODE_USE_ENV_PROXY=1`.
- SSH, and thus git remotes such as `git@github.com:…`. The SSH client of the VM connects through
  the proxy.

These connections do not work:

- Name lookups. The VM has no DNS. The proxy resolves the names of the hosts that it connects to.
  Commands such as `ping github.com` and `dig` fail.
- All protocols that are not TCP through the proxy: UDP, ICMP, NTP. The host sets the clock of the
  VM.
- Programs that ignore the proxy variables. They cannot connect. You must give them their own proxy
  setting. The Docker daemon is an example.
- The host.
- Private addresses, except for the names in
  [`services.sandbox-vm.proxy.allowedLocalDomains`](reference/options.md#proxy-allowedlocaldomains).

To connect to one port of a host without the proxy, use
[`services.sandbox-vm.reach`](reference/options.md#reach). Refer to [Ports between the host and the
VM](ports.md).

The log of the proxy has one `CANONICAL-PROXY-DECISION` line for each connection that the VM made or
tried to make. Refer to [Logs](logs-and-backups.md#logs).

## How to limit the destinations

The proxy always refuses the host. It also refuses private addresses, except for the names in
[`allowedLocalDomains`](reference/options.md#proxy-allowedlocaldomains) and the ranges in
[`allowRanges`](reference/options.md#proxy-allowranges). These options control the other
destinations. Changes apply when you restart
[`devenv up sandbox-vm`](reference/commands.md#sandbox-vm). A reboot in the VM does not apply them.

All options and their defaults are in [Options: Proxy](reference/options.md#proxy).

### Host names

```nix
services.sandbox-vm.proxy.allowedDomains = [
  "cache.nixos.org"
  "*.cachix.org"
  "github.com"
  "*.github.com"
  "*.githubusercontent.com"
];
```

- With an [`allowedDomains`](reference/options.md#proxy-alloweddomains) list, the proxy refuses
  connections to hosts that are not in the list. It also refuses connections to an IP address
  without a name.
- A name with `*.` at the start matches the subdomains: `*.github.com` matches `api.github.com`, but
  not `github.com`. To allow both, list both, as in the example. This is the same in
  [`deniedDomains`](reference/options.md#proxy-denieddomains) and
  [`allowedLocalDomains`](reference/options.md#proxy-allowedlocaldomains).
- The list in the example is a start for the hosts that the VM uses to install and update itself. To
  find the hosts that are missing, look for the lines with `"allow":false` in the log of the proxy.

### Servers on your local network

```nix
services.sandbox-vm.proxy.allowedLocalDomains = [ "git.home.example" ];
```

- [`allowedLocalDomains`](reference/options.md#proxy-allowedlocaldomains) lets the VM connect to
  these names also when they resolve to a private IP address, for example a git server or a cache on
  your local network.
- [`allowRanges`](reference/options.md#proxy-allowranges) opens address ranges in CIDR notation, for
  example `[ "192.168.1.0/24" ]` for the addresses `192.168.1.0` to `192.168.1.255`.
- The proxy has no rules for ports. All ports of the computers that you open are open to the VM. For
  one port, use [`services.sandbox-vm.reach`](reference/options.md#reach). Refer to [Ports between
  the host and the VM](ports.md).
- Neither option can open the host.
- [`ipv6 = true`](reference/options.md#proxy-ipv6) can also open your local network. On a network
  with public IPv6 addresses, the proxy cannot know which addresses are computers of your local
  network.
- [`denyRanges`](reference/options.md#proxy-denyranges) does the opposite: it refuses networks with
  public addresses that only the host can connect to, for example through a VPN.

### Limits

- If you allow access to servers on the internet, such as `*.github.com`, an attacker can send your
  data there. If this is a problem, open the proxy only when you install or update the VM. Keep it
  closed at other times.
- [`settings`](reference/options.md#proxy-settings) and [`acl`](reference/options.md#proxy-acl) have
  priority over the other options. They can make the proxy less safe.
