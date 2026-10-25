# The supervisor (the program of the sandbox-vm process), and console-filter
# (./supervisor/console-filter), which filters the console of the VM. They use only the Go
# standard library, thus there is no vendor directory and no dependency hash.
{ lib, buildGoModule }:
buildGoModule {
  pname = "devenv-sandbox-vm-supervisor";
  version = "0";

  src = ./supervisor;
  vendorHash = null;

  meta = {
    description = "Creates and runs the VM of devenv-sandbox-vm";
    license = lib.licenses.asl20;
    mainProgram = "sandbox-vm";
  };
}
