#!/usr/bin/env bash
# Installs NixOS on the disk of a new VM. It runs as root in the installer, not on the
# host: install.go sends this file to the installer.
#
# Arguments:
#   1    the directory in the installer with the files from the host
#   2    the directory of the installed system that gets the settings
#   3    the flake of the guest, without the architecture at the end of its name
#   4... options for nixos-install
#
# The NixOS module of the guest mounts the partitions by the labels that are set here.
set -euo pipefail
stage=$1 state=$2 flake=$3
shift 3

parted -s /dev/vda -- mklabel gpt \
  mkpart ESP fat32 1MiB 513MiB set 1 esp on \
  mkpart nixos ext4 513MiB 100%
udevadm settle
mkfs.vfat -F 32 -n ESP /dev/vda1 >/dev/null
mkfs.ext4 -q -F -L nixos /dev/vda2
mount /dev/vda2 /mnt
mkdir /mnt/boot
mount -o umask=0077 /dev/vda1 /mnt/boot
install -D -m 644 -t "/mnt$state" "$stage/env" "$stage/authorized_keys" "$stage/provisioned"
nixos-install --no-root-passwd --no-channel-copy "$@" \
  --flake "$flake-$(uname -m)" </dev/null
umount -R /mnt
