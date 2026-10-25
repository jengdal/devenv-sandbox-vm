# Personal setup in the VM

Some settings are yours and not part of the VM configuration, for example a git identity and coding
agents. If you use devenv in a repo in the VM, put them in the `devenv.local.nix` file of that repo.
Refer to the [devenv documentation](https://devenv.sh/files-and-variables/#devenvlocalnix) for this
file. You can also use
[your Home Manager configuration](#your-home-manager-configuration), or your own dotfiles.

Example of `~/<repo>/devenv.local.nix` in the VM:

```nix
{ pkgs, ... }:

let
  gitName = "Your Name";
  gitEmail = "you@example.com";
in
{
  packages = [
    pkgs.git
    pkgs.claude-code
    pkgs.tree-sitter
    pkgs.neovim
  ];

  # Git identity for this dev shell only. This does not change ~/.gitconfig.
  env = {
    GIT_AUTHOR_NAME = gitName;
    GIT_AUTHOR_EMAIL = gitEmail;
    GIT_COMMITTER_NAME = gitName;
    GIT_COMMITTER_EMAIL = gitEmail;
  };

  # Also write the identity to .git/config of the repo. Some tools read the
  # configuration directly and ignore the environment variables.
  enterShell = ''
    if git rev-parse --git-dir >/dev/null 2>&1; then
      git config user.name "${gitName}"
      git config user.email "${gitEmail}"

      git config pull.rebase false
    fi
  '';
}
```

`claude-code` is an unfree package. To permit it, create a `devenv.local.yaml` file in the same
directory:

```yaml
allowUnfree: true
```

## Your Home Manager configuration

The VM can apply your own Home Manager configuration to your user. Set
[`services.sandbox-vm.homeManager`](reference/options.md#homemanager) in `devenv.local.nix` on the
host, in the project directory. Do not commit this file.

```nix
services.sandbox-vm.homeManager = "github:me/dotfiles#me@sandbox-aarch64-linux";
```

The `<flake>#<configuration>` part depends on how your setup works.

The VM applies the configuration at these times:

- When the [`sandbox-vm`](reference/commands.md#sandbox-vm) process creates the VM. The VM is ready
  before the configuration is applied. If it fails, the process tries again at the next start.
- At each [`devenv tasks run sandbox-vm:update`](reference/commands.md#task-update), also when the
  system of the VM is up to date.
- When you run [`devenv tasks run sandbox-vm:home`](reference/commands.md#task-home).

Things to know:

- The configuration uses the `flake.lock` of your flake, not `sandbox-vm/flake.lock`. Thus its
  nixpkgs and Home Manager versions are yours, and the VM can keep two versions of nixpkgs.
- Each apply gets the latest commit of the default branch. To keep a fixed version, add the commit
  to the reference: `github:me/dotfiles/<commit>#…`.
- The VM gets the flake through the proxy. A public repo on GitHub works without setup.
- A private repo on GitHub needs a read-only deploy key and a host alias in the VM. Refer to
  [Option 2: A deploy key](repos.md#option-2-a-deploy-key). Then use an SSH URL with the alias:
  `git+ssh://github-dotfiles/me/dotfiles.git#me@sandbox-aarch64-linux`. Do not put a GitHub token in
  the VM for a private `github:` URL: a token can read more repos than your dotfiles.
- A new VM has no credentials, so the first apply of a private repo fails. Run
  [`devenv tasks run sandbox-vm:home`](reference/commands.md#task-home) after you add them.
- On other forges, use `git+https://<host>/me/dotfiles.git` or
  `git+ssh://git@<host>/me/dotfiles.git`. With
  [`proxy.allowedDomains`](reference/options.md#proxy-alloweddomains), add the host of the forge to
  the list.
