<!-- Made by docs/reference.py from host/options.nix. Do not edit. -->

# Options

Set these options in the `devenv.nix` of your project. All options are optional, except
`services.sandbox-vm.enable`.

## General

### `services.sandbox-vm.configDir` {#configdir}

The directory in the repo that contains the NixOS flake of the guest. The system of
the VM comes from the committed files in this directory. When these files change,
run [`devenv tasks run sandbox-vm:update`](commands.md#task-update) to update the installed system. Data in the
VM does not change. The task fails if the directory has changes that are not
committed.

- Type: `string matching the pattern [A-Za-z0-9_.][A-Za-z0-9_./-]*`
- Default: `"sandbox-vm"`

### `services.sandbox-vm.configName` {#configname}

The prefix of the nixosConfigurations (`prefix` of mkVms). The guest builds
`<configName>-<uname -m>`.

- Type: `string matching the pattern [A-Za-z0-9_][A-Za-z0-9_.-]*`
- Default: `"devenv-sandbox-vm"`

### `services.sandbox-vm.cpus` {#cpus}

The number of virtual CPUs.

- Type: `positive integer, meaning >0`
- Default: `4`

### `services.sandbox-vm.disk` {#disk}

The disk size in GiB. The process applies it when it creates the VM. If you change it
later, an existing VM does not change.

- Type: `positive integer, meaning >0`
- Default: `100`

### `services.sandbox-vm.enable` {#enable}

Whether to enable a NixOS VM for this devenv.

- Type: `boolean`
- Default: `false`
- Example: `true`

### `services.sandbox-vm.homeManager` {#homemanager}

Your home-manager configuration for the login user in the VM, as
`<flake>#<configuration>`: the name in `homeConfigurations` of the flake. The VM
builds and activates it when the process creates the VM, and at each
[`devenv tasks run sandbox-vm:update`](commands.md#task-update). To apply it at other times, run
[`devenv tasks run sandbox-vm:home`](commands.md#task-home).

The configuration is yours. Thus set it in `devenv.local.nix`. The VM gets the flake
through the proxy, and the flake uses its own flake.lock. A private repo or an SSH
URL needs credentials in the VM.

- Type: `null or string matching the pattern [^#]+#[A-Za-z0-9@._+-]+`
- Default: `null`
- Example: `"github:me/dotfiles#me@sandbox-aarch64-linux"`

### `services.sandbox-vm.hostName` {#hostname}

The hostname of the guest. The host sets it at run time. If you set
`networking.hostName` in the NixOS configuration of the guest, the guest uses that value.

- Type: `string matching the pattern [A-Za-z0-9]([A-Za-z0-9.-]{0,62}[A-Za-z0-9])?`
- Default: `"<project>"`

### `services.sandbox-vm.installer.iso` {#installer-iso}

The path of a NixOS installer ISO on the host, for the architecture of the VM. `~/` at
the start is your home directory. Get the minimal ISO from https://nixos.org/download.

With null, the host builds a disk image of the system on a Linux builder, and a new
VM gets a copy. Nix keeps the software of the system in its store on the host. Thus
Nix downloads only the changed software for a subsequent new VM. The host uses the
builder only when it creates a VM.

With a path, a new VM boots the ISO one time and the installer installs the system.
The VM downloads or builds all software, and the host uses no Linux builder. The
path is different for each person. Thus set it in `devenv.local.nix`.

- Type: `null or string`
- Default: `null`
- Example: `"~/Downloads/nixos-minimal-26.05-aarch64-linux.iso"`

### `services.sandbox-vm.logSize` {#logsize}

The size limit in MiB for each log of the VM (console, gvproxy and proxy). Each log
keeps its current file and one older file. Thus a log uses two times this size at
most.

- Type: `integer between 1 and 128 (both inclusive)`
- Default: `20`

### `services.sandbox-vm.memory` {#memory}

The memory in GiB.

- Type: `positive integer, meaning >0`
- Default: `8`

### `services.sandbox-vm.name` {#name}

The name of the VM. It is the SSH host alias and the directory name in
~/.devenv-sandbox-vm. It has only letters, digits, `.`, `_` and `-`.

Two projects in directories with the same name get the same default name. The
process of the second project then exits with a message, and it does not start the
VM of the first project. Set this option in one of the projects. If you move a
project to a different directory, remove the file
`~/.devenv-sandbox-vm/devenv-<project>/root`.

- Type: `string matching the pattern [A-Za-z0-9][A-Za-z0-9._-]*`
- Default: `"devenv-<project>"`

### `services.sandbox-vm.user` {#user}

The user name for the login. The guest creates the account at its first boot. The
default is your user name on the host. Thus the account is not in the NixOS
configuration, and each person on the project can have a different name.

Set it if your user name on the host is not a valid Linux user name, for example if
it has capital letters or dots. To set it only for yourself, use `devenv.local.nix`.
Do not change it for an existing VM: the VM has no account with the new name, so the
process cannot log in. To use a different name, create a new VM.

- Type: `null or string`
- Default: `null`

## Proxy

### `services.sandbox-vm.proxy.acl` {#proxy-acl}

More settings for the ACL file of smokescreen. They have priority over the
settings that this module sets. They can make the proxy less safe.

- Type: `YAML 1.1 value`
- Default: `{ }`

### `services.sandbox-vm.proxy.allowRanges` {#proxy-allowranges}

Address ranges (CIDR) that the proxy permits. This has priority over [`denyRanges`](#proxy-denyranges) and
over the rule that refuses private addresses. Thus it can open your local network
to the VM.

- Type: `list of string`
- Default: `[ ]`

### `services.sandbox-vm.proxy.allowedDomains` {#proxy-alloweddomains}

The host names that the VM can connect to. `*.example.com` matches the subdomains
of example.com, but not example.com. With null, the VM can connect to all hosts on
the internet. With a list, the proxy refuses all other hosts. The proxy always
refuses the host. It also refuses private addresses, except for [`allowedLocalDomains`](#proxy-allowedlocaldomains)
and [`allowRanges`](#proxy-allowranges).

- Type: `null or (list of string)`
- Default: `null`
- Example:

  ```nix
  [
    "github.com"
    "*.github.com"
    "cache.nixos.org"
  ]
  ```

### `services.sandbox-vm.proxy.allowedLocalDomains` {#proxy-allowedlocaldomains}

Host names that the VM can connect to also when they resolve to a private IP
address, for example a git server or a cache on your local network. `*.` works as in
[`allowedDomains`](#proxy-alloweddomains). The proxy still refuses this computer.

- Type: `list of string`
- Default: `[ ]`
- Example:

  ```nix
  [
    "git.home.example"
  ]
  ```

### `services.sandbox-vm.proxy.deniedDomains` {#proxy-denieddomains}

Host names that the proxy always refuses, also when [`allowedDomains`](#proxy-alloweddomains) is null. `*.`
works as in [`allowedDomains`](#proxy-alloweddomains).

- Type: `list of string`
- Default: `[ ]`

### `services.sandbox-vm.proxy.denyRanges` {#proxy-denyranges}

Address ranges (CIDR) that the proxy refuses in addition to private addresses. Use
this for networks with public addresses that only this computer can connect to,
for example through a VPN.

- Type: `list of string`
- Default: `[ ]`
- Example:

  ```nix
  [
    "203.0.113.0/24"
  ]
  ```

### `services.sandbox-vm.proxy.ipv6` {#proxy-ipv6}

Whether the proxy also connects to IPv6 addresses. On a network with public IPv6
addresses, the proxy cannot know which addresses are computers of your local
network. Thus with true, the VM can connect to these computers.

- Type: `boolean`
- Default: `false`

### `services.sandbox-vm.proxy.settings` {#proxy-settings}

More settings for the configuration file of smokescreen. They have priority over
the settings that this module sets, but not over `ip` and `acl_file`. They can
make the proxy less safe.

- Type: `YAML 1.1 value`
- Default: `{ }`
- Example:

  ```nix
  {
    connect_timeout = "10s";
  }
  ```

## Ports

### `services.sandbox-vm.expose` {#expose}

Ports in the VM that the host makes available on its address 127.0.0.1. Each port has a
name. A number sets the same port on the two sides. The host opens an SSH tunnel into
the VM for each port. To apply changes while the VM runs, run
[`devenv tasks run sandbox-vm:ports`](commands.md#task-ports) or enter the shell again.

- Type: `attribute set of ((submodule) or 16 bit unsigned integer; between 0 and 65535 (both inclusive) convertible to it)`
- Default: `{ }`
- Example:

  ```nix
  {
    web = 3000;
    db = { guest = 5432; host = 15432; };
  }
  ```

### `services.sandbox-vm.expose.<name>.guest` {#expose-name-guest}

The port in the VM.

- Type: `16 bit unsigned integer; between 0 and 65535 (both inclusive)`

### `services.sandbox-vm.expose.<name>.host` {#expose-name-host}

The port on 127.0.0.1 of the host.

- Type: `16 bit unsigned integer; between 0 and 65535 (both inclusive)`
- Default: `guest`

### `services.sandbox-vm.reach` {#reach}

Services that the VM can connect to, as exceptions to its network rules. Each
service has a name and is one port on one host. The VM gets the service on its
address 127.0.0.1. The host makes the connection for the VM through an SSH tunnel.
Thus a service can also be on your local network or on the host itself. To apply
changes, do the same as for [`expose`](#expose).

- Type: `attribute set of (submodule)`
- Default: `{ }`
- Example:

  ```nix
  {
    internal = { host = "my-internal-service"; port = 8080; };
  }
  ```

### `services.sandbox-vm.reach.<name>.guest` {#reach-name-guest}

The port on 127.0.0.1 of the VM.

- Type: `16 bit unsigned integer; between 0 and 65535 (both inclusive)`
- Default: `port`

### `services.sandbox-vm.reach.<name>.host` {#reach-name-host}

The host name or IPv4 address. The host resolves the name and makes the
connection.

- Type: `string matching the pattern [A-Za-z0-9.-]+`

### `services.sandbox-vm.reach.<name>.port` {#reach-name-port}

The port on that host.

- Type: `16 bit unsigned integer; between 0 and 65535 (both inclusive)`

## Backup

### `services.sandbox-vm.backup.compression` {#backup-compression}

The compression of borg (`--compression`).

- Type: `string`
- Default: `"zstd"`

### `services.sandbox-vm.backup.enable` {#backup-enable}

Whether to enable backups of the files of the VM with borg.

- Type: `boolean`
- Default: `false`
- Example: `true`

### `services.sandbox-vm.backup.exclude` {#backup-exclude}

Patterns for files that a backup does not contain (`--exclude` of GNU tar). The
names in a backup have no `/` at the start.

- Type: `list of string`
- Default: `[ ]`
- Example:

  ```nix
  [
    "home/*/.cache"
    "node_modules"
  ]
  ```

### `services.sandbox-vm.backup.interval` {#backup-interval}

The time in hours between two backups of the sandbox-vm-backup process.

- Type: `positive integer, meaning >0`
- Default: `24`

### `services.sandbox-vm.backup.keep.daily` {#backup-keep-daily}

The number of daily backups to keep (`--keep-daily` of `borg prune`).
Each backup deletes the older backups that no rule keeps. If all values are 0, borg
deletes no backup.

- Type: `unsigned integer, meaning >=0`
- Default: `7`

### `services.sandbox-vm.backup.keep.hourly` {#backup-keep-hourly}

The number of hourly backups to keep (`--keep-hourly` of `borg prune`).
Each backup deletes the older backups that no rule keeps. If all values are 0, borg
deletes no backup.

- Type: `unsigned integer, meaning >=0`
- Default: `0`

### `services.sandbox-vm.backup.keep.monthly` {#backup-keep-monthly}

The number of monthly backups to keep (`--keep-monthly` of `borg prune`).
Each backup deletes the older backups that no rule keeps. If all values are 0, borg
deletes no backup.

- Type: `unsigned integer, meaning >=0`
- Default: `6`

### `services.sandbox-vm.backup.keep.weekly` {#backup-keep-weekly}

The number of weekly backups to keep (`--keep-weekly` of `borg prune`).
Each backup deletes the older backups that no rule keeps. If all values are 0, borg
deletes no backup.

- Type: `unsigned integer, meaning >=0`
- Default: `4`

### `services.sandbox-vm.backup.keep.yearly` {#backup-keep-yearly}

The number of yearly backups to keep (`--keep-yearly` of `borg prune`).
Each backup deletes the older backups that no rule keeps. If all values are 0, borg
deletes no backup.

- Type: `unsigned integer, meaning >=0`
- Default: `0`

### `services.sandbox-vm.backup.maxSize` {#backup-maxsize}

The size limit in GiB for the data that the VM sends for one backup. A backup that
is larger fails. The VM controls what it sends, thus without a limit it can fill
the disk of the host. With null, the limit is the disk size of the VM
([`services.sandbox-vm.disk`](#disk)).

- Type: `null or (positive integer, meaning >0)`
- Default: `null`

### `services.sandbox-vm.backup.paths` {#backup-paths}

The directories in the VM that a backup contains. A backup does not go into other
file systems that the VM mounts below these directories.

- Type: `list of string matching the pattern /.*`
- Default:

  ```nix
  [
    "/home"
  ]
  ```

### `services.sandbox-vm.backup.repository` {#backup-repository}

The directory on the host for the borg repository, as an absolute path. The process
creates the repository if it does not exist. `~/` at the start is your home
directory. A relative path is not permitted. With null, the directory is
`~/.devenv-sandbox-vm-backups/<name>`. The default is not in the directory of the VM.
Thus the backups stay when you remove the VM.

- Type: `null or string matching the pattern (/|~/).*`
- Default: `null`
- Example: `"/Volumes/Backup/sandbox-vm"`
