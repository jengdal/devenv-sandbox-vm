//go:build darwin

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests need a parent process that reaps a detached program after it stops, as
// launchd does on macOS. On Linux, the program can stay a zombie, and kill and pkill
// continue to find it. The supervisor runs only on macOS.

func TestStartDetached(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	out := filepath.Join(t.TempDir(), "out")
	made := filepath.Join(t.TempDir(), "made")
	cmd := exec.Command("sh", "-c", "echo text; touch "+shellQuote(made)+"; exec sleep 30")
	var err error
	if cmd.Stdout, err = os.Create(out); err != nil {
		t.Fatal(err)
	}
	c, err := startDetached(cmd, pidfile)
	if err != nil {
		t.Fatal(err)
	}
	defer c.kill()
	if text, _ := os.ReadFile(pidfile); strings.TrimSpace(string(text)) != strconv.Itoa(c.pid) {
		t.Errorf("pid file: %q, pid %d", text, c.pid)
	}
	// The program is the leader of a session, and it is not in the group of the test.
	if pgid, err := syscall.Getpgid(c.pid); err != nil || pgid != c.pid || pgid == syscall.Getpgrp() {
		t.Errorf("process group %d of pid %d: %v", pgid, c.pid, err)
	}
	// It gets the output of the command.
	time.Sleep(500 * time.Millisecond)
	if text, _ := os.ReadFile(out); string(text) != "text\n" {
		t.Errorf("output: %q", text)
	}
	// Only the user can read the pid file and the files of the command.
	for _, f := range []string{pidfile, made} {
		if st, err := os.Stat(f); err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("mode of %s: %v %v", f, st, err)
		}
	}
	if !c.alive() {
		t.Error("the program is not alive")
	}
	c.kill()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the program did not stop")
	}
	if c.alive() {
		t.Error("the program is alive after it stopped")
	}

	// A pid file is not necessary.
	c, err = startDetached(exec.Command("sleep", "30"), "")
	if err != nil || !c.alive() {
		t.Fatalf("without a pid file: %v", err)
	}
	c.kill()
	<-c.done

	if _, err := startDetached(exec.Command("/does/not/exist"), ""); err == nil {
		t.Error("a program that does not exist gave no error")
	}
}

// A program that ignores SIGTERM must not stop the run.
func TestStopPidKills(t *testing.T) {
	if _, err := exec.LookPath("pkill"); err != nil {
		t.Skip(err)
	}
	defer func(d time.Duration) { stopGrace = d }(stopGrace)
	stopGrace = time.Second
	pidfile := filepath.Join(t.TempDir(), "pid")
	ready := filepath.Join(t.TempDir(), "ready")
	c, err := startDetached(exec.Command("sh", "-c", "trap '' TERM; touch "+shellQuote(ready)+"; while :; do sleep 1; done"), pidfile)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(c.pid, syscall.SIGKILL)
	if !waitUntil(context.Background(), 5*time.Second, func() bool { _, err := os.Stat(ready); return err == nil }) {
		t.Fatal("the program did not start")
	}
	calls := 0
	stopPid(pidfile, "sh", func() { calls++ })
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the program did not stop")
	}
	if calls == 0 {
		t.Error("each was not called")
	}
	// A pid file that does not exist is not an error.
	stopPid(filepath.Join(t.TempDir(), "none"), "sh", nil)
}
