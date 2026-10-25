{
  description = "The tests of devenv-sandbox-vm that need NixOS";

  # The flake of the repo (../flake.nix) has no inputs, thus a project that uses it gets
  # only its own nixpkgs. The tests need a nixpkgs, thus they have their own flake.
  #
  #   nix flake check ./tests                                       all checks of this computer
  #   nix build ./tests#checks.aarch64-linux.guest --print-build-logs   the VM test, on a Linux builder
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs =
    { nixpkgs, ... }:
    let
      lib = nixpkgs.lib;
      # The outputs of the flake of the repo.
      repo = (import ../flake.nix).outputs { self = repo; };
      guestSystems = [
        "aarch64-linux"
        "x86_64-linux"
      ];

      # The configurations that the template creates. nixpkgs-unstable is the nixpkgs of the
      # tests: the test only evaluates the configurations.
      template =
        modules:
        repo.lib.mkVms {
          inputs = {
            inherit nixpkgs;
            nixpkgs-unstable = nixpkgs;
          };
          inherit modules;
        };
    in
    {
      checks = lib.genAttrs ([ "aarch64-darwin" ] ++ guestSystems) (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          # The derivations of a configuration, as text. To make the text, Nix evaluates
          # the full configuration, and it builds nothing.
          derivations =
            name: vm:
            lib.concatMapStrings
              (output: "${name} ${output} ${builtins.unsafeDiscardStringContext vm.config.system.build.${output}.drvPath}\n")
              [
                "toplevel"
                "sandboxVmImage"
              ];
        in
        {
          # The guest module and the template evaluate, for each architecture of the guest:
          # the installed system and the disk image. This check also runs on a Mac.
          # Each new project locks the VM module from the URL in the template, thus the URL
          # must be the published repo.
          template =
            assert lib.assertMsg (
              (import ../template/sandbox-vm/flake.nix).inputs.devenv-sandbox-vm.url
              == "github:jengdal/devenv-sandbox-vm"
            ) "template/sandbox-vm/flake.nix: devenv-sandbox-vm.url must be github:jengdal/devenv-sandbox-vm";
            pkgs.writeText "devenv-sandbox-vm-template" (
            lib.concatStrings (lib.mapAttrsToList derivations (template [ ../template/sandbox-vm/configuration.nix ]))
            # The options that the template shows in its comments.
            + lib.concatStrings (
              lib.mapAttrsToList derivations (
                template [
                  ../template/sandbox-vm/configuration.nix
                  (
                    { pkgs, ... }:
                    {
                      nixpkgs.config.allowUnfree = true;
                      environment.systemPackages = [
                        pkgs.neovim
                        pkgs.unstable.hello
                      ];
                      boot.kernelPackages = pkgs.linuxPackages;
                      programs.nix-ld.enable = true;
                      networking.hostName = "static";
                      time.timeZone = "UTC";
                      devenv-sandbox-vm.memory.compact.enable = true;
                      devenv-sandbox-vm.memory.dropCaches.enable = true;
                    }
                  )
                ]
              )
            )
          );
        }
        # A VM with the guest module. It needs Linux with KVM.
        // lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
          guest = import ./guest.nix {
            inherit pkgs;
            module = repo.nixosModules.default;
          };
        }
      );
    };
}
