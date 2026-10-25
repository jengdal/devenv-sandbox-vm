//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testHome  = "/Users/tester"
	testVMDir = testHome + "/.devenv-sandbox-vm/vm1"
)

// The files in testdata are the profiles that this program creates. A change to the rules of
// the sandbox shows as a change to these files. To update them, run the tests with
// SVM_UPDATE_GOLDEN=1. The profile language ignores the layout, thus the comparison also
// ignores it.
func TestSandboxProfile(t *testing.T) {
	const dir = testVMDir
	tests := []struct {
		golden, prog string
		c            confinement
	}{
		{"console-filter.sb", "/nix/store/aaa-one/bin/console-filter", confinement{egress: egressNone}},
		{"s6log.sb", "/nix/store/aaa-one/bin/s6-log", confinement{
			egress: egressNone, writeDirs: []string{dir + "/log/console"},
		}},
		{"smokescreen.sb", "/nix/store/aaa-one/bin/smokescreen", confinement{
			listen: "23000", egress: egressInternet,
		}},
		{"gvproxy.sb", "/nix/store/aaa-one/bin/gvproxy", confinement{
			listen: "22000", egress: egressLocal(23000),
			bindSockets: []string{dir + "/net.sock"}, connectSocketDirs: []string{"/private/tmp/svm-1"},
		}},
		{"borg.sb", "/nix/store/aaa-one/bin/borg", confinement{
			egress: egressNone, execClosure: true, writeDirs: []string{"/repo", dir + "/borg"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			tt.c.storePaths = "testdata/store-paths"
			got, err := sandboxProfile(tt.c, tt.prog)
			if err != nil {
				t.Fatal(err)
			}
			compareGolden(t, tt.golden, got)
			deniesDefault(t, got)
		})
	}
}

// compareGolden compares a profile with its file in testdata, or writes the file.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	if os.Getenv("SVM_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile("testdata/"+name, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if tokens(got) != tokens(string(want)) {
		t.Errorf("profile:\n%s\nwant:\n%s", got, want)
	}
}

// deniesDefault fails if a profile permits what no rule names, or if it permits the files
// of the directory of the VM that a program in a sandbox must not get.
func deniesDefault(t *testing.T, profile string) {
	t.Helper()
	text := tokens(profile)
	if !strings.HasPrefix(text, "(version 1) (deny default) ") {
		t.Errorf("the profile does not start with (deny default):\n%s", profile)
	}
	for _, wide := range []string{
		"(allow default)", "(allow network*)", "(allow network-outbound)", "(allow mach-lookup)",
		"(allow file-read*)", "(allow file-write*)", "(allow iokit-open)", "(allow process-exec)",
		// A rule for all unix sockets or all files of the directory of the VM is also a rule
		// for the control socket of ssh. A program that connects to that socket can open
		// forwards on the host.
		`(subpath "` + testVMDir + `")`, `(subpath "` + testHome + `/.devenv-sandbox-vm")`,
		`(subpath "` + testHome + `")`,
		// These are files of this program and of ssh.
		"ssh.config", "id_ed25519", "known_hosts", "/ctl", ".pid", "/env",
		// A rule with a pattern is a rule for the files of other programs.
		"(regex ",
	} {
		if strings.Contains(text, wide) {
			t.Errorf("the profile has the rule %s:\n%s", wide, profile)
		}
	}
}

// The proxy connects to the internet. Its only unix socket is the resolver of the system:
// it cannot connect to the sockets of other services of this computer.
func TestSandboxProfileInternet(t *testing.T) {
	got, err := sandboxProfile(confinement{
		listen: "23000", egress: egressInternet, storePaths: "testdata/store-paths",
	}, "/p")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "unix-socket") {
		t.Errorf("the proxy has rules for unix sockets:\n%s", got)
	}
	allow := `(allow network-outbound (remote tcp "*:*"))`
	deny := `(deny network-outbound (remote ip "localhost:*"))`
	// The last rule that matches decides, thus the deny rule is after the allow rule.
	if a, d := strings.Index(got, allow), strings.Index(got, deny); a < 0 || d < a {
		t.Errorf("the proxy can connect to this computer:\n%s", got)
	}
	if n := strings.Count(got, "network-outbound"); n != 3 {
		t.Errorf("%d rules for connections, want 3:\n%s", n, got)
	}
}

func TestAncestors(t *testing.T) {
	got := ancestors([]string{"/nix/store/aaa-one", "/Users/tester/.vm/log", "/Users/tester/.vm/net.sock", "/p"})
	want := []string{"/Users", "/Users/tester", "/Users/tester/.vm", "/nix", "/nix/store"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("ancestors: %q, want %q", got, want)
	}
}

// tokens removes the layout of a profile.
func tokens(profile string) string {
	s := strings.Join(strings.Fields(profile), " ")
	return strings.ReplaceAll(s, " )", ")")
}

func TestSandboxProfileErrors(t *testing.T) {
	if _, err := sandboxProfile(confinement{egress: "all", storePaths: "testdata/store-paths"}, "/p"); err == nil {
		t.Error("an unknown egress gave no error")
	}
	if _, err := sandboxProfile(confinement{egress: egressNone, storePaths: "testdata/none"}, "/p"); err == nil {
		t.Error("a store paths file that does not exist gave no error")
	}
}

