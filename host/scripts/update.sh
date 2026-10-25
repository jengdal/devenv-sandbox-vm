#!/usr/bin/env bash
# Updates the NixOS system of the VM: the sandbox-vm:update task. The VM builds the system
# from the committed configuration directory of the host and changes to it immediately,
# without a restart (`nixos-rebuild switch`). The new system is also the system of the
# subsequent starts. The sandbox-vm process does not update the system.
#
# The file `provisioned` in the guest records the git tree of the last build.
set -euo pipefail
# shellcheck source=lib.sh
. "${BASH_SOURCE[0]%/*}/lib.sh"
require_host

# Write to stderr. devenv shows only stderr from a task.
exec >&2
if [ ! -e "$VM_DIR/ready" ]; then
  echo "[sandbox-vm] the VM is not ready; the system was not updated"
  exit 1
fi
# The VM builds the system only from the committed files. Thus the directory must not
# contain other changes.
if [ -n "$(git -C "$SVM_ROOT" status --porcelain -- "$SVM_CONFIG_DIR")" ]; then
  echo "[sandbox-vm] $SVM_CONFIG_DIR/ has uncommitted changes; commit or revert them first"
  exit 1
fi
# "./": the path is in $SVM_ROOT. Without it, git reads the path from the top of the repo, and
# the project can be in a directory below that.
tree=$(git -C "$SVM_ROOT" rev-parse "HEAD:./$SVM_CONFIG_DIR") || {
  echo "[sandbox-vm] $SVM_CONFIG_DIR/ must be committed"
  exit 1
}
provisioned=$SVM_GUEST_STATE/provisioned

# The home-manager configuration (home.sh) after the system, also if the system did not change:
# the configuration comes from a different flake.
apply_home() {
  if [ -n "${SVM_HOME_MANAGER:-}" ]; then
    "$BASH" "$SCRIPTS/home.sh"
  fi
}

if [ "$(vm_ssh cat "$provisioned" 2>/dev/null)" = "$tree" ]; then
  echo "[sandbox-vm] the system of the VM is up to date"
  apply_home
  exit 0
fi

echo "[sandbox-vm] $SVM_CONFIG_DIR/ has changed: updating the VM's system (its files are kept)"
src=$SVM_GUEST_STATE/src
proxy=$(guest_proxy) || exit 1

# The system that the guest runs now. On a failure, the script goes back to this system. The
# guest controls the output, thus the script accepts only a store path.
profile=/nix/var/nix/profiles/system
store_path='^/nix/store/[0-9a-z]{32}-[^/[:space:]]+$'
previous=$(vm_ssh readlink -f "$profile" 2>/dev/null) || previous=
if ! [[ $previous =~ $store_path ]]; then
  echo "[sandbox-vm] cannot read the current system of the VM; the system was not updated"
  exit 1
fi

# The archive has the files of $tree, and not those of HEAD at this moment. Thus the VM gets
# the files that it records as its version, also if a commit came after the check.
# git starts at the top of the repo: in a directory below it, the archive of a tree is empty.
top=$(git -C "$SVM_ROOT" rev-parse --show-toplevel)
git -C "$top" archive --prefix="$SVM_CONFIG_DIR/" "$tree" \
  | vm_ssh "sudo rm -rf $src && sudo mkdir $src && sudo tar -x -m -C $src" 2>&1 | safe_text
# The guest adds its architecture to the name. Status 4: the new system is active, but a
# unit of it did not start. That is also a failure.
status=0
# shellcheck disable=SC2029
vm_ssh "sudo env http_proxy=$proxy https_proxy=$proxy HTTPS_PROXY=$proxy nixos-rebuild switch" \
  "--flake \"path:$src/$SVM_CONFIG_DIR#$SVM_CONFIG_NAME-\$(uname -m)\"" </dev/null 2>&1 | safe_text \
  || status=${PIPESTATUS[0]}

if [ "$status" != 0 ]; then
  # nixos-rebuild changes the profile before it activates the new system. If the profile is
  # the same, the guest did not activate the new system.
  current=$(vm_ssh readlink -f "$profile" 2>/dev/null) || current=
  if [ "$current" = "$previous" ]; then
    echo "[sandbox-vm] system update failed; the VM is still on its previous system"
    exit 1
  fi
  # Go back to the exact previous system. `nixos-rebuild switch --rollback` goes back one
  # generation, and that is not always the previous system.
  echo "[sandbox-vm] system update failed; going back to the previous system"
  rollback=0
  # shellcheck disable=SC2029
  vm_ssh "sudo nix-env -p $profile --set $previous && sudo $previous/bin/switch-to-configuration switch" \
    </dev/null 2>&1 | safe_text || rollback=${PIPESTATUS[0]}
  # Status 4: the previous system is active again, but a unit of it did not start.
  if [ "$rollback" != 0 ] && [ "$rollback" != 4 ]; then
    echo "[sandbox-vm] the update failed, and the VM could not go back to its previous system"
    echo "[sandbox-vm] to go back manually, run in the VM: sudo nix-env -p $profile --set $previous && sudo $previous/bin/switch-to-configuration switch"
    exit 1
  fi
  echo "[sandbox-vm] the update failed; the VM is back on its previous system"
  exit 1
fi
printf '%s\n' "$tree" | vm_ssh "sudo tee $provisioned >/dev/null" 2>&1 | safe_text
echo "[sandbox-vm] the system of the VM is updated"
apply_home
