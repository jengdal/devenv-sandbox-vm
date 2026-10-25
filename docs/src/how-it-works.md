# How it works

- The VM is a devenv service, `services.sandbox-vm`. The command
  [`devenv up sandbox-vm`](reference/commands.md#sandbox-vm) on the host starts the
  [`sandbox-vm`](reference/commands.md#sandbox-vm) process. This process creates, configures and
  runs the VM. The command `devenv up` without a name does not start the VM.
- The process is ready when the VM accepts SSH connections and has the settings of the host.
- A process that runs on the host can wait for the VM. Use
  `after = [ "devenv:processes:sandbox-vm" ]`. This does not start the VM. Start the two processes
  together: [`devenv up sandbox-vm <process>`](reference/commands.md#sandbox-vm). Do not use this
  for processes that must also run in the VM.
- The hypervisor is [krunkit](https://github.com/libkrun/krunkit). It runs in a sandbox: it can use
  only the files of the VM, and it has no network. devenv-sandbox-vm changes the packages krunkit
  and libkrun of nixpkgs (`host/platform/krunkit.nix`): libkrun has a fixed version, and the VM has
  no GPU device. Thus the VM cannot attack the GPU code of the host.
- The network of the VM is [gvproxy](https://github.com/containers/gvisor-tap-vsock). gvproxy is a
  user-mode network. It runs as your user.
- All connections out of the VM go through [smokescreen](https://github.com/stripe/smokescreen).
  smokescreen is an HTTP proxy that starts together with the VM. A sandbox makes sure that gvproxy
  can connect only to the proxy.
- These programs on the host read data from the VM and run in a macOS sandbox (`sandbox-exec`):
  krunkit, gvproxy, smokescreen, the log programs (s6-log and console-filter), and borg. A sandbox
  denies everything that its rules do not allow. Thus each program gets only its own files, the
  system libraries and the connections that it needs. It gets no other file of your computer. It
  gets no macOS service, except the name resolver (smokescreen) and power events (krunkit). Of the
  environment variables of your shell, it gets only the locale, the time zone and `TMPDIR`.
- Some programs on the host read data from the VM and have no sandbox:
  - The [`sandbox-vm`](reference/commands.md#sandbox-vm) process. It reads the output of the
    commands that it runs in the VM and removes control characters.
  - `ssh`: in the [`sandbox-vm`](reference/commands.md#sandbox-vm) process, in the tasks and in your
    own sessions.
  - `git`, when it fetches from the VM, and all other programs that copy files from the VM.
- The process creates a new VM with one of two procedures. Refer to
  [How the process creates a new VM](#how-the-process-creates-a-new-vm).
  - By default, Nix on the host builds a disk image of the NixOS configuration in the `sandbox-vm/`
    directory of your repo. The build uses a Linux builder.
  - With [`services.sandbox-vm.installer.iso`](reference/options.md#installer-iso), the VM boots a
    NixOS installer ISO one time. The installer installs the NixOS configuration. This procedure is
    easier if Nix and devenv are new to you, as it does not require setting up a Linux builder.
- The process does not change the NixOS system of an existing VM. To apply a change to
  `sandbox-vm/`, run the [`sandbox-vm:update`](reference/commands.md#task-update) task. The VM
  builds its new system and changes to it immediately, without a restart. Your repos and all other
  data in the VM stay the same. Refer to [How to change the system of the VM](vm-system.md).
- devenv-sandbox-vm does not clone a repo into the VM. The VM is a normal SSH host. You decide how
  your repos get there.
- Apple marks `sandbox-exec` as deprecated, but there is no replacement for it. Other sandbox tools
  on macOS, for example Nix, Homebrew, the common coding agents, also use it. The sandbox of the
  macOS kernel (Seatbelt), which `sandbox-exec` uses, is still supported, and macOS uses it for its
  own services.

## How the process creates a new VM

There are two procedures. [`services.sandbox-vm.installer.iso`](reference/options.md#installer-iso)
selects the procedure.

|                                                       | Host build (default)                | Installer ISO                |
| ----------------------------------------------------- | ----------------------------------- | ---------------------------- |
| [`installer.iso`](reference/options.md#installer-iso) | `null`                              | the path of the ISO          |
| The host must have                                    | a Linux builder for Nix             | the ISO file (1.8 GB)        |
| The software comes from                               | the Nix store of the host           | downloads in the VM          |
| A second new VM                                       | downloads only the changed software | downloads all software again |
| The host evaluates                                    | the flake in `sandbox-vm/`          | nothing                      |

**Host build.** Nix on the host builds a disk image of the NixOS configuration, from the committed
files in `sandbox-vm/`. Nix downloads only the software that is not in the Nix store of the host,
and keeps it there. Thus all new VMs on this computer share the downloads, also VMs of other
projects.

- The build needs a Linux builder for `aarch64-linux`, because a Mac cannot build a Linux system.
  With [nix-darwin](https://github.com/nix-darwin/nix-darwin), `nix.linux-builder.enable = true`
  gives you one. If Nix has no such builder, the process exits and shows the two procedures.
- **The host uses the builder only when it creates a VM.** When you change `sandbox-vm/` later, the
  VM builds the update of its system. The host then does not use the builder and does not evaluate
  the flake. Refer to [How to change the system of the VM](vm-system.md).
- The image stays in the Nix store until you run the Nix garbage collector. Each version of
  `sandbox-vm/` that you create a VM from has its own image. The size of an image is several GB.

WARNING: With this procedure, Nix on the host evaluates the flake in `sandbox-vm/`, and the builder
builds it. Before you create a new VM, review the changes to `sandbox-vm/` that come from the VM,
the same as changes to the [`proxy`](reference/options.md#proxy) options.

**Installer ISO.** Download the minimal ISO for `aarch64-linux` from
[nixos.org](https://nixos.org/download/#nixos-iso). Then set its path:

```nix
services.sandbox-vm.installer.iso = "~/Downloads/nixos-minimal-26.05-aarch64-linux.iso";
```

A new VM boots a copy of the ISO one time. The host sends the `sandbox-vm/` directory to the
installer, and the installer installs NixOS. The VM downloads or builds all software through the
proxy. The host does not evaluate the flake and uses no Linux builder. The host does not change the
ISO file.

Use a current NixOS ISO. The author tested the NixOS 26.05 minimal ISO.
