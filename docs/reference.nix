# The options of services.sandbox-vm and the processes and tasks of the host module as JSON,
# for docs/reference.py. Nix evaluates the host module like the tests do (tests/nix/lib.nix)
# and builds nothing.
#
#   nix-instantiate --eval --strict --json docs/reference.nix --arg pkgs 'import <nixpkgs> { }'
{
  pkgs,
  lib ? pkgs.lib,
}:
let
  inherit (import ../tests/nix/lib.nix { inherit pkgs lib; }) devenvOptions;
  eval = lib.evalModules {
    specialArgs = { inherit pkgs; };
    modules = [
      ../host/devenv.nix
      devenvOptions
      {
        devenv.root = "/home/user/project";
        services.sandbox-vm.enable = true;
      }
    ];
  };
in
{
  # The paths of the files are not part of the documentation.
  options = map (option: removeAttrs option [ "declarations" ]) (
    lib.optionAttrSetToDocList eval.options.services.sandbox-vm
  );
  commands = import ../host/commands.nix;
}