func TestQuote(t *testing.T) {
	if got := quote(`/a "b"\c é`); got != `"/a \"b\"\\c é"` {
		t.Error(got)
	}
}

func testConfig() *config {
	return &config{
		home: testHome, dir: testVMDir,
		krunkit: "/nix/store/aaa-one/bin/krunkit", krunkitStorePaths: "testdata/store-paths",
	}
}

func TestKrunkitProfile(t *testing.T) {
	c := testConfig()
	got, err := krunkitProfile(c, "/private/tmp/svm-1", "")
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "krunkit.sb", got)
	deniesDefault(t, got)
	// krunkit gets the socket of its own pid only.
	if n := strings.Count(got, "krunkit_timesync_"+pidToken+".sock"); n != 3 || strings.Count(got, "krunkit_timesync_") != n {
		t.Errorf("the rules for the timesync socket:\n%s", got)
	}

	// The installer also gets its copy of the ISO.
	boot := c.path("install", "installer.iso")
	got, err = krunkitProfile(c, "/private/tmp/svm-1", boot)
	if err != nil {
		t.Fatal(err)
	}
	if rule := `(allow file-read* file-write-data (literal "` + boot + `"))`; !strings.Contains(got, rule) {
		t.Errorf("no rule %s:\n%s", rule, got)
	}
	deniesDefault(t, got)
}

// A program can listen on its own socket and connect to the sockets that its confinement
// names. It gets no rule for the other sockets of the same directory.
func TestSandboxProfileSockets(t *testing.T) {
	got, err := sandboxProfile(confinement{
		egress: egressLocal(23000), storePaths: "testdata/store-paths",
		bindSockets: []string{"/vm/net.sock"}, connectSockets: []string{"/vm/other.sock"},
		connectSocketDirs: []string{"/private/tmp/svm-1"},
	}, "/p")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		`(allow file-read-metadata file-write-create (literal "/vm/net.sock"))`,
		`(allow network-bind network-inbound (local unix-socket (literal "/vm/net.sock")))`,
		`(allow network-outbound (remote unix-socket (literal "/vm/other.sock")))`,
		`(allow network-outbound (remote unix-socket (subpath "/private/tmp/svm-1")))`,
	} {
		if !strings.Contains(got, rule) {
			t.Errorf("no rule %s:\n%s", rule, got)
		}
	}
	if n := strings.Count(got, "unix-socket"); n != 3 {
		t.Errorf("%d rules for unix sockets, want 3:\n%s", n, got)
	}
	if strings.Contains(got, `(subpath "/vm")`) || strings.Contains(got, "file-write*") {
		t.Errorf("the program can write the directory of the socket:\n%s", got)
	}
}

func TestKrunkitCommand(t *testing.T) {
	c := testConfig()
	count := func(args []string, option, value string) int {
		n := 0
		for i := 1; i < len(args); i++ {
			if args[i-1] == option && args[i] == value {
				n++
			}
		}
		return n
	}
	iso := c.path("install", "installer.iso")
	isoDisk := "virtio-blk,path=" + iso + ",format=raw"

	t.Setenv("SECRET_KEY", "1")
	t.Setenv("RUST_LOG", "trace")
	start, err := hvStartCommand(c, "/private/tmp/svm-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(start.Args, " ")
	if count(start.Args, "--device", isoDisk) != 0 || strings.Contains(text, "--oem-string") {
		t.Errorf("start: %q", start.Args)
	}
	// This program starts krunkit through itself, and it writes the pid file.
	if start.Args[1] != "exec-sandbox" || !strings.Contains(start.Args[2], pidToken) || strings.Contains(text, "--pidfile") {
		t.Errorf("start: %q", start.Args)
	}
	if env := strings.Join(start.Env, " "); strings.Contains(env, "SECRET_KEY") || !strings.Contains(env, "TMPDIR=/private/tmp/svm-1") {
		t.Errorf("environment: %q", start.Env)
	}
	// Not the RUST_LOG of the shell.
	if env := strings.Join(start.Env, " "); !strings.Contains(env, "RUST_LOG="+krunkitLogFilter) || strings.Contains(env, "RUST_LOG=trace") {
		t.Errorf("environment: %q", start.Env)
	}

	installer, err := hvInstallerCommand(c, "/private/tmp/svm-1", iso, []string{"a=1", "b=2"})
	if err != nil {
		t.Fatal(err)
	}
	if count(installer.Args, "--device", isoDisk) != 1 || count(installer.Args, "--oem-string", "a=1") != 1 ||
		count(installer.Args, "--oem-string", "b=2") != 1 {
		t.Errorf("installer: %q", installer.Args)
	}
}

// execSandbox puts the pid of the program in the rules: the program cannot write the file
// that has its pid as its name, and it can write a different file.
func TestExecSandbox(t *testing.T) {
	needSandbox(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	profile := "(version 1)(allow default)(deny file-write* (literal " + quote(dir+"/"+pidToken) + "))"
	for _, tc := range []struct {
		name string
		ok   bool
	}{{"$$", false}, {"other", true}} {
		err := exec.Command(self, "exec-sandbox", profile, tool(t, "sh"), "-c", "echo x > "+shellQuote(dir)+"/"+tc.name).Run()
		if (err == nil) != tc.ok {
			t.Errorf("write of the file %s: %v", tc.name, err)
		}
	}
}
