# The only route out of the VM: an HTTP proxy on the host (smokescreen). The proxy resolves
# each name and makes the connection. It refuses the addresses of the host and private
# networks. It records each connection in its log. The options in services.sandbox-vm.proxy
# set the other destinations that it permits. By default it permits all of them.
{
  pkgs,
  lib,
  cfg,
}:
let
  yaml = pkgs.formats.yaml { };

  # The rules for host names. Without client certificates, the proxy cannot identify its
  # clients, thus only the `default` rule applies. The VM is the only client.
  aclSettings =
    lib.recursiveUpdate {
      version = "v1";
      services = [ ];
      global_deny_list = cfg.proxy.deniedDomains;
      default = {
        project = "devenv-sandbox-vm";
        action = if cfg.proxy.allowedDomains == null then "open" else "enforce";
      }
      // lib.optionalAttrs (cfg.proxy.allowedDomains != null) {
        # A name must pass the rules for names and the rules for addresses.
        allowed_domains = cfg.proxy.allowedDomains ++ cfg.proxy.allowedLocalDomains;
      };
    } cfg.proxy.acl;
  acl = yaml.generate "devenv-sandbox-vm-acl.yaml" aclSettings;

  # allow_missing_role accepts clients without a certificate. With `network: ip4` the proxy
  # uses only the IPv4 addresses of a name. On a network with public IPv6 addresses, the
  # other computers of the local network have addresses that the proxy does not refuse.
  # `ip` and `acl_file` are last: proxy.settings cannot change them.
  settings =
    lib.recursiveUpdate (
      {
        allow_missing_role = true;
        exit_timeout = "1s";
      }
      // lib.optionalAttrs (!cfg.proxy.ipv6) { network = "ip4"; }
      // lib.optionalAttrs (cfg.proxy.denyRanges != [ ]) { deny_ranges = cfg.proxy.denyRanges; }
      // lib.optionalAttrs (cfg.proxy.allowRanges != [ ]) { allow_ranges = cfg.proxy.allowRanges; }
      # For these names, the proxy does not check the address. It still refuses the
      # addresses of this computer.
      // lib.optionalAttrs (cfg.proxy.allowedLocalDomains != [ ]) {
        unsafe_ip_filter_bypassed_domains = cfg.proxy.allowedLocalDomains;
      }
    ) cfg.proxy.settings
    // {
      ip = "127.0.0.1";
      acl_file = "${acl}";
    };
in
{
  # The settings of the two files, for the tests (tests/nix).
  inherit settings aclSettings;

  package = pkgs.callPackage ./smokescreen.nix { };

  config = yaml.generate "devenv-sandbox-vm-smokescreen.yaml" settings;
}
