#!/usr/bin/env bash
# The end-to-end test: it creates a real VM for a new project, and checks from the host and
# from the VM what the documentation promises (docs/src/security.md). It runs only on a host
# that is supported, and it takes many minutes: the VM downloads its system.
#
#   tests/e2e/run.sh [--iso FILE] [--keep]
#
#   --iso FILE   install the VM from this NixOS installer ISO. Without it, the host builds a
#                disk image on a Linux builder.
#   --keep       do not remove the VM and the project at the end
#
# The project is a new directory in /tmp with the name svm-e2e, thus the VM has the name
# devenv-svm-e2e. The host module comes from the files of this repo that git knows. The
# guest module is a copy of the files of this repo in the project.
#
# The script needs no line in ~/.ssh/config: it uses the ssh.config of the VM directly.
#
# Start it in the devenv of this repo: `devenv shell -- tests/e2e/run.sh`. All programs that
# it uses come from that environment (devenv.nix), but not Nix and devenv.
set -euo pipefail

repo=$(cd "${BASH_SOURCE[0]%/*}/../.." && pwd -P)
if [ "${DEVENV_ROOT:-}" != "$repo" ]; then
  echo "start this script in the devenv of the repo: devenv shell -- tests/e2e/run.sh" >&2
  exit 2
fi
iso='' keep=''
while [ $# -gt 0 ]; do
  case "$1" in
    --iso)
      iso=$(cd "$(dirname "$2")" && pwd -P)/$(basename "$2")
      shift
      ;;
    --keep) keep=1 ;;
    *)
      echo "usage: $0 [--iso FILE] [--keep]" >&2
      exit 2
      ;;
  esac
  shift
done

name=devenv-svm-e2e
vm_dir=$HOME/.devenv-sandbox-vm/$name
if [ -e "$vm_dir" ]; then
  echo "$vm_dir exists: a VM of an earlier test. Remove it first." >&2
  exit 1
fi
work=$(cd "$(mktemp -d /tmp/svm-e2e.XXXXXX)" && pwd -P)
project=$work/svm-e2e
log=$work/up.log
up_pid='' server_pid=''

say() {
  printf '\n== %s\n' "$*"
}
fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}
vm() {
  ssh -F "$vm_dir/ssh.config" -o BatchMode=yes -o ConnectTimeout=10 "$name" "$@"
}
# Runs a command in the VM that must fail.
vm_fails() {
  if vm "$@" >/dev/null 2>&1; then
    fail "this command in the VM gave no error: $*"
  fi
}
# Waits until the command is true.
wait_for() {
  local what=$1 seconds=$2
  shift 2
  for ((i = 0; i < seconds; i++)); do
    if "$@" >/dev/null 2>&1; then
      return 0
    fi
    if [ -n "$up_pid" ] && ! kill -0 "$up_pid" 2>/dev/null; then
      tail -n 40 "$log" >&2
      fail "the sandbox-vm process stopped before: $what"
    fi
    sleep 1
  done
  tail -n 40 "$log" >&2
  fail "no $what after $seconds seconds"
}
# The programs of the VM that run on the host.
vm_programs() {
  pgrep -f "$vm_dir/" || true
}
ready() {
  [ -e "$vm_dir/ready" ] && vm true
}

cleanup() {
  status=$?
  [ -z "$server_pid" ] || kill "$server_pid" 2>/dev/null || true
  if [ -n "$up_pid" ] && kill -0 "$up_pid" 2>/dev/null; then
    kill "$up_pid" 2>/dev/null || true
    wait "$up_pid" 2>/dev/null || true
  fi
  if [ -n "$keep" ]; then
    echo "the project is $project, the VM is $vm_dir, the log is $log"
  else
    # A process that failed can leave programs.
    for pid in $(vm_programs); do
      kill "$pid" 2>/dev/null || true
    done
    rm -rf "$vm_dir" "$work"
  fi
  if [ "$status" = 0 ]; then
    say "the end-to-end test passed"
  else
    say "the end-to-end test FAILED"
  fi
}
trap cleanup EXIT

say "create the project $project"
mkdir -p "$project/sandbox-vm"
cd "$project"
cat >devenv.yaml <<EOF
inputs:
  nixpkgs:
    url: github:NixOS/nixpkgs/nixpkgs-unstable
  devenv-sandbox-vm:
    url: git+file://$repo
    flake: false
imports:
  - devenv-sandbox-vm/host
