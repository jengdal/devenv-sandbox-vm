{
  description = "NixOS guest for this devenv project";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    nixpkgs-unstable.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    devenv-sandbox-vm.url = "github:jengdal/devenv-sandbox-vm";
  };

  outputs =
    inputs@{ devenv-sandbox-vm, ... }:
    {
      nixosConfigurations = devenv-sandbox-vm.lib.mkVms {
        inherit inputs;
        # prefix = "devenv-sandbox-vm"; # the same as services.sandbox-vm.configName on the host
        modules = [ ./configuration.nix ];
      };
    };

}
