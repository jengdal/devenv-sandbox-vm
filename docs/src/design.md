# Design

## Goals

Provide a sandboxed development environment for coding agents and package managers that potentially
install compromised dependencies.

### The user's full project must easily run inside the VM

The user's project must easily run inside the VM. If the VM is created from scratch every time, they
would need to always move resources like databases, large files etc. into each new VM. Compared to
traditional workflows it would just create a bunch of new unnecessary complexity. Having a long
lived VM makes things simpler. The user may still use Docker inside the VM in case short lived tasks
are needed.

- Let the user define and manage long lived VMs.
- One VM per devenv that declares it. The VM can hold one or more projects.

### The host devenv and a devenv in the VM are independent

The devenv on the host declares the VM: its NixOS and Home Manager configuration, its network rules,
and the tooling to install and update it. It does not run or sync your project's devenv inside the
VM. If you want the same devenv in the VM, set it up there yourself. Keeping them independent avoids
unnecessary complexity and coupling, and you decide which projects run inside the VM.

### The VM has no direct filesystem access to the host

A user might be interested in continuing to run their project code, databases and so on on the host,
and only run coding agents in the sandboxed VM. But if the agent can modify code that runs
immediately on the host, the sandbox is broken. The solution is to not share files between the VM
and the host. The user should run their project inside the VM. Not mounting any volumes also makes
the attack surface of the host smaller.

Move the files between the VM and the host with git. The remote editing feature of an editor and
sshfs can decrease the isolation of the VM, so decide if the risk is acceptable for your work. A
terminal editor in the VM does not have this risk. Refer to
[How to use the VM](using-the-vm.md).

### The VM's network access is controlled

By default only traffic over an HTTP proxy is possible, with allow/deny rules. The user can port
forward access to additional services into the VM. Those can be declared in the devenv.

### Use krunkit as the hypervisor

It's lighter weight than alternatives. To make it even smaller, devenv-sandbox-vm disables its GPU
features. That also removes the GPU stack from what the guest can reach. It's probably not advisable
to run a local LLM inside the same VM that also runs the agent, so in the author's opinion it should
not be a problem that the GPU is unreachable. If you have another use case where you need the GPU, run the
code in another VM or on the host.

libkrun has a fixed version, not the one of nixpkgs, and a patch (a backport of
[libkrun PR #931](https://github.com/libkrun/libkrun/pull/931)) that releases free guest memory back
to the host. The changes are in `host/platform/`. See also [The memory of the VM](memory.md).

## Future work

### Network

- If the VM needs to access an external service that requires an API key, that API key must
  currently be configured inside the VM, accessible to agents and potentially compromised packages
  - The author has not needed to solve this yet, so has not done any research on the topic, but
    there's probably some ready made "MITM" proxy that could be used to replace a placeholder
    API key the VM uses in its requests with the real one. The MITM proxy could be run by the devenv
    that runs the VM, and its port tunneled into the VM using
    [`services.sandbox-vm.reach`](reference/options.md#reach).

### Platforms

- Linux support should be possible. The author may or may not implement it.
