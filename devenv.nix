# The development environment of this repo: tools to check the shell scripts and the
# supervisor. A project that uses devenv-sandbox-vm does not read this file. It imports the
# directory host/.
{ pkgs, lib, ... }:
let
  # The proxy test of the supervisor (host/supervisor/proxy_test.go).
  proxyTest = import ./tests/nix/proxy-configs.nix { inherit pkgs; };
in
{
  packages = [
    pkgs.shellcheck
    pkgs.golangci-lint
    pkgs.ruff
    pkgs.mdbook
    pkgs.mdbook-linkcheck2

    pkgs.bash
    pkgs.coreutils
    pkgs.curl
    pkgs.findutils
    pkgs.gawk
    pkgs.git
    pkgs.gnugrep
    pkgs.gnused
    pkgs.openssh
    pkgs.procps
    pkgs.python3
  ];

  # Shows the documentation site on localhost and updates it when a file in docs/ changes.
  # mdbook gets the other arguments, for example `docs -p 4000`.
  scripts.docs.exec = ''mdbook serve "$DEVENV_ROOT/docs" --open "$@"'';

  # Makes the reference pages of the documentation (options.md and commands.md) from
  # host/options.nix and host/commands.nix, in docs/src/reference or the directory in the
  # first argument.
  scripts.docs-reference.exec = ''
    nix-instantiate --eval --strict --json "$DEVENV_ROOT/docs/reference.nix" \
      --arg pkgs 'import ${pkgs.path} { config = { }; overlays = [ ]; }' |
      python3 "$DEVENV_ROOT/docs/reference.py" "''${1:-$DEVENV_ROOT/docs/src/reference}"
  '';

  languages.go = {
    enable = true;
    lsp.enable = true;
  };

  # The programs that the supervisor tests start. Without these variables, the tests that
  # use them are skipped (TestConsoleLog, TestProxyRules).
  env.SVM_S6_LOG = "${pkgs.s6}/bin/s6-log";
  env.SVM_SMOKESCREEN = lib.getExe proxyTest.smokescreen;
  env.SVM_TEST_PROXY_CONFIGS = "${proxyTest.configs}";

  # Check the files that change in a commit. .shellcheckrc has the settings of shellcheck,
  # and ruff.toml the settings of ruff.
  git-hooks.hooks.shellcheck.enable = true;
  git-hooks.hooks.gofmt.enable = true;
  git-hooks.hooks.ruff.enable = true;
  git-hooks.hooks.ruff-format.enable = true;

  # `devenv test` checks all shell scripts, tests the values that the host module creates
  # (tests/nix), builds the documentation and checks its links (docs/), and checks and tests
  # the supervisor. host/supervisor/.golangci.yml has the settings of golangci-lint.
  #
  # On a Mac, each supervisor test must run: a test that is skipped because a program is
  # missing is a failure there. On Linux, the tests of the sandbox rules and of the
  # sandbox-vm process do not exist. The tests that depend on how macOS handles processes
  # and names also run only on macOS.
  enterTest = ''
    reference=$(mktemp -d)
    log=$(mktemp)
    trap 'rm -rf "$reference" "$log"' EXIT

    shellcheck host/scripts/*.sh host/supervisor/*.sh tests/e2e/*.sh
    ruff check docs tests/docs >&2
    ruff format --check docs tests/docs >&2

    failures=$(nix-instantiate --eval --strict --json --expr \
      'import ./tests/nix { pkgs = import ${pkgs.path} { config = { }; overlays = [ ]; }; }')
    if [ "$failures" != "[]" ]; then
      echo "tests/nix: these tests fail: $failures" >&2
      exit 1
    fi

    # The reference pages must match host/options.nix and host/commands.nix.
    docs-reference "$reference"
    for page in options.md commands.md; do
      if ! diff -u "docs/src/reference/$page" "$reference/$page" >&2; then
        echo "docs/src/reference/$page is not up to date: run docs-reference and commit it" >&2
        exit 1
      fi
    done
    mdbook build docs
    python3 tests/docs/anchors.py >&2

    cd host/supervisor
    test -z "$(gofmt -l .)"
    go vet ./...
    golangci-lint run
    if ! go test -race -v ./... >"$log" 2>&1; then
      cat "$log" >&2
      exit 1
    fi
    # devenv shows only stderr.
    grep -E '^(ok|FAIL|---)' "$log" | grep -v -e '--- PASS' >&2 || true
    if [ "$(uname)" = Darwin ] && grep -q -e '--- SKIP' "$log"; then
      echo "a supervisor test was skipped" >&2
      exit 1
    fi
  '';
}
