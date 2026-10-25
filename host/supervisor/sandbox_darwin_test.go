//go:build darwin

package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSandboxProfile compares the text of the rules. These tests start a program with the
// rules and check what the macOS sandbox permits. The program is the test binary
// (probe_test.go).

// check is one operation of the probe, and whether the sandbox permits it.
type check struct {
	op string
	ok bool
}

// sandboxWorld is the files and the ports of one run of the probe: a directory of a VM, as
// the programs of devenv-sandbox-vm have it, and a file of the user.
type sandboxWorld struct {
	t *testing.T
	// The directory of the VM, and a file of the user that is not in it.
	dir, secret string
	// The file with the store paths. The program can read the directory `store`.
	storePaths, store string
}

func newSandboxWorld(t *testing.T) *sandboxWorld {
	t.Helper()
	// The rules of the sandbox have the paths without symbolic links.
	top, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &sandboxWorld{
		t: t, dir: top + "/home/.devenv-sandbox-vm/vm1", secret: top + "/home/secret",
		storePaths: top + "/store-paths", store: top + "/store",
	}
	for _, d := range []string{w.dir + "/log", w.store} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.file(w.secret)
	w.file(w.dir + "/id_ed25519")
	w.file(w.dir + "/ssh.config")
	w.file(w.store + "/file")
	if err := os.WriteFile(w.storePaths, []byte(w.store+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *sandboxWorld) file(name string) string {
	w.t.Helper()
	if err := os.WriteFile(name, []byte("text\n"), 0o644); err != nil {
		w.t.Fatal(err)
	}
	return name
}

// listener returns the port of a program that accepts connections on the loopback address.
func (w *sandboxWorld) listener() string {
	w.t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { l.Close() })
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// freePort returns a port of the loopback address that no program has.
func (w *sandboxWorld) freePort() string {
	w.t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		w.t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// unixListener creates a unix socket that accepts connections. The name of a socket has a
// maximum length, thus its directory is in /tmp.
func (w *sandboxWorld) unixListener(name string) string {
	w.t.Helper()
	dir, err := os.MkdirTemp("/private/tmp", "svm-test-")
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { os.RemoveAll(dir) })
	l, err := net.Listen("unix", dir+"/"+name)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { l.Close() })
	return dir + "/" + name
}

// The operations that each sandbox denies: the files of the user, the other files of the
// directory of the VM, the list of that directory, and a different program.
func (w *sandboxWorld) denied() []check {
	checks := []check{
		{"read=" + w.secret, false},
		{"read=" + w.dir + "/id_ed25519", false},
		{"append=" + w.dir + "/ssh.config", false},
		{"write=" + w.dir + "/new", false},
		{"list=" + w.dir, false},
		{"write=" + w.store + "/new", false},
		{"exec=" + tool(w.t, "echo"), false},
	}
	// The home directory of the user, if this program can list it without a sandbox.
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.ReadDir(home); err == nil {
			checks = append(checks, check{"list=" + home, false})
		}
	}
	return checks
}