EOF
# The ports of the host that the test uses.
web_port=18473 second_port=18474 host_port=18475
write_devenv_nix() {
  cat >devenv.nix <<EOF
{ ... }:
{
  services.sandbox-vm = {
    enable = true;
    cpus = 4;
    memory = 4;
    disk = 20;
    ${iso:+installer.iso = \"$iso\";}
    expose.web = { guest = 8000; host = $web_port; };
    $1
    reach.host-service = { host = "127.0.0.1"; port = $host_port; guest = 9000; };
    backup.enable = true;
    backup.repository = "$work/backups";
  };
}
EOF
}
write_devenv_nix ''
# The guest module of this repo, as an input with a path in the configuration directory.
# Thus the VM needs no connection to a server for it.
mkdir sandbox-vm/devenv-sandbox-vm
cp -R "$repo/flake.nix" "$repo/constants.nix" "$repo/guest" sandbox-vm/devenv-sandbox-vm/
cp "$repo/template/sandbox-vm/configuration.nix" sandbox-vm/
sed -e 's|^    devenv-sandbox-vm\.url = .*|    devenv-sandbox-vm.url = "path:./devenv-sandbox-vm";|' \
  "$repo/template/sandbox-vm/flake.nix" >sandbox-vm/flake.nix
grep -q 'path:./devenv-sandbox-vm' sandbox-vm/flake.nix || fail "the flake of the template has no devenv-sandbox-vm input"
# A program for the tests of the forwards.
write_extra() {
  cat >sandbox-vm/extra.nix <<EOF
{ pkgs, ... }:
{
  environment.systemPackages = [ pkgs.python3 $1 ];
}
EOF
}
write_extra ''
sed -i.orig -e 's|modules = \[ ./configuration.nix \];|modules = [ ./configuration.nix ./extra.nix ];|' sandbox-vm/flake.nix
rm sandbox-vm/flake.nix.orig
grep -q extra.nix sandbox-vm/flake.nix || fail "the flake of the template has no list of modules"
git init -q
git add .
nix flake lock ./sandbox-vm
git add .
git -c user.name=e2e -c user.email=e2e@example.com commit -q -m "The project of the end-to-end test"

# A service of the host that the VM must not get, and that `reach` makes available.
mkdir "$work/host-service"
echo "the service of the host" >"$work/host-service/index.html"
python3 -m http.server --bind 127.0.0.1 --directory "$work/host-service" "$host_port" >/dev/null 2>&1 &
server_pid=$!
wait_for "service of the host" 10 curl -fsS --max-time 5 "http://127.0.0.1:$host_port/"

say "start the VM (the log is $log)"
devenv up sandbox-vm >"$log" 2>&1 &
up_pid=$!
wait_for "VM that is ready" 3600 ready
[ "$(stat -c %a "$vm_dir")" = 700 ] || fail "other users can read $vm_dir"

say "the VM has no files and no keys of the host"
# shellcheck disable=SC2016 # the VM expands the variable
[ "$(vm 'echo "[$SSH_AUTH_SOCK]"')" = "[]" ] || fail "the VM has an ssh-agent of the host"
vm_fails "mount | grep -E 'virtiofs|9p'"
vm sudo -n true || fail "the user has no root access in the VM"

say "the only route out of the VM is the proxy"
[ "$(vm curl -sS -o /dev/null -w '%{http_code}' --max-time 60 https://cache.nixos.org/nix-cache-info)" = 200 ] \
  || fail "the VM cannot connect to the internet through the proxy"
vm_fails curl -sS --noproxy "'*'" --max-time 10 https://cache.nixos.org/nix-cache-info
vm_fails getent hosts cache.nixos.org
[ -z "$(vm ip route show default)" ] || fail "the VM has a default route"

say "the VM cannot connect to the host and to the local network"
gateway=192.168.127.254
# Through the proxy: the names and the addresses of this computer, and private addresses.
# The address of this computer in the local network: the address that a connection out
# uses. The command sends nothing.
lan=$(python3 -c 'import socket; s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.connect(("192.0.2.1", 9)); print(s.getsockname()[0])' 2>/dev/null || true)
for target in 127.0.0.1 localhost "$gateway" 192.168.127.1 10.0.0.1 169.254.169.254 ${lan:+"$lan"}; do
  # The option names the proxy: curl does not use the proxy of the session for the names of
  # this computer.
  code=$(vm curl -sS -o /dev/null -w "'%{http_code}'" --proxy http://127.0.0.1:3128 --noproxy "''" --max-time 20 "http://$target:$host_port/" || true)
  [ "$code" = 407 ] || fail "the proxy gave the status $code for $target:$host_port, and not a refusal"
done
# Without the proxy: the addresses of the network of gvproxy.
for target in "$gateway" 192.168.127.1 ${lan:+"$lan"}; do
  vm_fails curl -sS --noproxy "'*'" --max-time 10 "http://$target:$host_port/"
done
# The proxy records the connections that it refused.
grep -q '"allow":false' "$vm_dir/log/smokescreen/current" || fail "the log of the proxy has no refused connection"

say "expose and reach"
vm "mkdir -p web && echo 'the service of the VM' > web/index.html && (nohup python3 -m http.server --bind 127.0.0.1 --directory web 8000 >/dev/null 2>&1 &)"
wait_for "forward of expose.web" 30 curl -fsS --max-time 5 "http://127.0.0.1:$web_port/"
[ "$(curl -fsS --max-time 5 "http://127.0.0.1:$web_port/")" = "the service of the VM" ] || fail "expose.web gives a different service"
[ "$(vm curl -fsS --noproxy "'*'" --max-time 10 http://127.0.0.1:9000/)" = "the service of the host" ] \
  || fail "reach.host-service does not give the service of the host"
# The forwards are only on the loopback address of the host.
if [ -n "$lan" ] && curl -fsS --max-time 5 "http://$lan:$web_port/" >/dev/null 2>&1; then
  fail "expose.web is open on $lan"
fi

say "the task sandbox-vm:ports applies a new forward"
write_devenv_nix "expose.second = { guest = 8000; host = $second_port; };"
devenv tasks run sandbox-vm:ports
wait_for "forward of expose.second" 30 curl -fsS --max-time 5 "http://127.0.0.1:$second_port/"
write_devenv_nix ''
devenv tasks run sandbox-vm:ports
if curl -fsS --max-time 5 "http://127.0.0.1:$second_port/" >/dev/null 2>&1; then
  fail "expose.second is open after its removal"
fi
curl -fsS --max-time 5 "http://127.0.0.1:$web_port/" >/dev/null || fail "expose.web closed with expose.second"

say "the task sandbox-vm:backup, and the restore of a file"
vm "echo 'a file of the user' > kept.txt"
devenv tasks run sandbox-vm:backup
# The shell of the user did not create the repository, thus borg asks about it.
export BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes
archive=$(devenv shell -- borg list --short "$work/backups" | tail -n 1)
[ -n "$archive" ] || fail "the repository has no backup"
user=$(vm whoami)
# borg writes the file below the directory of the project. That directory is not a part of
# the configuration directory.
devenv shell -- borg extract "$work/backups::$archive" "home/$user/kept.txt"
[ "$(cat "home/$user/kept.txt")" = "a file of the user" ] || fail "the backup does not have the file"

say "the task sandbox-vm:update changes the system, and the files stay"
vm_fails command -v hello
write_extra 'pkgs.hello'
if devenv tasks run sandbox-vm:update >"$work/update.log" 2>&1; then
  fail "the update did not stop for changes that are not committed"
fi
git -c user.name=e2e -c user.email=e2e@example.com commit -q -a -m "Add a package"
devenv tasks run sandbox-vm:update
vm hello >/dev/null || fail "the system of the VM does not have the new package"
[ "$(vm cat kept.txt)" = "a file of the user" ] || fail "the update changed the files of the user"
[ "$(vm cat /var/lib/devenv-sandbox-vm/provisioned)" = "$(git rev-parse HEAD:sandbox-vm)" ] \
  || fail "the VM does not record the tree of its system"

say "a reboot of the guest"
krunkit=$(cat "$vm_dir/krunkit.pid")
vm sudo reboot || true
wait_for "second boot" 300 sh -c "[ \"\$(cat '$vm_dir/krunkit.pid')\" != '$krunkit' ] && [ -e '$vm_dir/ready' ]"
wait_for "VM that is ready after the reboot" 120 ready
vm hello >/dev/null || fail "the reboot gave the older system"
vm "(nohup python3 -m http.server --bind 127.0.0.1 --directory web 8000 >/dev/null 2>&1 &)"
wait_for "forward of expose.web after the reboot" 60 curl -fsS --max-time 5 "http://127.0.0.1:$web_port/"
[ "$(vm curl -sS -o /dev/null -w '%{http_code}' --max-time 60 https://cache.nixos.org/nix-cache-info)" = 200 ] \
  || fail "the VM cannot connect through the proxy after the reboot"

say "the stop of the process shuts down the VM"
[ -n "$(vm_programs)" ] || fail "no program of the VM runs"
kill "$up_pid"
wait "$up_pid" || true
up_pid=''
sleep 2
[ -z "$(vm_programs)" ] || fail "these programs of the VM run after the stop: $(vm_programs | tr '\n' ' ')"
[ ! -e "$vm_dir/ready" ] || fail "the file ready is there after the stop"
grep -q 'Reached target System Power Off' "$vm_dir/log/console/current" || fail "the guest did not shut down correctly"
if curl -fsS --max-time 5 "http://127.0.0.1:$web_port/" >/dev/null 2>&1; then
  fail "expose.web is open after the stop"
fi
