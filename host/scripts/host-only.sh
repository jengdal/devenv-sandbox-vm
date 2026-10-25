#!/usr/bin/env bash
# The sandbox-vm process on a computer where the VM cannot run: the VM itself, or a host
# that is not supported.
set -euo pipefail
# shellcheck source=lib.sh
. "${BASH_SOURCE[0]%/*}/lib.sh"
require_host
