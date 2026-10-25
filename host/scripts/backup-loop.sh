#!/usr/bin/env bash
# The sandbox-vm-backup process. While the VM is ready, it makes a backup when the last
# backup is older than services.sandbox-vm.backup.interval. $VM_DIR/backup.stamp has the
# time of the last backup. Thus a start of the VM does not cause one more backup.
set -euo pipefail
# shellcheck source=lib.sh
. "${BASH_SOURCE[0]%/*}/lib.sh"
require_host

repo=$(backup_repo)
echo "[sandbox-vm] backups: one each $SVM_BACKUP_INTERVAL_HOURS h, in $repo"
while :; do
  if [ -e "$VM_DIR/ready" ] \
    && [ -z "$(find "$VM_DIR/backup.stamp" -mmin "-$((SVM_BACKUP_INTERVAL_HOURS * 60))" 2>/dev/null)" ]; then
    "$BASH" "$SCRIPTS/backup.sh" || {
      echo "[sandbox-vm] the subsequent try is in 10 minutes" >&2
      sleep 600
    }
  fi
  sleep 60
done
