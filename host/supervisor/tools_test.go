package main

import (
	"os/exec"
	"testing"
)

// tool returns the path of a program that a test starts. The programs come from PATH, thus
// from the devenv of the repo (devenv.nix), and not from fixed paths of the computer. The
// test is skipped without the program.
func tool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skip(err)
	}
	return path
}
