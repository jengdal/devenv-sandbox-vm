//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The console goes through console-filter to s6-log, and the last line is in the file
// after stopLogs. The test needs s6-log: SVM_S6_LOG is its path.
func TestConsoleLog(t *testing.T) {
	s6Log := os.Getenv("SVM_S6_LOG")
	if s6Log == "" {
		t.Skip("SVM_S6_LOG is not set")
	}
	// The rules of the sandbox have the paths without symbolic links.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filter := filepath.Join(home, "console-filter")
	if out, err := exec.Command("go", "build", "-o", filter, "./console-filter").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	store := filepath.Join(home, "store-paths")
	if err := os.WriteFile(store, []byte("/nix/store\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &supervisor{cfg: &config{
		home: home, dir: filepath.Join(home, "vm"), logBytes: "100000",
		s6Log: s6Log, s6LogStorePaths: store,
		consoleFilter: filter, consoleFilterStorePaths: store,
	}}
	if err := s.startLogs(); err != nil {
		s.stopLogs()
		t.Fatal(err)
	}
	// A different program writes the console, and stops.
	f, err := s.logWriter("console")
	if err == nil {
		_, err = f.WriteString("a\x1b[31mb\x1b]0;title\x07\r\r\n")
		f.Close()
	}
	if err != nil {
		s.stopLogs()
		t.Fatal(err)
	}
	if err := s.markBoot(); err != nil {
		s.stopLogs()
		t.Fatal(err)
	}
	filterDone := s.consoleFilter.done
	s.stopLogs()
	select {
	case <-filterDone:
	default:
		t.Error("console-filter did not stop")
	}
	text, err := os.ReadFile(s.cfg.path("log", "console", "current"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "  ab") || !strings.HasSuffix(lines[1], "  "+bootMark) {
		t.Errorf("log of the console: %q", text)
	}
	for _, n := range append([]string{"console-filter"}, "log-console", "log-gvproxy", "log-smokescreen") {
		if exec.Command("pkill", "-0", "-F", s.cfg.path(n+".pid")).Run() == nil {
			t.Errorf("%s did not stop", n)
		}
	}
}
