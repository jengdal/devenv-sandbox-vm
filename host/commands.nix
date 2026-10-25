# The processes and tasks of the host module, for the documentation
# (docs/src/reference/commands.md) and for `devenv tasks list`. devenv.nix defines them. The
# first sentence of a task is its description in devenv. Write option names in backticks:
# the documentation links them.
#
# tests/nix checks that this file has each process and task of devenv.nix.
{
  processes = {
    sandbox-vm = ''
      Creates, configures and runs the VM. Start it with `devenv up sandbox-vm`. `devenv up`
      without `sandbox-vm` does not start it.

      The first start creates the VM, which takes a few minutes. The process is ready when the
      VM accepts SSH connections and has the settings of the host. When you stop the process,
      the VM shuts down. If the VM does not shut down in 45 seconds, the process powers it off.
      If the VM fails to install or start, devenv does not start the process again.

      A process on the host can wait for the VM with `after = [ "devenv:processes:sandbox-vm" ]`.
      This does not start the VM. Start the two processes together:
      `devenv up sandbox-vm <process>`.
    '';
    sandbox-vm-backup = ''
      Makes the automatic backups. It exists only if `backup.enable` is true. Start it together
      with the VM: `devenv up sandbox-vm sandbox-vm-backup`.

      It waits until the VM is ready, but it does not start the VM. Then it makes a backup when
      the last backup is older than `backup.interval` hours. Changes to the `backup` options
      apply when you restart `devenv up sandbox-vm sandbox-vm-backup`.
    '';
  };

  tasks = {
    "sandbox-vm:ports" = ''
      Applies `expose` and `reach` to the running VM. It also runs each time you enter the
      devenv shell. If the VM is not running, it does nothing.
    '';
    "sandbox-vm:update" = ''
      Updates the NixOS system of the VM from the committed `configDir`. The VM must be
      running. The VM changes to the new system immediately, without a restart.

      The `sandbox-vm` process does not update the system: a change to `configDir` has no effect
      until you run this task. The task fails if `configDir` has changes that are not committed.
      After the update, it applies `homeManager`.
    '';
    "sandbox-vm:home" = ''
      Applies `homeManager` to the login user in the VM. The VM must be running. The process
      also does this when it creates the VM, and `sandbox-vm:update` after each update.
    '';
    "sandbox-vm:backup" = ''
      Makes a backup now. It exists only if `backup.enable` is true. The VM must be running;
      the `sandbox-vm-backup` process does not need to be running. The time of the last backup has
      no effect.
    '';
  };
}