// runProbe starts the probe, in a sandbox if c is not nil, and compares its lines with
// the checks. Without a sandbox, each operation must be possible: an operation that fails
// for a different cause shows nothing about the rules.
func runProbe(t *testing.T, c *confinement, checks []check) {
	t.Helper()
	self, err := os.Executable()
	if err == nil {
		self, err = filepath.EvalSymlinks(self)
	}
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"probe"}
	for _, ch := range checks {
		args = append(args, ch.op)
	}
	cmd := exec.Command(self, args...)
	if c != nil {
		if cmd, err = confinedCommand(*c, self, args...); err != nil {
			t.Fatal(err)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(checks) {
		t.Fatalf("the probe gave %d lines for %d operations:\n%s", len(lines), len(checks), out)
	}
	for i, ch := range checks {
		ok := lines[i] == strconv.Quote(ch.op)+" ok"
		switch {
		case c == nil && !ok:
			t.Errorf("without a sandbox: %s", lines[i])
		case c != nil && ok && !ch.ok:
			t.Errorf("the sandbox permits %s", ch.op)
		case c != nil && !ok && ch.ok:
			t.Errorf("the sandbox denies %s", lines[i])
		}
	}
}

func TestSandboxBehaviour(t *testing.T) {
	needSandbox(t)
	// A variable of the shell of the project. confinedEnv does not pass it.
	t.Setenv("SVM_TEST_PASSWORD", "x")
	t.Setenv("SVM_TEST_NAMED", "x")
	t.Setenv("TZ", "UTC")

	tests := []struct {
		name string
		// make returns the confinement and the operations, for the files and the ports of w.
		make func(w *sandboxWorld) (confinement, []check)
	}{
		// console-filter: no file, no network.
		{"no access", func(w *sandboxWorld) (confinement, []check) {
			return confinement{egress: egressNone}, []check{
				{"read=" + w.store + "/file", true},
				{"connect=" + w.listener(), false},
				{"listen=" + w.freePort(), false},
				{"unix-connect=" + w.unixListener("ctl"), false},
				{"env=TZ", true},
				{"env=SVM_TEST_PASSWORD", false},
				{"env=SVM_TEST_NAMED", false},
			}
		}},
		// s6-log and borg: they write only their directories.
		{"write directory", func(w *sandboxWorld) (confinement, []check) {
			return confinement{egress: egressNone, writeDirs: []string{w.dir + "/log"}, env: []string{"SVM_TEST_NAMED"}}, []check{
				{"write=" + w.dir + "/log/current", true},
				{"read=" + w.dir + "/log/current", true},
				{"remove=" + w.dir + "/log/current", true},
				{"write=" + w.dir + "/log2", false},
				{"connect=" + w.listener(), false},
				{"env=SVM_TEST_NAMED", true},
				{"env=SVM_TEST_PASSWORD", false},
			}
		}},
		// The program can write the data of the file, but it cannot put a link in its place.
		{"data file", func(w *sandboxWorld) (confinement, []check) {
			data := w.file(w.dir + "/data")
			return confinement{egress: egressNone, dataFiles: []string{data}}, []check{
				{"append=" + data, true},
				{"read=" + data, true},
				{"symlink=" + data, false},
				{"remove=" + data, false},
			}
		}},
		// The proxy: its port, and no connection to this computer.
		{"internet", func(w *sandboxWorld) (confinement, []check) {
			port := w.freePort()
			checks := []check{
				{"listen=" + port, true},
				{"listen=" + w.freePort(), false},
				{"connect=" + w.listener(), false},
				{"unix-connect=" + w.unixListener("ctl"), false},
			}
			// The other addresses of this computer are also "localhost" for the sandbox.
			if addr := lanAddress(w.t); addr != "" {
				checks = append(checks, check{"dial=" + addr, false})
			}
			return confinement{egress: egressInternet, listen: port}, checks
		}},
		// gvproxy: its port, the port of the proxy, and the sockets of the hypervisor.
		{"local port", func(w *sandboxWorld) (confinement, []check) {
			port, proxy := w.freePort(), w.listener()
			hv := w.unixListener("hv.sock")
			return confinement{
					egress: egressLocal(mustAtoi(w.t, proxy)), listen: port,
					bindSockets: []string{w.dir + "/net.sock"}, connectSocketDirs: []string{filepath.Dir(hv)},
				}, []check{
					{"connect=" + proxy, true},
					{"connect=" + w.listener(), false},
					{"listen=" + port, true},
					{"listen=" + w.freePort(), false},
					{"unix-listen=" + w.dir + "/net.sock", true},
					{"unix-listen=" + w.dir + "/ctl", false},
					{"unix-connect=" + hv, true},
					{"unix-connect=" + w.unixListener("ctl"), false},
				}
		}},
		{"connect socket", func(w *sandboxWorld) (confinement, []check) {
			sock := w.unixListener("one.sock")
			other := w.unixListener("two.sock")
			return confinement{egress: egressNone, connectSockets: []string{sock}}, []check{
				{"unix-connect=" + sock, true},
				{"unix-connect=" + other, false},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The operations change the files, thus each run has new files.
			for _, sandbox := range []bool{false, true} {
				w := newSandboxWorld(t)
				c, checks := tt.make(w)
				c.storePaths = w.storePaths
				if !sandbox {
					// Without a sandbox, the program gets all variables.
					var possible []check
					for _, ch := range append(checks, w.denied()...) {
						if !strings.HasPrefix(ch.op, "env=") {
							possible = append(possible, ch)
						}
					}
					runProbe(t, nil, possible)
					continue
				}
				runProbe(t, &c, append(checks, w.denied()...))
			}
		})
	}
}

// With --exec-closure (borg), the program can start the programs of its store paths, and
// no other program.
func TestSandboxExecClosure(t *testing.T) {
	needSandbox(t)
	for _, sandbox := range []bool{false, true} {
		w := newSandboxWorld(t)
		// A program in the store path: a copy of the test binary. The probe starts
		// it without an operation.
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		prog := w.store + "/prog"
		if err := copyFile(t.Context(), self, prog); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(prog, 0o755); err != nil {
			t.Fatal(err)
		}
		checks := []check{{"exec=" + prog, true}, {"exec=" + tool(w.t, "echo"), false}, {"read=" + w.secret, false}}
		if !sandbox {
			runProbe(t, nil, checks)
			continue
		}
		runProbe(t, &confinement{egress: egressNone, execClosure: true, storePaths: w.storePaths}, checks)
		// Without the option, the program of the store path does not start.
		checks[0].ok = false
		runProbe(t, &confinement{egress: egressNone, storePaths: w.storePaths}, checks)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A path is text in the rules (quote). A path with the characters of the profile language
// must not change the rules: the home directory of a user can have such a name.
func TestSandboxOddPaths(t *testing.T) {
	needSandbox(t)
	for _, name := range []string{
		`a b`, `a"b`, `a\b`, `a\"b`, `a")(allow default)("b`, `a;b`, `a#"b`, "a\nb", "a\tb", `é 日本`, `(subpath "x")`,
	} {
		for _, sandbox := range []bool{false, true} {
			w := newSandboxWorld(t)
			dir := filepath.Join(w.dir, name)
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			checks := append([]check{{"write=" + dir + "/file", true}}, w.denied()...)
			if !sandbox {
				runProbe(t, nil, checks)
				continue
			}
			runProbe(t, &confinement{egress: egressNone, writeDirs: []string{dir}, storePaths: w.storePaths}, checks)
		}
	}
}

// needSandbox skips a test where sandbox-exec does not work, for example in the build of
// Nix: a program in a sandbox cannot create a second sandbox.
func needSandbox(t *testing.T) {
	t.Helper()
	if exec.Command(sandboxExecProgram, "-p", "(version 1)(allow default)", tool(t, "true")).Run() != nil {
		t.Skip("sandbox-exec does not work here")
	}
}
