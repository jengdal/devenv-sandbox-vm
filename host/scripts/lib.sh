#!/usr/bin/env bash
# Definitions for all scripts in this directory. Each script reads this file first.
#
# host/scripts.nix starts the scripts and the supervisor (host/supervisor, the program
# of the sandbox-vm process). It gives them the options of services.sandbox-vm in environment
# variables. The names of these variables start with SVM_:
#
#   SVM_NAME, SVM_HOSTNAME, SVM_USER      name of the VM; hostname and login name in the guest
#                                         (SVM_USER can be empty: the user name on the host)
#   SVM_ROOT, SVM_CONFIG_DIR,             the repo on the host, and the flake of the guest in it
#   SVM_CONFIG_NAME
#   SVM_CPUS, SVM_MEMORY_MIB, SVM_DISK_GB the size of the VM
#   SVM_LOG_BYTES                         the size limit of one log file
#   SVM_FORWARDS                          the forwards of `expose` and `reach` (refer to ports.sh)
#   SVM_HOME_MANAGER                      the home-manager configuration for the guest,
#                                         <flake>#<name>; empty for none (refer to home.sh)
#   SVM_GUEST_MARKER, SVM_GUEST_STATE,    values that the guest module also has (constants.nix)
#   SVM_GUEST_MAC, SVM_PROXY_GATEWAY,
#   SVM_TIMESYNC_PORT
#   SVM_SUPERVISOR,                       the supervisor, and the filter
#   SVM_CONSOLE_FILTER                    for the text of the guest (safe_text). Empty on a
#                                         computer where the VM cannot run.
#
# Only for the sandbox-vm process:
#
#   SVM_INSTALLER_ISO                     the installer ISO for a new VM. Empty: the host
#                                         builds a disk image on a Linux builder.
#   SVM_GUEST_ARCH                        the architecture of the VM, as `uname -m` gives it
#   SVM_SUBSTITUTERS,                     the binary caches for the build of the disk image
#   SVM_TRUSTED_PUBLIC_KEYS
#   SVM_NIX_OPTIONS                       options for nixos-install, as shell words
#   SVM_SMOKESCREEN, SVM_GVPROXY,         the programs that run in a sandbox. For each one,
#   SVM_S6_LOG, SVM_*_STORE_PATHS         a file lists the store paths that it can read.
#   SVM_SMOKESCREEN_CONFIG                the configuration file of the proxy
#   SVM_PORTS                             the program that applies the forwards (ports.sh)
#   SVM_HOME_PROGRAM                      the program that applies the home-manager
#                                         configuration (home.sh); empty for none
#   SVM_KRUNKIT, SVM_KRUNKIT_STORE_PATHS  the hypervisor, and the store paths that it can read
#                                         in its sandbox
#
# Only for backups:
#
#   SVM_BORG, SVM_BORG_STORE_PATHS        borg and the store paths that it can read
#   SVM_BACKUP_REPO                       the repository; empty for the default
#   SVM_BACKUP_COMMAND                    the command in the guest that writes the tar stream
#   SVM_BACKUP_MAX_BYTES                  the size limit of that stream
#   SVM_BACKUP_COMPRESSION,               options of borg
#   SVM_BACKUP_KEEP
#   SVM_BACKUP_INTERVAL_HOURS             the time between two automatic backups
#
# The directory where the host keeps all data for this VM.
VM_DIR="$HOME/.devenv-sandbox-vm/$SVM_NAME"
# The directory of the scripts. The scripts that read this file use it.
# shellcheck disable=SC2034
SCRIPTS=${BASH_SOURCE[0]%/*}

# The clone in the VM has the same devenv.nix, thus the clone also enables this service. The
# NixOS module of the guest creates a marker file. This function finds the guest by that file.
in_guest() {
  [ -e "$SVM_GUEST_MARKER" ]
}

# For the scripts that must run on a host. Exits in the guest and on a host that is not
# supported.
require_host() {
  if in_guest; then
    echo "[sandbox-vm] this is the VM itself; start it from the host" >&2
    exit 1
  fi
  if [ -z "${SVM_SUPERVISOR:-}" ]; then
    echo "[sandbox-vm] only Apple silicon Macs are supported as hosts" >&2
    exit 1
  fi
}

vm_ssh() {
  ssh -F "$VM_DIR/ssh.config" -o BatchMode=yes -o ConnectTimeout=5 "$SVM_NAME" "$@"
}

# Prints a word as a shell word in single quotes, for a command that ssh gives to the shell of
# the guest.
shell_quote() {
  printf "'%s'" "${1//\'/\'\\\'\'}"
}

# Prints the address of the proxy as the guest sees it. Thus a command in the guest does not
# depend on the configuration of the guest. $VM_DIR/env has the port of the proxy of this run.
# The file is shell text for the guest; the host reads only the port and does not run the file.
guest_proxy() {
  local port
  port=$(sed -n 's/^proxy_port="\([0-9][0-9]*\)"$/\1/p' "$VM_DIR/env" 2>/dev/null) || port=
  if [ -z "$port" ]; then
    echo "[sandbox-vm] $VM_DIR/env does not have the port of the proxy; start the VM again" >&2
    return 1
  fi
  printf '%s\n' "http://$SVM_PROXY_GATEWAY:$port"
}

# Passes text of the guest, without the bytes that a terminal must not get: control
# characters and escape sequences (host/supervisor/internal/termsafe). The guest controls
# the output of its commands. Thus all output of the guest that the user sees goes through
# this function. Output that a script only compares or stores does not. -lines: a progress
# counter that writes over its line shows only its last value.
safe_text() {
  "$SVM_CONSOLE_FILTER" -lines
}

# Prints the directory of the borg repository. The default is not in $VM_DIR. Thus the backups
# stay when you remove the VM.
backup_repo() {
  local repo=${SVM_BACKUP_REPO:-"$HOME/.devenv-sandbox-vm-backups/$SVM_NAME"}
  case "$repo" in
    \~/*) repo="$HOME/${repo#\~/}" ;;
  esac
  # A relative path is a different directory for each directory that a script starts in.
  case "$repo" in
    /*) ;;
    *)
      echo "[sandbox-vm] services.sandbox-vm.backup.repository is not an absolute path: $repo" >&2
      return 1
      ;;
  esac
  printf '%s\n' "$repo"
}
