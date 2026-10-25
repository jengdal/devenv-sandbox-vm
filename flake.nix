{
  description = "NixOS VMs for devenv projects";

  outputs = { self, ... }: {
    nixosModules.default = ./guest/module.nix;

    # One NixOS configuration for each architecture of the guest, named <prefix>-<arch>. The
    # host builds the configuration for the architecture of the VM (`uname -m`).
    lib.mkVms =
      {
        inputs,
        prefix ? "devenv-sandbox-vm",
        modules ? [ ],
      }:
      let
        inherit (inputs.nixpkgs) lib;
        systems = [
          "aarch64-linux"
          "x86_64-linux"
        ];
      in
      builtins.listToAttrs (
        map (system: {
          name = "${prefix}-${lib.removeSuffix "-linux" system}";
          value = lib.nixosSystem {
            inherit system;
            specialArgs = { inherit inputs; };
            modules = [ self.nixosModules.default ] ++ modules;
          };
        }) systems
      );

    templates.default = {
      path = ./template;
      description = "The sandbox-vm/ directory for a devenv project that uses devenv-sandbox-vm";
    };
  };
}
