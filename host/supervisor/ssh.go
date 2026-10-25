package main

import (
	"context"
	"os/exec"
	"strconv"
)

// vmSSH returns a command that runs in the installed system of the guest.
func (s *supervisor) vmSSH(ctx context.Context, command ...string) *exec.Cmd {
	return helper(ctx, "ssh", append([]string{
		"-F", s.cfg.path("ssh.config"), "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		s.cfg.name,
	}, command...)...)
}

// installerSSH returns a command that runs in the installer. The installer has its own host
// key, which the host created before the boot.
func (s *supervisor) installerSSH(ctx context.Context, command ...string) *exec.Cmd {
	return helper(ctx, "ssh", append([]string{
		"-F", "/dev/null", "-p", strconv.Itoa(s.sshPort), "-i", s.cfg.path("id_ed25519"),
		"-o", "IdentitiesOnly=yes",
		"-o", "UserKnownHostsFile=" + s.cfg.path("install", "known_hosts"),
		"-o", "StrictHostKeyChecking=yes",
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "root@127.0.0.1",
	}, command...)...)
}
