# The host module (host/) for a project, without devenv: the tests of tests/nix and the
# files of the proxy test (proxy-configs.nix) use it.
{
  pkgs,
  lib ? pkgs.lib,
}:
rec {
  # The options of devenv that the host module sets. The tests check only their values.
  devenvOptions = {
    options = {
      devenv.root = lib.mkOption { type = lib.types.str; };
      assertions = lib.mkOption {
        type = lib.types.listOf lib.types.unspecified;
        default = [ ];
      };
      packages = lib.mkOption {
        type = lib.types.listOf lib.types.package;
        default = [ ];
      };
      tasks = lib.mkOption {
        type = lib.types.attrsOf lib.types.anything;
        default = { };
      };
      processes = lib.mkOption {
        type = lib.types.attrsOf lib.types.anything;
        default = { };
      };
    };
  };

  # The configuration of a project in the directory `root` with these options of
  # services.sandbox-vm.
  host =
    {
      root ? "/home/user/project",
      options ? { },
    }:
    (lib.evalModules {
      specialArgs = { inherit pkgs; };
      modules = [
        ../../host/devenv.nix
        devenvOptions
        {
          devenv.root = root;
          services.sandbox-vm = {
            enable = true;
          }
          // options;
        }
      ];
    }).config;
  service = args: (host args).services.sandbox-vm;

  # The proxy (host/proxy.nix) for these options of services.sandbox-vm.
  proxy =
    options:
    import ../../host/proxy.nix {
      inherit pkgs lib;
      cfg = service { inherit options; };
    };
}
