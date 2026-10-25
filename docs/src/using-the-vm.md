# How to use the VM

```bash
ssh devenv-<project>
```

- To use an editor on the host, open the VM as an SSH remote. The host is `devenv-<project>`.
  Editors that can connect through SSH support this, for example VS Code, Zed and IntelliJ.

  WARNING: The remote editing feature of an editor can decrease the isolation of the VM. Read the
  security information in the documentation of your editor for this feature. Then decide if the risk
  is acceptable for your work. A terminal editor in the VM does not have this risk.

- If the remote server of the editor does not start, enable `programs.nix-ld` in
  `sandbox-vm/configuration.nix`. The template contains this line as a comment.

- Before you can commit in the VM, the VM must have a git identity. Refer to
  [Personal setup in the VM](personal-setup.md).
