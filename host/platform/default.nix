# The parts of the host module that depend on the operating system of the host.
#
# A platform is these values:
# - supported: false if the VM cannot run on this host
# - runtimeInputs: the packages that the supervisor uses, without the hypervisor
# - krunkit: the package of the hypervisor
# - guestArch: the architecture of the VM
#
# The supervisor is in host/supervisor. Its file platform_<system>.go starts the hypervisor and
# puts a program in a sandbox.
#
# To add a platform, add a .nix file to this directory and a line to the list, and add a
# platform file to host/supervisor.
{ pkgs }:
let
  platforms = {
    aarch64-darwin = ./darwin.nix;
  };
  system = pkgs.stdenv.hostPlatform.system;
in
if platforms ? ${system} then
  { supported = true; } // import platforms.${system} { inherit pkgs; }
else
  # Also the guest: its clone has the same devenv.nix, and the hypervisor is not available there.
  {
    supported = false;
    runtimeInputs = [ ];
    guestArch = pkgs.stdenv.hostPlatform.linuxArch or "aarch64";
  }
