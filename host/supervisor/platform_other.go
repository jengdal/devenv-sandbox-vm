//go:build !darwin

package main

// The platform file for the hosts where the VM cannot run. With it, the supervisor builds
// on these hosts, and the tests of the other files run there, for example in the VM
// itself. platform_darwin.go lists what a platform file gives.

import (
	"context"
	"errors"
	"os/exec"
)

var errNoPlatform = errors.New("this host is not supported: the VM runs only on macOS")

const hvProgram = "hypervisor"

func confinedArgv(confinement, []string) ([]string, error) {
	return nil, errNoPlatform
}

func hvNetworkDir() (string, error) {
	return "", errNoPlatform
}

func hvStartCommand(*config, string, []string) (*exec.Cmd, error) {
	return nil, errNoPlatform
}

func hvInstallerCommand(*config, string, string, []string) (*exec.Cmd, error) {
	return nil, errNoPlatform
}

func cloneFile(context.Context, string, string) error {
	return errNoPlatform
}

func hvCleanupNetwork(string) {}

func gvproxyListenArgs(*config) []string {
	return nil
}

func execSandbox([]string) error {
	return errNoPlatform
}
