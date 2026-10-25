# Additions to the VM for this project. The defaults of devenv-sandbox-vm use mkDefault.
# Thus a normal assignment in this file replaces a default.
{
  pkgs,
  inputs,
  ...
}:
{
  # After you change this file, commit the changes and run `devenv tasks run sandbox-vm:update`
  # on the host, while the VM runs. The VM updates its system from the committed files
  # in this directory on the host. Repos and other files in the VM do not change. The task
  # fails if this directory has changes that are not committed.
  #
  # For your personal setup (for example, coding agents), use a `devenv.local.nix` file in
  # the VM. Do not commit that file.
  #
  # The NixOS release of the first installation. It selects the formats of data that some
  # services keep. Do not change it when you update nixpkgs.
  system.stateVersion = "26.05";

  # pkgs.unstable is nixpkgs-unstable, with the same settings as pkgs (for example
  # allowUnfree).
  nixpkgs.overlays = [
    (final: prev: {
      unstable = import inputs.nixpkgs-unstable {
        inherit (final.stdenv.hostPlatform) system;
        inherit (final) config;
      };
    })
  ];

  # To install "unfree" software, remove the comment marks from these lines:
  # nixpkgs.config = {
  #   allowUnfree = true;
  # };

  environment.systemPackages = [
    pkgs.unstable.devenv
    pkgs.git

    # pkgs.unstable.tree-sitter
    # pkgs.unstable.neovim

    # Install a package from a different flake in the inputs of flake.nix:
    #   inputs.<flake>.packages.${pkgs.stdenv.hostPlatform.system}.<package>

    # If you don't want to use the built in `services.sandbox-vm.homeManager` feature, you
    # can install it yourself here, and manually run it inside the VM:
    # pkgs.home-manager
  ];

  #
  # boot.kernelPackages = pkgs.linuxPackages;   # the LTS kernel, not the latest kernel
  #
  # Editors that connect through SSH (VS Code, Zed, IntelliJ and others) send a compiled
  # server program to the VM. If that program does not start, remove the comment mark from
  # this line. NixOS can then run such programs.
  # programs.nix-ld.enable = true;
  #
  # macOS gets back the memory that the VM frees, but not small fragments of free memory and
  # not the cache of files. These timers give those back too. The first uses the CPU of the
  # VM for a short time at each run. After each run of the second, the VM reads files from
  # the disk again.
  # devenv-sandbox-vm.memory.compact.enable = true;
  # devenv-sandbox-vm.memory.compact.interval = "5min";
  # devenv-sandbox-vm.memory.dropCaches.enable = true;
  # devenv-sandbox-vm.memory.dropCaches.interval = "15min";
}
