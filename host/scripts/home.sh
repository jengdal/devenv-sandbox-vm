#!/usr/bin/env bash
# Applies the home-manager configuration of services.sandbox-vm.homeManager to the login user
# in the VM: the sandbox-vm:home task. The sandbox-vm process starts it once after it creates a
# VM, and update.sh starts it after each update.
#
# The VM builds the activation package of the configuration from the flake, through the proxy,
# and runs it. The flake uses its own flake.lock, not the flake.lock of the project.
#
# The file `home-pending` on the host shows that a new VM did not get the configuration yet.
set -euo pipefail
# shellcheck source=lib.sh
. "${BASH_SOURCE[0]%/*}/lib.sh"
require_host

# Write to stderr. devenv shows only stderr from a task.
exec >&2
if [ -z "${SVM_HOME_MANAGER:-}" ]; then
  echo "[sandbox-vm] services.sandbox-vm.homeManager is not set; nothing to apply"
  rm -f "$VM_DIR/home-pending"
  exit 0
fi
if [ ! -e "$VM_DIR/ready" ]; then
  echo "[sandbox-vm] the VM is not ready; the home-manager configuration was not applied"
  exit 1
fi
# <flake>#<configuration>. The flake reference can also contain `#` only before the name.
flake=${SVM_HOME_MANAGER%#*}
name=${SVM_HOME_MANAGER##*#}
if [ "$flake" = "$SVM_HOME_MANAGER" ] || [ -z "$flake" ] || ! [[ $name =~ ^[A-Za-z0-9@._+-]+$ ]]; then
  echo "[sandbox-vm] services.sandbox-vm.homeManager must be <flake>#<configuration>: $SVM_HOME_MANAGER"
  exit 1
fi
proxy=$(guest_proxy) || exit 1

echo "[sandbox-vm] applying the home-manager configuration $SVM_HOME_MANAGER"
attr="$flake#homeConfigurations.\"$name\".activationPackage"
# The guest gets the flake reference as one argument of `sh -c`. --refresh: Nix gets the latest
# commit of the branch, and not the commit that it keeps in its cache for one hour.
status=0
# shellcheck disable=SC2016,SC2029
vm_ssh "env http_proxy=$proxy https_proxy=$proxy HTTPS_PROXY=$proxy sh -c" \
  "'out=\$(nix build --refresh --no-link --print-out-paths \"\$1\") && \"\$out/activate\"' sh $(shell_quote "$attr")" \
  </dev/null 2>&1 | safe_text || status=${PIPESTATUS[0]}
if [ "$status" != 0 ]; then
  echo "[sandbox-vm] the home-manager configuration was not applied"
  exit 1
fi
rm -f "$VM_DIR/home-pending"
echo "[sandbox-vm] the home-manager configuration is applied"
