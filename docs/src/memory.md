# The memory of the VM

[`services.sandbox-vm.memory`](reference/options.md#memory) is the maximum. The VM gets memory from
macOS when it uses it. When the VM frees memory, macOS gets it back after a few seconds. Two kinds
of memory do not go back:

- Free memory in fragments smaller than 2 MiB.
- The cache of files. Linux keeps files in memory that is not in use.

Thus the memory of the VM in Activity Monitor can be larger than the memory that the programs in the
VM use. Two timers in the VM give this memory back. They are off by default. To enable them, add
these lines to `sandbox-vm/configuration.nix` and
[change the system of the VM](vm-system.md):

```nix
devenv-sandbox-vm.memory.compact.enable = true;
devenv-sandbox-vm.memory.compact.interval = "5min";
devenv-sandbox-vm.memory.dropCaches.enable = true;
devenv-sandbox-vm.memory.dropCaches.interval = "15min";
```
