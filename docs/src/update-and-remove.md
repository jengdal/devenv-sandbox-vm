# Update and remove

## How to update devenv-sandbox-vm

Your project locks this repo in two files: `devenv.lock` (for the host) and `sandbox-vm/flake.lock`
(for the VM).

CAUTION: Always update the two files together. The host module and the VM module must have the same
version. If you update only one file, some functions can fail.

```bash
devenv update devenv-sandbox-vm                         # host -> devenv.lock
nix flake update --flake ./sandbox-vm devenv-sandbox-vm # VM   -> sandbox-vm/flake.lock
git commit devenv.lock sandbox-vm/flake.lock -m "Update devenv-sandbox-vm"
```

Then:

1. While the VM is running, run
   [`devenv tasks run sandbox-vm:update`](reference/commands.md#task-update). The VM then uses the
   new VM module.
2. Restart [`devenv up sandbox-vm`](reference/commands.md#sandbox-vm). The host then uses the new
   host module. If the update has a new kernel, the VM then uses it.

## How to remove the VM

WARNING: Before you delete the VM, move out all data that you want to keep, such as:

- Commits that you did not fetch or push.
- Changes that are not committed.
- Files that are only in the VM, such as `devenv.local.nix` and deploy keys.

If you enabled [backups](logs-and-backups.md#backups), run
[`devenv tasks run sandbox-vm:backup`](reference/commands.md#task-backup) first. The backups stay
when you delete the VM.

1. Stop [`devenv up sandbox-vm`](reference/commands.md#sandbox-vm).
2. Delete the `~/.devenv-sandbox-vm/devenv-<project>` directory.
3. If the VM had a deploy key, also remove the key from the repo on your forge.
4. To also delete the backups, delete `~/.devenv-sandbox-vm-backups/devenv-<project>`.

After that, [`devenv up sandbox-vm`](reference/commands.md#sandbox-vm) creates a new VM.
