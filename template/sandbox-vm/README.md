# sandbox-vm

This directory contains the NixOS system of the VM of this project. The VM comes from
[devenv-sandbox-vm](https://github.com/jengdal/devenv-sandbox-vm). The VM builds its system from
the committed files in this directory:

- `flake.nix`: the inputs (nixpkgs, devenv-sandbox-vm and the devenv CLI of the VM).
- `configuration.nix`: the additions to the VM for this project.
- `flake.lock`: the locked versions of the inputs.

Run all commands on the host, in the root directory of the project.

## Requirements

- A Mac with Apple silicon.
- Nix with flakes enabled, and devenv.
- To create a new VM: a Linux builder for `aarch64-linux` or a NixOS installer ISO. Refer to the
  [documentation of devenv-sandbox-vm](https://jengdal.github.io/devenv-sandbox-vm/quick-start.html#requirements).

## Setup

1. Add this line at the top of `~/.ssh/config`:

   ```
   Include ~/.devenv-sandbox-vm/*/ssh.config
   ```

2. Start the VM:

   ```sh
   devenv up sandbox-vm
   ```

3. Wait until the message `[sandbox-vm] ready` shows. The first start creates the VM.
4. Connect to the VM. `<project>` is the name of the project directory.

   ```sh
   ssh devenv-<project>
   ```

When you stop `devenv up sandbox-vm`, the VM shuts down. The disk of the VM stays in
`~/.devenv-sandbox-vm/devenv-<project>/`.

## Change the system of the VM

1. Edit `configuration.nix` or `flake.nix`.
2. Commit the change. If this directory has changes that are not committed, the task fails.
3. While the VM is running, run:

   ```sh
   devenv tasks run sandbox-vm:update
   ```

The VM changes to the new system. The VM does not restart. Your repos and other files in the VM do
not change.

Do not put your personal tools and settings in this directory. Put them in a `devenv.local.nix`
file in your clone of the project in the VM. Do not commit that file.

## Update

To update all inputs, run these commands:

```sh
devenv update
nix flake update --flake ./sandbox-vm
```

To update only devenv-sandbox-vm, run these commands:

```sh
devenv update devenv-sandbox-vm
nix flake update --flake ./sandbox-vm devenv-sandbox-vm
```

Always update `devenv.lock` and `sandbox-vm/flake.lock` together. The host and the VM must use the
same version of devenv-sandbox-vm.

Then:

1. Commit the two lock files.
2. While the VM is running, run `devenv tasks run sandbox-vm:update`. The VM then uses the new
   version.
3. Restart `devenv up sandbox-vm`. The host then uses the new version. If the update has a new
   kernel, the VM then uses it.
