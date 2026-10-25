# Quick start

## Requirements

- A Mac with Apple silicon.
- Nix and devenv. Refer to [Installation](https://devenv.sh/getting-started/#installation) in the
  devenv documentation. Nix must have [flakes](https://wiki.nixos.org/wiki/Flakes) enabled.
- A project with devenv. To add devenv to a project, run `devenv init`. Refer to
  [Initial set up](https://devenv.sh/getting-started/#initial-set-up).
- A NixOS installer ISO for `aarch64-linux`, or a Linux builder for Nix. Refer to
  [How the process creates a new VM](how-it-works.md#how-the-process-creates-a-new-vm).

## Where to declare the VM

You can declare the VM in the devenv of your project, or in a separate devenv for the VM only.

Use the devenv of your project if:

- You want to share the VM with other people who work on the project.
- You do not want two devenvs for one project.

Use a separate devenv if:

- You do not want to share the VM declaration with other people who work on the project.
- You do not own the project, or you cannot or do not want to add the VM to it.
- You use the VM for more than one project, and no single devenv is the natural place for it.
- You declare many services and processes on the host around the VM, for example ngrok or MCP
  servers. Then the `devenv up` list can become crowded and confusing, because some items run only
  on the host and others only in the VM.

## Steps

1. Add this line at the top of `~/.ssh/config`. `ssh` and git use it:

   ```
   Include ~/.devenv-sandbox-vm/*/ssh.config
   ```

2. Download the minimal NixOS ISO for `aarch64-linux` from
   [nixos.org](https://nixos.org/download/#nixos-iso). To build the VM on a Linux builder instead,
   refer to [How the process creates a new VM](how-it-works.md#how-the-process-creates-a-new-vm).

3. Add these lines to `devenv.yaml`. Keep the inputs and imports that are already there:

   ```yaml
   require_version: true
   inputs:
     # ... your other inputs, for example nixpkgs
     devenv-sandbox-vm:
       url: github:jengdal/devenv-sandbox-vm
       flake: false
   imports:
     # ... your other imports
     - devenv-sandbox-vm/host
   ```

4. Configure the VM in `devenv.nix`:

   ```nix
   { pkgs, ... }: {
     services.sandbox-vm = {
       enable = true;
       memory = 16; # GiB; the default is 8
       disk = 200; # GiB; the default is 100. The process applies it only when it creates the VM.

       # The ISO from step 2. Without this line, the host builds the VM on a Linux builder.
       installer.iso = "~/Downloads/nixos-minimal-26.05-aarch64-linux.iso";
     };
   }
   ```

   Settings that are only for you go in `devenv.local.nix` on the host. Do not commit that file.
   Refer to the [devenv documentation](https://devenv.sh/files-and-variables/#devenvlocalnix). For
   all `services.sandbox-vm` options, refer to [Options](reference/options.md).

5. Create the NixOS configuration of the VM:

   ```bash
   nix flake init -t github:jengdal/devenv-sandbox-vm   # creates sandbox-vm/{flake,configuration}.nix, README.md
   ```

6. Optional: add packages and settings to `sandbox-vm/configuration.nix`. It is a normal NixOS
   module, and it has examples in comments. You can also change it later. Refer to
   [How to change the system of the VM](vm-system.md).

7. Lock the inputs and commit all files. The process uses only the committed files of `sandbox-vm/`.

   ```bash
   nix flake lock ./sandbox-vm   # creates sandbox-vm/flake.lock
   devenv update                 # creates devenv.lock
   git add sandbox-vm devenv.yaml devenv.nix devenv.lock
   git commit -m "Add devenv-sandbox-vm"
   ```

   The VM gets the inputs of `sandbox-vm/flake.nix` through the proxy when it updates its system,
   and when it installs from an ISO. If an input is on a server with a private address, put the name
   of the server in
   [`services.sandbox-vm.proxy.allowedLocalDomains`](reference/options.md#proxy-allowedlocaldomains).

8. Start the VM, and wait until the message `[sandbox-vm] ready` shows. The first start installs
   NixOS from the ISO. This takes a few minutes.

   ```bash
   devenv up sandbox-vm
   ```

9. Connect to the VM:

   ```bash
   ssh devenv-<project>
   ```

   Then put your repos in the VM. Refer to
   [How to put your repos in the VM](repos.md).

10. Verify that the proxy blocks and permits what you expect. Refer to
    [The network of the VM](network.md).

[`devenv up sandbox-vm`](reference/commands.md#sandbox-vm) stays in the foreground. When you stop
it, the VM shuts down. If the VM does not shut down in 45 seconds, the process powers it off.

If you reboot the VM (`sudo reboot`), the process boots it again. Your SSH sessions end. The
forwards are available again after the message `[sandbox-vm] ready`. If you power off the VM
(`sudo poweroff`), the process exits.

Each VM keeps its disk, its SSH key and its `ssh.config` in
`~/.devenv-sandbox-vm/devenv-<project>/`. If you enable [backups](logs-and-backups.md#backups), they
are in `~/.devenv-sandbox-vm-backups/devenv-<project>/` by default.
