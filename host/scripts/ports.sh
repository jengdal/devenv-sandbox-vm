#!/usr/bin/env bash
# Makes the forwards of the VM agree with services.sandbox-vm.expose and
# services.sandbox-vm.reach. It uses the control connection that the sandbox-vm process keeps
# open. It closes the forwards that are not necessary and opens the new forwards.
# $VM_DIR/forwards lists the forwards on that connection.
#
# The sandbox-vm process starts this script. The sandbox-vm:ports task also starts it, to
# apply changes while the VM runs. With --again, the script applies the forwards of the
# last run. The sandbox-vm process uses --again after it connects again, because its own copy
# of the options is from its start.
#
# The status is not 0 only if the script could not apply the forwards at all. A forward that
# it cannot open gets a message, and the status stays 0: the task runs at each start of
# the shell, and a port that is in use must not make that fail. Without a control connection
# the script does nothing, because the VM is not running. With --connected that is a
# failure: the sandbox-vm process uses it, because it opened the connection a moment ago.
#
# SVM_FORWARDS has one line for each forward, with these fields:
# - the ssh option: L from the host into the VM, R in the opposite direction
# - the name
# - the ssh forward
# - the host port that it binds, or "-"
# - a description for messages
set -euo pipefail
# shellcheck source=lib.sh
. "${BASH_SOURCE[0]%/*}/lib.sh"
! in_guest || exit 0

again='' connected=''
for arg in "$@"; do
  case "$arg" in
    --again) again=1 ;;
    --connected) connected=1 ;;
  esac
done

ctl() {
  ssh -F "$VM_DIR/ssh.config" -S "$VM_DIR/ctl" -o BatchMode=yes "$@" "$SVM_NAME" 2>/dev/null
}
# No control connection: the VM is not running, or it is not ready.
if ! ctl -O check; then
  [ -n "$connected" ] || exit 0
  echo "[sandbox-vm] the connection for the forwards does not answer" >&2
  exit 1
fi

# A command that fails after this point stops the script, for example when the script cannot
# write its files. The commands that open and close one forward do not: they have their own
# messages.
trap 'echo "[sandbox-vm] the forwards were not applied correctly" >&2' ERR

want=$SVM_FORWARDS
if [ -n "$again" ] && [ -e "$VM_DIR/wanted" ]; then
  want=$(cat "$VM_DIR/wanted")
fi
printf '%s\n' "$want" >"$VM_DIR/wanted"
# Write to stderr. devenv shows only stderr from a task.
exec >&2
touch "$VM_DIR/forwards"
open=
while read -r kind name spec port desc; do
  [ -n "$kind" ] || continue
  if grep -qxF "$kind $name $spec $port $desc" <<<"$want"; then
    open+="$kind $name $spec $port $desc"$'\n'
  else
    ctl -O cancel "-$kind" "$spec" || true
    echo "[sandbox-vm] $name: closed ($desc)"
  fi
done <"$VM_DIR/forwards"
while read -r kind name spec port desc; do
  [ -n "$kind" ] || continue
  ! grep -qxF "$kind $name $spec $port $desc" <<<"$open" || continue
  if [ "$port" != - ] && nc -z 127.0.0.1 "$port" 2>/dev/null; then
    echo "[sandbox-vm] $name: 127.0.0.1:$port is in use on this Mac; not forwarded"
  elif ctl -O forward "-$kind" "$spec"; then
    echo "[sandbox-vm] $name: $desc"
    open+="$kind $name $spec $port $desc"$'\n'
  elif [ "$kind" = R ]; then
    echo "[sandbox-vm] $name: could not be opened, is the port in use in the VM? ($desc)"
  else
    echo "[sandbox-vm] $name: could not be opened on this Mac ($desc)"
  fi
done <<<"$want"
printf '%s' "$open" >"$VM_DIR/forwards"
