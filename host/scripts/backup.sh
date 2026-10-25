#!/usr/bin/env bash
# Makes one backup. The sandbox-vm-backup process starts this script, and the
# sandbox-vm:backup task also starts it.
#
# borg runs on the host. The guest sends its files as a tar stream through SSH, and
# `borg import-tar` stores each file of the stream in an archive. Thus the guest gets no
# access to the repository, and you can restore a single file.
#
# While borg reads the stream, the archive has a name that ends with ".partial". It gets its
# final name only when the stream is complete. A run that stopped early leaves a ".partial"
# archive, and the subsequent run deletes it.
set -euo pipefail
# shellcheck source=lib.sh
. "${BASH_SOURCE[0]%/*}/lib.sh"
require_host

# Write to stderr. devenv shows only stderr from a task.
exec >&2
if [ ! -e "$VM_DIR/ready" ]; then
  echo "[sandbox-vm] the VM is not ready; no backup was made"
  exit 1
fi
repo=$(backup_repo)
[ -d "$repo" ] || {
  mkdir -p "$repo"
  chmod 700 "$repo"
}
mkdir -p "$VM_DIR/borg/tmp"
# The rules of the sandbox use paths without symbolic links.
repo=$(cd "$repo" && pwd -P)
borg_dir=$(cd "$VM_DIR/borg" && pwd -P)

# borg keeps its cache and its temporary files in $borg_dir. A new VM can use a repository
# that exists, thus borg must not ask about a repository that it does not know.
export BORG_BASE_DIR="$borg_dir" TMPDIR="$borg_dir/tmp"
export BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes BORG_RELOCATED_REPO_ACCESS_IS_OK=yes
export USER="${USER:-$(id -un)}"
cd "$borg_dir"
# borg reads bytes from the guest, thus it has a sandbox: no network, and it can write only
# the repository and its own directory. borg is a script that starts Python, thus it can start
# the programs of its own store paths. It gets only the environment variables that are
# named here, and not the passwords and keys of the shell of the project. It records the
# user name in each archive, and in the sandbox it gets the name only from USER. Its messages
# can contain the names of files of the guest, thus they go through safe_text.
borg() {
  "$SVM_SUPERVISOR" confine --egress none --store-paths "$SVM_BORG_STORE_PATHS" --exec-closure \
    --write-dir "$repo" --write-dir "$borg_dir" \
    --env BORG_BASE_DIR --env BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK \
    --env BORG_RELOCATED_REPO_ACCESS_IS_OK --env USER -- "$SVM_BORG" "$@" 2>&1 | safe_text
}

# Passes the bytes of the stream, and fails if the stream has more than $1 bytes. The guest
# controls the stream: without a limit, the guest can fill the disk of the host.
limit_bytes() {
  head -c "$1"
  if [ "$(head -c 1 | wc -c)" -ne 0 ]; then
    echo "[sandbox-vm] the backup is larger than services.sandbox-vm.backup.maxSize" >&2
    return 1
  fi
}

[ -e "$repo/config" ] || borg init --encryption=none "$repo"
borg delete --glob-archives '*.partial*' "$repo"
name=$(date +%Y-%m-%dT%H:%M:%S)
echo "[sandbox-vm] backup $name: started"
# With pipefail, a connection that stops or a stream that is too large also makes the backup
# fail. borg alone accepts a stream that ends between two files.
if ! vm_ssh "$SVM_BACKUP_COMMAND" </dev/null 2> >(safe_text >&2) \
  | limit_bytes "$SVM_BACKUP_MAX_BYTES" \
  | borg import-tar --compression "$SVM_BACKUP_COMPRESSION" "$repo::$name.partial" -; then
  borg delete --glob-archives '*.partial*' "$repo" || true
  echo "[sandbox-vm] backup $name: failed"
  exit 1
fi
borg rename "$repo::$name.partial" "$name"
# Delete the older backups that no rule keeps. The pattern is for the final names only. They
# end with a digit.
read -ra keep <<<"$SVM_BACKUP_KEEP"
[ "${#keep[@]}" = 0 ] || borg prune --glob-archives '*[0-9]' "${keep[@]}" "$repo"
borg compact "$repo"
touch "$VM_DIR/backup.stamp"
echo "[sandbox-vm] backup $name: done ($repo)"
