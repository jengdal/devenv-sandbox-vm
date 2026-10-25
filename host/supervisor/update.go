package main

// The settings of the guest. This function runs when SSH is available in the installed
// system. The task sandbox-vm:update updates the system of the guest
// (host/scripts/update.sh).

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"sandbox-vm/internal/termsafe"
)

// pushSettings sends the settings to the guest again. The port of the proxy can be
// different in each run.
func (s *supervisor) pushSettings(ctx context.Context) error {
	env, err := os.ReadFile(s.cfg.path("env"))
	if err != nil {
		return err
	}
	install := showGuest(s.vmSSH(ctx, "sudo", "install", "-D", "-m", "644", "/dev/stdin", s.cfg.guestState+"/env"))
	install.Stdin = bytes.NewReader(env)
	if err := install.Run(); err != nil {
		return fmt.Errorf("could not send the settings to the VM: %w", err)
	}
	// The status of the command is that of the proxy service: without it, the guest has no
	// network.
	restart := s.vmSSH(ctx, "sudo systemctl restart"+
		" devenv-sandbox-vm-settings.service devenv-sandbox-vm-proxy.service;"+
		" systemctl is-active --quiet devenv-sandbox-vm-proxy.service")
	restart.Stdout = termsafe.NewWriter(os.Stdout)
	if err := restart.Run(); err != nil && ctx.Err() == nil {
		warn("the proxy service of the VM did not start; the VM has no connection out")
	}
	return nil
}
