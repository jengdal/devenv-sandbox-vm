# The egress proxy from Stripe. It is not in nixpkgs. The repo contains its dependencies
# (vendor directory).
{
  lib,
  buildGoModule,
  fetchFromGitHub,
}:
buildGoModule {
  pname = "smokescreen";
  version = "0-unstable-2026-10-10";

  src = fetchFromGitHub {
    owner = "stripe";
    repo = "smokescreen";
    rev = "8e963a32d4b0e332dc73467e7d83d8abfec9f8ea";
    hash = "sha256-VENS+ATmLCavLqTEWMQ+CRKMiRDLxgNlCbT95GX1b3k=";
  };
  vendorHash = null;

  subPackages = [ "." ];
  # The tests make network connections.
  doCheck = false;

  meta = {
    description = "HTTP CONNECT proxy that controls the destinations of its clients";
    homepage = "https://github.com/stripe/smokescreen";
    license = lib.licenses.mit;
    mainProgram = "smokescreen";
  };
}
