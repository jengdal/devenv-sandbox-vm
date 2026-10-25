# How to change the system of the VM

1. Change `sandbox-vm/` on the host: edit `sandbox-vm/configuration.nix`, or
   [update the inputs](#how-to-update-the-inputs-of-the-vm).
2. Commit the change.
3. While the VM is running, run this command on the host:

   ```bash
   devenv tasks run sandbox-vm:update
   ```

The VM builds its NixOS system from the committed `sandbox-vm/` directory on the host. Then the VM
changes to the new system immediately (`nixos-rebuild switch`).

- [`devenv up sandbox-vm`](reference/commands.md#sandbox-vm) does not update the system. A change to
  `sandbox-vm/` has no effect on an existing VM until you run the task
  ([`devenv tasks run sandbox-vm:update`](reference/commands.md#task-update)).
- If the system is up to date, the task does nothing.
- The VM uses a new kernel only after its next start. To get the new kernel, restart
  [`devenv up sandbox-vm`](reference/commands.md#sandbox-vm).
- The task fails if `sandbox-vm/` has changes that are not committed. This includes files that git
  does not track. Commit or revert them first.
- If the update fails, the task shows the error and the VM keeps its previous system. If the VM
  already changed to the new system, the task changes it back. A service of the new system that does
  not start is also a failure. Correct `sandbox-vm/`, commit, and run the task again.
- Changes to `sandbox-vm/` in a clone in the VM have no effect until they are on the host.
- The VM uses the time zone of the host. The process sends it at each boot of the VM. If you set
  `time.timeZone` in `sandbox-vm/configuration.nix`, the VM uses that value.

Note: This is an update of the installed system, the same as `nixos-rebuild` on a NixOS computer. It
changes the installed software and the system configuration. It does not create a new VM. The VM
builds the update. Home directories, your repos, deploy keys, `devenv.local.nix` and other data on
the disk do not change.

Use `sandbox-vm/` for the items that all users of the project must get. Put your own tools in the
[`devenv.local.nix`](personal-setup.md) of a repo in the VM, or use
[your Home Manager configuration](personal-setup.md#your-home-manager-configuration).

## How to update the inputs of the VM

The inputs of the VM, for example nixpkgs, are locked in `sandbox-vm/flake.lock`. To update them,
run this command on the host. Then commit the file and run
[`devenv tasks run sandbox-vm:update`](reference/commands.md#task-update), the same as for other
changes.

```bash
nix flake update --flake ./sandbox-vm nixpkgs       # one input; omit the name to update all inputs
```

The devenv CLI in the VM comes from the `nixpkgs-unstable` input (`pkgs.unstable.devenv` in
`sandbox-vm/configuration.nix`). To update it, run
`nix flake update --flake ./sandbox-vm nixpkgs-unstable`.
