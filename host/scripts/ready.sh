#!/usr/bin/env bash
# The probe of the sandbox-vm process. The VM is ready when SSH is available and the VM has
# its settings. The process creates the file `ready` to show that. An SSH probe is not
# sufficient, because the installer of a new VM also answers SSH.
set -euo pipefail
# shellcheck source=lib.sh
. "${BASH_SOURCE[0]%/*}/lib.sh"

test -e "$VM_DIR/ready"
