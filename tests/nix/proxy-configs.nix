# The proxy and the configuration files that host/supervisor/proxy_test.go starts it with.
# The host module creates them (host/proxy.nix), thus the test checks the rules that a VM
# gets. The addresses and names are for documentation: the proxy makes no connection in the
# test. The file devenv.nix of this repo gives them to the test.
{ pkgs }:
let
  inherit (import ./lib.nix { inherit pkgs; }) proxy;
in
{
  smokescreen = (proxy { }).package;

  configs = pkgs.linkFarm "devenv-sandbox-vm-test-proxy" {
    "default.yaml" = (proxy { }).config;
    "deny.yaml" =
      (proxy {
        proxy = {
          denyRanges = [ "203.0.113.0/24" ];
          deniedDomains = [ "denied.example" ];
        };
      }).config;
    "allowed-domains.yaml" = (proxy { proxy.allowedDomains = [ "allowed.example" ]; }).config;
  };
}
