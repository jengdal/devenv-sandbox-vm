# Logs and backups

## Logs

The logs are in `~/.devenv-sandbox-vm/devenv-<project>/log/` on the host:

- `console/current`: the console of the VM. The output of
  [`devenv up sandbox-vm`](reference/commands.md#sandbox-vm) also shows the console. The log file
  has the same lines with the time.
- `gvproxy/current`: the log of the network of the VM.
- `smokescreen/current`: the log of the proxy. Look for `"allow":false` to find refused connections.

The VM controls the text of its console and the output of the commands that the host runs in it.
Thus the host removes control characters, escape sequences and colors from that text. The messages
of the [`sandbox-vm`](reference/commands.md#sandbox-vm) process start with `[sandbox-vm]`. A line
from the VM cannot start with this text: it gets a space before it. A long line from the VM that
your terminal wraps can still show `[sandbox-vm]` at the start of a row.

The VM controls how much text these logs get. Thus each log has a size limit,
[`services.sandbox-vm.logSize`](reference/options.md#logsize). When `current` is full, s6-log
renames it to a name that starts with `@`, and deletes the older `@` file.

## Backups

Backups are off by default. To enable them, set
[`services.sandbox-vm.backup.enable`](reference/options.md#backup-enable) in `devenv.nix`:

```nix
services.sandbox-vm.backup.enable = true;
```

Then start the VM together with the backup process:

```bash
devenv up sandbox-vm sandbox-vm-backup
```

- [`sandbox-vm-backup`](reference/commands.md#sandbox-vm-backup) is a second process. It waits until
  the VM is ready, and then it makes a backup when the last backup is older than
  [`backup.interval`](reference/options.md#backup-interval) hours.
- To make a backup immediately, run
  [`devenv tasks run sandbox-vm:backup`](reference/commands.md#task-backup). The VM must be
  running. The [`sandbox-vm-backup`](reference/commands.md#sandbox-vm-backup) process is not
  necessary for this command.
- A backup contains the directories in [`backup.paths`](reference/options.md#backup-paths) of the
  VM. The system is not in the backup, because you can build it again from `sandbox-vm/`.
- [`backup.exclude`](reference/options.md#backup-exclude) removes files from the backup.
- The backups are in a [borg](https://www.borgbackup.org) repository on the host, in
  `~/.devenv-sandbox-vm-backups/devenv-<project>/`. To use a different directory, set
  [`backup.repository`](reference/options.md#backup-repository).
- borg stores the data of a file only one time. Thus a second backup uses only the space of the
  files that changed.
- After each backup, borg deletes the older backups that the
  [`backup.keep`](reference/options.md#backup-keep-daily) rules do not keep.
- Changes to the [`backup` options](reference/options.md#backup) apply to the automatic backups when
  you restart the [`sandbox-vm-backup`](reference/commands.md#sandbox-vm-backup) process.

How it works: borg runs on the host. The VM sends its files as a tar stream through SSH, and
`borg import-tar` stores each file of the stream. Thus the VM has no access to the repository and
cannot change or delete old backups. borg runs in a sandbox, without a network, and can write only
to the repository and to its cache.

Limits:

- The repository is not encrypted. The disk of the VM on the host is also not encrypted.
- The VM runs during a backup. A file that changes during the backup can be in the backup in a state
  that is not consistent. For an exact copy of a database, stop the database first.
- The VM controls the data that it sends for a backup. As a precaution, a backup has a size limit,
  [`backup.maxSize`](reference/options.md#backup-maxsize). The default is the disk size of the VM
  ([`services.sandbox-vm.disk`](reference/options.md#disk)). A backup that is larger fails, and the
  older backups stay.

### How to restore files

When you enable backups, the devenv shell on the host has the `borg` command. The first time, borg
asks if it can use a repository that is not encrypted. Answer `y`.

The VM controls the files of a backup: their contents, names, symbolic links and permissions.
Extract them into a new empty directory, and examine them before you use them on the host.

For the commands, refer to the [borg documentation](https://borgbackup.readthedocs.io).
