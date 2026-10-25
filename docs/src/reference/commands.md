<!-- Made by docs/reference.py from host/commands.nix. Do not edit. -->

# Processes and tasks

Run these commands on the host, in the directory of your project.

## Processes

### `sandbox-vm` {#sandbox-vm}

Creates, configures and runs the VM. Start it with `devenv up sandbox-vm`. `devenv up`
without `sandbox-vm` does not start it.

The first start creates the VM, which takes a few minutes. The process is ready when the
VM accepts SSH connections and has the settings of the host. When you stop the process,
the VM shuts down. If the VM does not shut down in 45 seconds, the process powers it off.
If the VM fails to install or start, devenv does not start the process again.

A process on the host can wait for the VM with `after = [ "devenv:processes:sandbox-vm" ]`.
This does not start the VM. Start the two processes together:
`devenv up sandbox-vm <process>`.

### `sandbox-vm-backup` {#sandbox-vm-backup}

Makes the automatic backups. It exists only if [`backup.enable`](options.md#backup-enable) is true. Start it together
with the VM: `devenv up sandbox-vm sandbox-vm-backup`.

It waits until the VM is ready, but it does not start the VM. Then it makes a backup when
the last backup is older than [`backup.interval`](options.md#backup-interval) hours. Changes to the [`backup`](options.md#backup) options
apply when you restart `devenv up sandbox-vm sandbox-vm-backup`.

## Tasks

### `sandbox-vm:backup` {#task-backup}

```bash
devenv tasks run sandbox-vm:backup
```

Makes a backup now. It exists only if [`backup.enable`](options.md#backup-enable) is true. The VM must be running;
the [`sandbox-vm-backup`](#sandbox-vm-backup) process does not need to be running. The time of the last backup has
no effect.

### `sandbox-vm:home` {#task-home}

```bash
devenv tasks run sandbox-vm:home
```

Applies [`homeManager`](options.md#homemanager) to the login user in the VM. The VM must be running. The process
also does this when it creates the VM, and [`sandbox-vm:update`](#task-update) after each update.

### `sandbox-vm:ports` {#task-ports}

```bash
devenv tasks run sandbox-vm:ports
```

Applies [`expose`](options.md#expose) and [`reach`](options.md#reach) to the running VM. It also runs each time you enter the
devenv shell. If the VM is not running, it does nothing.

### `sandbox-vm:update` {#task-update}

```bash
devenv tasks run sandbox-vm:update
```

Updates the NixOS system of the VM from the committed [`configDir`](options.md#configdir). The VM must be
running. The VM changes to the new system immediately, without a restart.

The [`sandbox-vm`](#sandbox-vm) process does not update the system: a change to [`configDir`](options.md#configdir) has no effect
until you run this task. The task fails if [`configDir`](options.md#configdir) has changes that are not committed.
After the update, it applies [`homeManager`](options.md#homemanager).
