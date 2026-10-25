# Development

For the goals and the design decisions of the project, refer to [Design](design.md).

The directories of this repo:

| Path                        | What it is                                                                                                     | Where it runs  |
| --------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------- |
| `host/`                     | The devenv module that projects import: options, scripts, and the supervisor (Go) that creates and runs the VM | The host       |
| `guest/`                    | The NixOS module of the VM (`nixosModules.default`)                                                            | In the VM      |
| `template/`                 | The `sandbox-vm/` directory for a project (`nix flake init -t`)                                                | In the project |
| `flake.nix`                 | The outputs for projects: `nixosModules`, `lib.mkVms`, `templates`                                             |                |
| `constants.nix`             | The values that `host/` and `guest/` must agree on                                                             |                |
| `tests/`                    | The tests of this repo: `nix` (host module), `guest.nix` (NixOS VM test), `e2e` (a real VM)                    |                |
| `devenv.nix`, `devenv.yaml` | The development environment of this repo (below). Projects do not read them                                    |                |
| `docs/`                     | This documentation (mdBook). GitHub Pages publishes it                                                         |                |

`host/supervisor/guest-install.sh` runs in the NixOS installer, not on the host: the supervisor
embeds it (`go:embed` needs it in the same directory) and sends it to the installer.

This repo has its own devenv:

```bash
devenv shell    # installs pre-commit hooks (shellcheck, gofmt) for the files of a commit
devenv test     # lints the scripts and the Go code; tests the host module, the supervisor and the docs
```

`devenv test` creates no VM. It takes a few minutes. It has these tests:

- **The host module** (`tests/nix`). Nix evaluates the module with different options and compares
  the values that the programs get, for example the forwards and the rules of the proxy.
- **The sandbox rules** (`host/supervisor/sandbox_darwin_test.go`). A test program runs in each
  sandbox and tries the operations that the sandbox must deny. `platform_darwin_test.go` compares
  the rules with the files in `host/supervisor/testdata`. To update these files after a change, run
  the tests with `SVM_UPDATE_GOLDEN=1`.
- **The proxy rules** (`proxy_test.go`). The test starts smokescreen with the configuration files of
  the host module and asks for connections to this computer and to private addresses.
- **The sandbox-vm process** (`run_darwin_test.go`). The hypervisor, gvproxy, the proxy, `ssh` and
  console-filter are fakes (`fakes_test.go`). The sandboxes, the logs and the signals are the real
  ones.
- **The shell scripts** (`scripts_test.go`), with fakes in the place of `ssh`, `nc` and borg.
- **The documentation**: the reference pages and the links. Refer to
  [The documentation](#the-documentation).

libkrun and smokescreen have fixed versions in this repo (`host/platform/krunkit.nix`,
`host/smokescreen.nix`). Thus nixpkgs does not update them. Look for security fixes in these
projects, and update the versions. `krunkit.nix` has the steps in its first comment.

`devenv test` also runs on Linux, for example in the VM. The tests of the sandbox rules and of the
sandbox-vm process run only on macOS. On macOS, `devenv test` fails if it skips a test.

While you change the supervisor, run the tests in `host/supervisor` directly. `-short` leaves out
the tests of the sandbox-vm process, which take the most time:

```bash
go test -short ./...
go test -fuzz FuzzCopy ./internal/termsafe    # looks for new inputs until you stop it
```

The tests of the guest module need NixOS. They have their own flake in `tests/`, so the flake of
this repo needs no nixpkgs input:

```bash
nix build ./tests#checks.aarch64-darwin.template    # the template evaluates; builds nothing
nix build -L ./tests#checks.aarch64-linux.guest     # a NixOS VM with the guest module
```

The second command needs a Linux builder. The test boots a VM with the guest module in QEMU and
checks what the guest does with the files of the host: the account, the key, the proxy forward and
the network without a route. It does not use krunkit and gvproxy.

The end-to-end test creates a real VM for a new project in `/tmp` and checks the list in [What an
agent in the VM can and cannot do](security.md#what-an-agent-in-the-vm-can-and-cannot-do) from the
host and from the VM. It takes a long time and downloads a NixOS system:

```bash
devenv shell -- tests/e2e/run.sh --iso ~/Downloads/nixos-minimal-26.05-aarch64-linux.iso   # or without --iso: Linux builder
```

## The documentation

This documentation is in `docs/src`, one Markdown file for each page. `docs/src/SUMMARY.md` sets the
pages and their order in the sidebar. mdBook makes the HTML pages:

```bash
devenv shell -- docs              # shows the site on localhost and updates it when you save
devenv shell -- mdbook build docs # only makes the HTML files, in docs/book/html
```

`docs` gives its other arguments to `mdbook serve`, for example `docs -p 4000` for a different
port.

The reference pages are made from Nix files: [Options](reference/options.md) from the
descriptions in `host/options.nix`, and [Processes and tasks](reference/commands.md) from
`host/commands.nix`. Do not edit the pages. Change the Nix files, then run
`devenv shell -- docs-reference` and commit the new pages. The guides link to the reference pages
and do not repeat what they say.

Write option, process and task names in backticks, and link each one to its reference page, for
example `` [`proxy.allowRanges`](reference/options.md#proxy-allowranges) `` or
`` [`devenv tasks run sandbox-vm:update`](reference/commands.md#task-update) ``. In the Nix files,
only the backticks are necessary: `docs-reference` adds the links. `devenv test` fails if a name
has no link. `docs/links.py` has the rules for the names.

`devenv test` fails if the reference pages do not match the Nix files. It also builds the site
and fails if a link to a page or a heading does not exist. When you rename a heading, change the
links to it. ruff checks the Python scripts in `docs/` and `tests/docs/`. A push to `main` on GitHub publishes the site
(`.github/workflows/docs.yml`).
