package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

// openForwards opens the SSH connection for the forwards (services.sandbox-vm.expose and
// services.sandbox-vm.reach). It stays open while the VM runs. The ports program
// (host/scripts/ports.sh) adds the forwards to it, and the arguments go to that program.
// The channel that the function returns is closed when the connection stops.
func (s *supervisor) openForwards(ctx context.Context, args ...string) <-chan struct{} {
	c := s.cfg
	closed := make(chan struct{})
	os.Remove(c.path("forwards"))
	os.Remove(c.path("ctl"))
	conn := helper(ctx, "ssh", "-F", c.path("ssh.config"), "-M", "-S", c.path("ctl"), "-N",
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15", c.name)
	if err := conn.Start(); err != nil {
		close(closed)
		return closed
	}
	// Without this file, a subsequent run cannot stop a connection that stayed.
	if err := os.WriteFile(c.path("ssh.pid"), fmt.Appendf(nil, "%d\n", conn.Process.Pid), 0o644); err != nil {
		warn("%v", err)
	}
	go func() {
		// Only the end of the connection matters, not its status.
		_ = conn.Wait()
		close(closed)
	}()

	isClosed := func() bool {
		select {
		case <-closed:
			return true
		default:
			return false
		}
	}
	connected := waitUntil(ctx, connectTimeout, func() bool {
		st, err := os.Stat(c.path("ctl"))
		return isClosed() || err == nil && st.Mode()&os.ModeSocket != 0
	})
	switch {
	case isClosed() || ctx.Err() != nil:
	case connected:
		// The ports program shows a message for each forward that it could not open, and
		// that is not a failure. It fails only if it could not apply the forwards at all.
		// The VM continues without them, the same as with a port that is in use. There is
		// no second try: the cause can stay, for example a disk that is full.
		//
		// --connected: without a control connection the program fails. For the task that
		// is not a failure, because the VM does not run then.
		err := show(helper(ctx, c.ports, append([]string{"--connected"}, args...)...)).Run()
		// The connection or this program can stop during the command.
		if err != nil && !isClosed() && ctx.Err() == nil {
			warn("could not apply the forwards (expose, reach): %v. To try again: devenv tasks run sandbox-vm:ports", err)
		}
	default:
		// The connection does not start. Stop it: the caller then opens a new connection.
		conn.Process.Signal(syscall.SIGTERM)
	}
	return closed
}

// The time that the connection for the forwards gets to start.
const connectTimeout = 30 * time.Second
