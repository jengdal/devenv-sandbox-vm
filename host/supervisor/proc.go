package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sandbox-vm/internal/termsafe"
)

// child is a program that runs in its own session, and that is not a child process of
// this program.
type child struct {
	pid int
	// Closed when the program stops.
	done chan struct{}
}

// startDetached starts a command in its own session and without a parent process. If
// pidfile is not empty, it gets the pid of the program.
//
// devenv stops a process with a signal to all processes that it started: this program and
// all programs below it. That removes the network of the VM and powers it off while the
// guest shuts down. Thus these programs must not be below this program. A session of their
// own is not sufficient: devenv (2.4) also signals a program below this program that has its
// own session. Go cannot fork.
// Thus this program starts itself a second time (`sandbox-vm detach`), and the second
// program starts the command and stops immediately.
//
// The command creates its files with a mode that permits only the user: krunkit creates a socket
// to the guest in /tmp.
//
// The pid files are for a subsequent run: if this program is killed, the programs continue,
// and the subsequent run stops them (stopPid).
func startDetached(cmd *exec.Cmd, pidfile string) (*child, error) {
	if pidfile == "" {
		tmp, err := os.CreateTemp("", "sandbox-vm-pid")
		if err != nil {
			return nil, err
		}
		tmp.Close()
		pidfile = tmp.Name()
		defer os.Remove(pidfile)
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	launch := exec.Command(self, append([]string{"detach", pidfile, cmd.Path}, cmd.Args[1:]...)...)
	launch.Stdin, launch.Stdout, launch.Stderr = cmd.Stdin, cmd.Stdout, cmd.Stderr
	// The second program gives its environment to the command.
	launch.Env = cmd.Env
	if launch.Stderr == nil {
		// The messages of the second program. The command gets its own stderr.
		launch.Stderr = os.Stderr
		launch.Args = append([]string{launch.Args[0], "detach", "--quiet"}, launch.Args[2:]...)
	}
	if err := launch.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", cmd.Args[0], err)
	}
	text, err := os.ReadFile(pidfile)
	if err != nil {
		return nil, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(text)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pidfile, err)
	}
	c := &child{pid: pid, done: make(chan struct{})}
	go func() {
		// The program is not a child process, thus this program cannot wait for it.
		for syscall.Kill(pid, 0) != syscall.ESRCH {
			time.Sleep(pollInterval)
		}
		close(c.done)
	}()
	return c, nil
}

// detach is the second program of startDetached: `sandbox-vm detach [--quiet] PIDFILE
// PROGRAM [ARGUMENT]...`. With --quiet, the program gets no stderr.
func detach(args []string) error {
	quiet := len(args) > 0 && args[0] == "--quiet"
	if quiet {
		args = args[1:]
	}
	if len(args) < 2 {
		return errors.New("usage: sandbox-vm detach [--quiet] PIDFILE PROGRAM [ARGUMENT]...")
	}
	cmd := exec.Command(args[1], args[2:]...)
	cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
	if !quiet {
		cmd.Stderr = os.Stderr
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The umask is for the whole process: the command inherits it, and the pid file gets it.
	// It has no other effect, because this process only starts the command, writes the pid
	// file, and exits.
	syscall.Umask(0o077)
	if err := cmd.Start(); err != nil {
		return err
	}
	return os.WriteFile(args[0], fmt.Appendf(nil, "%d\n", cmd.Process.Pid), 0o644)
}

func (c *child) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *child) kill() {
	syscall.Kill(c.pid, syscall.SIGTERM)
}

// stopPid stops a program by its pid file and waits until the program stops. The program
// can be of this run or of a run that did not stop correctly. pkill also checks the name of the
// program, because a different program can have the pid of an old file. pkill is available
// on macOS and on Linux.
//
// The function calls each after each signal, while the program continues to run.
//
// A program that does not stop in stopGrace gets SIGKILL. Without that, a program that
// ignores SIGTERM stops this run and each subsequent run.
func stopPid(pidfile, program string, each func()) {
	signal := "-TERM"
	deadline := time.Now().Add(stopGrace)
	for exec.Command("pkill", signal, "-F", pidfile, program).Run() == nil {
		if each != nil {
			each()
		}
		time.Sleep(pollInterval)
		if time.Now().Before(deadline) {
			continue
		}
		if signal == "-KILL" {
			warn("could not stop %s (%s)", program, pidfile)
			return
		}
		signal, deadline = "-KILL", time.Now().Add(stopGrace)
	}
}

// The time between two checks of a condition that has no event.
const pollInterval = 200 * time.Millisecond

// The time that a program gets to stop after SIGTERM, and then after SIGKILL. The tests
// change it.
var stopGrace = 10 * time.Second

// waitUntil checks cond until it is true. It is false if the context ended or the time
// was over first.
func waitUntil(ctx context.Context, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) || !sleep(ctx, pollInterval) {
			return false
		}
	}
	return true
}

// helper returns a command that this program waits for: ssh, git, nix. The command
// stops when the context ends.
func helper(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// show sends the output of a command to the output of this program. It is for the commands
// of the host. For a command in the guest, use showGuest.
func show(cmd *exec.Cmd) *exec.Cmd {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// showGuest sends the output of a command in the guest to the output of this program. The
// guest controls that output, thus it goes through the same filter as the console.
func showGuest(cmd *exec.Cmd) *exec.Cmd {
	cmd.Stdout = termsafe.NewWriter(os.Stdout)
	cmd.Stderr = termsafe.NewWriter(os.Stderr)
	return cmd
}

// pipe runs two commands with the output of the first as the input of the second. It
// fails if one of them fails.
func pipe(from, to *exec.Cmd) error {
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	from.Stdout = w
	to.Stdin = r
	err = from.Start()
	if err == nil {
		err = to.Start()
		if err != nil {
			from.Process.Kill()
		}
	}
	r.Close()
	w.Close()
	if err != nil {
		// The error of the start is the result.
		_ = from.Wait()
		return err
	}
	toErr := to.Wait()
	if err := from.Wait(); err != nil {
		return err
	}
	return toErr
}

// sleep waits. It is false if the context ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// shellQuote returns s as one word of a shell command.
func shellQuote(s string) string {
	q := "'"
	for _, r := range s {
		if r == '\'' {
			q += `'\''`
		} else {
			q += string(r)
		}
	}
	return q + "'"
}
