# devenv-sandbox-vm

**WARNING: This is an experiment. Use it if you want to help test and develop it.**

devenv-sandbox-vm runs a sandboxed NixOS VM on a Mac. Use it to decrease the risk from coding agents
and supply chain attacks from package managers such as NPM.

**Documentation: <https://jengdal.github.io/devenv-sandbox-vm/>**

## What is it?

- Declare a NixOS VM in the devenv of your project, or in a separate project.
- The VM is a long-lived workspace for you and/or agents.
- The VM is sandboxed.
  - It cannot read files on the host.
  - It can connect to the network only through an HTTP proxy. You set the access rules. By default,
    the proxy rejects only the host and private addresses.
  - The hypervisor, the network and the proxy run in macOS sandboxes (`sandbox-exec`). If an
    attacker gets out of the VM, the sandbox limits what the attacker can do on the host.
- Work in the VM with a terminal editor, or with an editor on the host through SSH.
- Expose ports of the VM on the host.
- Let the VM connect to ports on other hosts.
- Make backups of the VM automatically (optional).
- The VM gives the memory that it does not use back to macOS. VMs on macOS usually do not do this.

## Where to start

- [Quick start](https://jengdal.github.io/devenv-sandbox-vm/quick-start.html): the requirements and
  the steps to create a VM.
- [Security](https://jengdal.github.io/devenv-sandbox-vm/security.html): what an agent in the VM can
  and cannot do, and the limits of the sandbox.
- [Options](https://jengdal.github.io/devenv-sandbox-vm/reference/options.html): all settings of the VM.
- [Development](https://jengdal.github.io/devenv-sandbox-vm/development.html): how to test and
  change this repo.

The pages of the documentation are in [docs/src](docs/src).

## License

devenv-sandbox-vm is licensed under the [Apache License, Version 2.0](LICENSE). Some parts come from
other projects. [NOTICE](NOTICE) lists them.
