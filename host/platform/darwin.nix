# Apple silicon Macs: the hypervisor krunkit (libkrun), and the macOS sandbox
# (sandbox-exec). host/supervisor/platform_darwin.go uses them.
{ pkgs }:
{
  runtimeInputs = [ ];
  # krunkit and libkrun of the nixpkgs of the project, with changes (./krunkit.nix): libkrun
  # has a fixed version and a patch that gives the free memory of the VM back to macOS, and
  # the VM has no GPU device.
  krunkit = import ./krunkit.nix { inherit pkgs; };
  guestArch = "aarch64";
}
