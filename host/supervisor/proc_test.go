package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

// startDetached starts the test binary a second time, as `sandbox-vm detach`. The
// command of the hypervisor starts it as `sandbox-vm exec-sandbox`. The tests of the sandbox
// rules start it as `probe` (probe_test.go). A copy with the name of a program of the VM is
// that program for the tests of the sandbox-vm process (fakes_test.go).
func TestMain(m *testing.M) {
	// os.Exit waits one second with the race detector. syscall.Exit does not.
	if fakeProgram(filepath.Base(os.Args[0])) {
		syscall.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "probe" {
		probe(os.Args[2:])
		syscall.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "exec-sandbox" {
		_ = execSandbox(os.Args[2:])
		syscall.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "detach" {
		if err := detach(os.Args[2:]); err != nil {
			syscall.Exit(1)
		}
		syscall.Exit(0)
	}
	// The git repos of the tests do not read the git configuration of the user, for example
	// a key that signs each commit.
	_ = os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	_ = os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

func TestPipe(t *testing.T) {
	ctx := context.Background()
	out := filepath.Join(t.TempDir(), "out")
	if err := pipe(helper(ctx, "echo", "text"), helper(ctx, "sh", "-c", "cat > "+shellQuote(out))); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "text\n" {
		t.Errorf("output: %q", got)
	}
	if pipe(helper(ctx, "false"), helper(ctx, "cat")) == nil {
		t.Error("no error when the first command failed")
	}
	if pipe(helper(ctx, "true"), helper(ctx, "false")) == nil {
		t.Error("no error when the second command failed")
	}
}

func TestShellQuote(t *testing.T) {
	text := `a b'c"$d`
	out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(text)).Output()
	if err != nil || string(out) != text {
		t.Errorf("%q through the shell: %q %v", text, out, err)
	}
}

// The shell gives the text again, for each text. To look for new inputs:
// go test -fuzz FuzzShellQuote .
func FuzzShellQuote(f *testing.F) {
	for _, seed := range []string{"", "a b", `a'b`, `''`, "$(id) `id` \\ \"", "a\nb", "-n", "\x01*?[~ \u00e9"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		// An argument of a program cannot have the byte 0. The shell of macOS changes bytes
		// that are not UTF-8.
		if strings.ContainsRune(text, 0) || !utf8.ValidString(text) {
			t.Skip()
		}
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(text)).Output()
		if err != nil || string(out) != text {
			t.Errorf("%q through the shell: %q %v", text, out, err)
		}
	})
}

func TestWaitUntil(t *testing.T) {
	n := 0
	if !waitUntil(context.Background(), time.Minute, func() bool { n++; return n == 3 }) {
		t.Error("false for a condition that became true")
	}
	if waitUntil(context.Background(), 0, func() bool { return false }) {
		t.Error("true after the time was over")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitUntil(ctx, time.Minute, func() bool { return false }) {
		t.Error("true after the context ended")
	}
}

func TestStartOnPort(t *testing.T) {
	ctx := context.Background()
	var started []int
	// The program of the first port stops; that of the second port listens.
	port, prog, err := startOnPort(ctx, []int{1, 2, 3}, func(p int) (*child, error) {
		started = append(started, p)
		c := &child{done: make(chan struct{})}
		if p == 1 {
			close(c.done)
		}
		return c, nil
	}, func(p int) bool { return p == 2 })
	if err != nil || port != 2 || len(started) != 2 || prog == nil || !prog.alive() {
		t.Errorf("port %d, started %v, program %v: %v", port, started, prog, err)
	}
	if _, _, err := startOnPort(ctx, nil, nil, nil); err == nil {
		t.Error("no error without a port")
	}
}

func TestLockVM(t *testing.T) {
	c := &config{name: "vm1", dir: t.TempDir()}
	lock, err := lockVM(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockVM(c); err == nil {
		t.Error("a second lock gave no error")
	}
	lock.Close()
	again, err := lockVM(c)
	if err != nil {
		t.Errorf("after the first lock ended: %v", err)
	} else {
		again.Close()
	}
}
