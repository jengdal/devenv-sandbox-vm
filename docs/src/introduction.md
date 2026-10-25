# devenv-sandbox-vm

**WARNING: This is an experiment. Use it if you want to help test and develop it.**

devenv-sandbox-vm runs a sandboxed NixOS VM on a Mac. Use it to decrease the risk from coding agents
and supply chain attacks from package managers such as NPM.

In this documentation:

- The "host" is your Mac.
- The "VM" is the virtual machine that devenv-sandbox-vm creates for a project.
- `<project>` is the directory name of your repo.

## What is it?

- Declare a NixOS VM in the devenv of your project, or in a separate project.
- The VM is a long-lived workspace for you and/or agents.
- The VM is sandboxed.
  - It cannot read files on the host.
  - It can connect to the network only through an HTTP proxy. You set the access rules. By default,
    the proxy rejects only the host and private addresses.
  - The hypervisor, the network and the proxy run in macOS sandboxes (`sandbox-exec`). If an
    attacker gets out of the VM, the sandbox limits what the attacker can do on the host.
  - Refer to [What an agent in the VM can and cannot
    do](security.md#what-an-agent-in-the-vm-can-and-cannot-do).
- Work in the VM with a terminal editor, or with an editor on the host through SSH. Refer to
  [How to use the VM](using-the-vm.md).
- Expose ports of the VM on the host.
- Let the VM connect to ports on other hosts.
- Make backups of the VM automatically (optional).
- The VM gives the memory that it does not use back to macOS. VMs on macOS usually do not do this.

You declare the VM with Nix in devenv, so you can extend it. For example, expose ports of the VM
with tools like `netbird expose`, `tailscale serve`, `ngrok`, `sish` etc. or let agents in the VM
use external services and MCP servers.

For the reasons behind these decisions, refer to [Design](design.md).

## LLM Use

The author used coding agents carefully for the design and development of this project.

## Credits

devenv-sandbox-vm is a small layer on top of these projects:

- [libkrun](https://github.com/libkrun/libkrun) and [krunkit](https://github.com/libkrun/krunkit):
  the hypervisor. The memory patch is a backport of
  [libkrun PR #931](https://github.com/libkrun/libkrun/pull/931).
- [gvproxy](https://github.com/containers/gvisor-tap-vsock): the network of the VM.
- [smokescreen](https://github.com/stripe/smokescreen): the HTTP proxy.
- [s6](https://skarnet.org/software/s6/) (s6-log): the log rotation.
- [BorgBackup](https://www.borgbackup.org): the backups.
- [OpenSSH](https://www.openssh.com): the connection to the VM and the port forwards.
- [Nix](https://nixos.org), [NixOS and nixpkgs](https://github.com/NixOS/nixpkgs): the system of
  the VM and all packages.
- [devenv](https://devenv.sh): the host environment.
- [Home Manager](https://github.com/nix-community/home-manager): the personal configuration in the
  VM.

## License

devenv-sandbox-vm is licensed under the [Apache License, Version
2.0](https://github.com/jengdal/devenv-sandbox-vm/blob/main/LICENSE). Some parts come from other
projects. [NOTICE](https://github.com/jengdal/devenv-sandbox-vm/blob/main/NOTICE) lists them.
