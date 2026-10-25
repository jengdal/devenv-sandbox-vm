//go:build darwin

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests of the sequence of the sandbox-vm process (run.go). There is no VM: the
// hypervisor, gvproxy, the proxy and ssh are fakes (fakes_test.go). The
// sandboxes, the pid files, the pipes of the logs and s6-log are the real ones. The tests
// need s6-log: SVM_S6_LOG is its path.

// runWorld is one VM of the test and the directory of its programs.
type runWorld struct {
	t   *testing.T
	cfg *config
	// The directory of the fakes and of their files.
	bin string
	// The result of run.
	done chan error
}

func newRunWorld(t *testing.T) *runWorld {
	t.Helper()
	// Each test starts the programs of a VM and waits for them.
	if testing.Short() {
		t.Skip("not with -short")
	}
	s6Log := os.Getenv("SVM_S6_LOG")
	if s6Log == "" {
		t.Skip("SVM_S6_LOG is not set")
	}
	needSandbox(t)
	// The path of a unix socket has a maximum length, thus the directory is in /tmp.
	top, err := os.MkdirTemp("/private/tmp", "svm-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(top) })
	w := &runWorld{t: t, bin: top + "/bin", done: make(chan error, 1)}
	home := top + "/home"
	for _, d := range []string{w.bin, home} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range fakeNames {
		if err := cloneFile(context.Background(), self, w.file(name)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(w.file(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.put("ports", "#!"+tool(t, "sh")+"\necho \"$*\" >>"+shellQuote(w.file("ports.log"))+"\n")
	if err := os.Chmod(w.file("ports"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The programs in a sandbox read their own files, and s6-log reads its libraries.
	w.put("store-paths", "/nix/store\n"+w.bin+"\n")
	store := w.file("store-paths")
	w.cfg = &config{
		name: "vm1", hostname: "vm", user: "tester",
		root: top + "/project", configDir: "sandbox-vm", configName: "devenv-sandbox-vm",
		cpus: "1", memoryMiB: "1024", diskGB: 1, logBytes: "100000",
		guestMarker: top + "/guest-marker", guestState: "/var/lib/devenv-sandbox-vm",
		guestMAC: "5a:94:ef:e4:0c:ee", proxyGateway: "192.168.127.254", timesyncPort: "1234",
		guestArch:   "aarch64",
		smokescreen: w.file("smokescreen"), smokescreenConfig: "/dev/null", smokescreenStorePaths: store,
		gvproxy: w.file("gvproxy"), gvproxyStorePaths: store,
		s6Log: s6Log, s6LogStorePaths: store,
		consoleFilter: w.file("console-filter"), consoleFilterStorePaths: store,
		krunkit: w.file("krunkit"), krunkitStorePaths: store,
		ports: w.file("ports"),
		home:  home, dir: home + "/.devenv-sandbox-vm/vm1",
	}
	// A VM that is installed.
	if err := os.MkdirAll(w.cfg.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"installed", "disk.img"} {
		if err := os.WriteFile(w.cfg.path(f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// run finds ssh in PATH.
	t.Setenv("PATH", w.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Without this, a signal before run is ready for it stops the test binary.
	// run makes the signals have no effect at its end.
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGTERM)
	t.Cleanup(func() {
		signal.Stop(signals)
		signal.Reset(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	})
	// A test that fails must not leave programs.
	t.Cleanup(func() {
		for _, pidfile := range w.pidFiles() {
			if pid := w.pid(pidfile); pid > 0 {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	return w
}

func (w *runWorld) file(name string) string { return filepath.Join(w.bin, name) }

func (w *runWorld) put(name, text string) {
	w.t.Helper()
	if err := os.WriteFile(w.file(name), []byte(text), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *runWorld) text(name string) string {
	text, _ := os.ReadFile(w.file(name))
	return string(text)
}

// lines returns the lines of a file of the directory of the programs that contain the text.
func (w *runWorld) lines(name, part string) []string {
	var lines []string
	for _, line := range strings.Split(w.text(name), "\n") {
		if line != "" && strings.Contains(line, part) {
			lines = append(lines, line)
		}
	}
	return lines
}

func (w *runWorld) exists(vmFile string) bool {
	_, err := os.Stat(w.cfg.path(vmFile))
	return err == nil
}

// The pid files of the programs that run starts.
func (w *runWorld) pidFiles() []string {
	return []string{
		"krunkit.pid", "gvproxy.pid", "smokescreen.pid", "ssh.pid",
		"console-filter.pid", "log-console.pid", "log-gvproxy.pid", "log-smokescreen.pid",
	}
}

// pid returns the pid of a pid file of the VM, or 0.
func (w *runWorld) pid(pidfile string) int {
	text, _ := os.ReadFile(w.cfg.path(pidfile))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(text)))
	return pid
}

// pids returns the pid of each program that runs.
func (w *runWorld) pids() map[string]int {
	pids := map[string]int{}
	for _, f := range w.pidFiles() {
		if pid := w.pid(f); pid > 0 && syscall.Kill(pid, 0) == nil {
			pids[f] = pid
		}
	}
	return pids
}

// start starts run.
func (w *runWorld) start() {
	go func() { w.done <- run(w.cfg) }()
}

// waitFor waits until the condition is true. It fails if run stops first.
func (w *runWorld) waitFor(what string, cond func() bool) {
	w.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !cond() {
		select {
		case err := <-w.done:
			w.t.Fatalf("run stopped before %s: %v\n%s", what, err, w.logs())
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("no %s\n%s", what, w.logs())
		}
	}
}

// waitReady waits until the VM is ready and returns the programs that run.
func (w *runWorld) waitReady() map[string]int {
	w.t.Helper()
	w.waitFor("file `ready`", func() bool { return w.exists("ready") })
	pids := w.pids()
	if len(pids) != len(w.pidFiles()) {
		w.t.Errorf("the programs that run: %v", pids)
	}
	return pids
}

// guest makes the guest reboot or power off.
func (w *runWorld) guest(command string) {
	w.t.Helper()
	w.put("vm-command", command+" "+strconv.Itoa(w.pid("krunkit.pid"))+"\n")
}

// signal sends the signal of the process manager to run.
func (w *runWorld) signal() {
	w.t.Helper()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		w.t.Fatal(err)
	}
}

// wait waits for the end of run and returns its error. After the end, no program of the run
// runs, and the files of the run are not there.
func (w *runWorld) wait(pids map[string]int) error {
	w.t.Helper()
	var err error
	select {
	case err = <-w.done:
	case <-time.After(60 * time.Second):
		w.t.Fatalf("run did not stop\n%s", w.logs())
	}
	for name, pid := range pids {
		if syscall.Kill(pid, 0) == nil {
			w.t.Errorf("after run: %s (%d) runs", name, pid)
		}
	}
	for _, f := range []string{"ready", "forwards", "wanted", "install"} {
		if w.exists(f) {
			w.t.Errorf("after run: the file %s is there", f)
		}
	}
	// A subsequent run can get the lock.
	lock, lockErr := lockVM(w.cfg)
	if lockErr != nil {
		w.t.Errorf("after run: %v", lockErr)
	} else {
		lock.Close()
	}
	return err
}

// logs returns the logs of the programs, for the message of a test that fails.
func (w *runWorld) logs() string {
	var b strings.Builder
	for _, f := range []string{"log/console/current", "log/gvproxy/current", "log/smokescreen/current"} {
		text, _ := os.ReadFile(w.cfg.path(f))
		b.WriteString(f + ":\n" + string(text))
	}
	b.WriteString("ssh.log:\n" + w.text("ssh.log") + "ports.log:\n" + w.text("ports.log"))
	return b.String()
}

// A start, and the signal of the process manager.
func TestRunAndShutdown(t *testing.T) {
	w := newRunWorld(t)
	c := w.cfg
	w.start()
	pids := w.waitReady()

	// The files of the directory of the VM.
	env, _ := os.ReadFile(c.path("env"))
	ports := envProxyPorts(string(env))
	if len(ports) != 1 || !strings.Contains(string(env), "hostname=\"vm\"\nuser=\"tester\"\n") {
		t.Errorf("the file env: %q", env)
	}
	sshConfig, _ := os.ReadFile(c.path("ssh.config"))
	if len(sshConfigPorts(string(sshConfig))) != 1 || !strings.Contains(string(sshConfig), "Host vm1\n") {
		t.Errorf("the file ssh.config: %q", sshConfig)
	}
	if st, err := os.Stat(c.dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("the directory of the VM: %v %v", st, err)
	}
	if root, _ := os.ReadFile(c.path("root")); string(root) != c.root+"\n" {
		t.Errorf("the file root: %q", root)
	}
	// The guest got the settings, and the forwards were applied on the new connection.
	if got := w.text("guest-env"); got != string(env) || got == "" {
		t.Errorf("the guest got the settings %q, the file env has %q", got, env)
	}
	if got := w.lines("ssh.log", "systemctl restart"); len(got) != 1 {
		t.Errorf("the restart of the services of the guest: %q", got)
	}
	if got := w.lines("ports.log", ""); len(got) != 1 || got[0] != "--connected" {
		t.Errorf("the ports program: %q", got)
	}
	if st, err := os.Stat(c.path("ctl")); err != nil || st.Mode()&os.ModeSocket == 0 {
		t.Errorf("the control socket: %v", err)
	}
	// The console of the guest is in its log.
	w.waitFor("console in the log", func() bool {
		text, _ := os.ReadFile(c.path("log", "console", "current"))
		return strings.Contains(string(text), bootMark) && strings.Contains(string(text), "Reached target Multi-User System")
	})

	// A second sandbox-vm process must not run the same VM: it stops the hypervisor of
	// the first.
	if err := run(c); err == nil || !strings.Contains(err.Error(), "already runs the VM vm1") {
		t.Errorf("a second run: %v", err)
	}
	if got := w.pids(); len(got) != len(pids) {
		t.Errorf("after a second run, the programs that run: %v", got)
	}

	// The signal shuts down the guest. The process manager can send it again.
	w.signal()
	w.signal()
	if err := w.wait(pids); err != nil {
		t.Errorf("run: %v", err)
	}
	if got := w.lines("ssh.log", "sudo poweroff"); len(got) != 1 {
		t.Errorf("the shutdown of the guest: %q", got)
	}
	if text, _ := os.ReadFile(c.path("log", "console", "current")); !strings.Contains(string(text), "Reached target System Power Off") {
		t.Errorf("the last line of the console is not in the log:\n%s", text)
	}
	if dirs, _ := filepath.Glob("/tmp/svm-*/krunkit-*"); len(dirs) != 0 {
		t.Errorf("the directory of the network socket: %v", dirs)
	}

	// The subsequent run keeps the ports, thus ssh.config and the settings of the guest
	// stay correct.
	w.start()
	pids = w.waitReady()
	env2, _ := os.ReadFile(c.path("env"))
	sshConfig2, _ := os.ReadFile(c.path("ssh.config"))
	if string(env2) != string(env) || string(sshConfig2) != string(sshConfig) {
		t.Errorf("the second run changed the ports:\n%s%s", env2, sshConfig2)
	}
	// The guest powers off: run stops without a signal.
	w.guest("poweroff")
	if err := w.wait(pids); err != nil {
		t.Errorf("run after the guest powered off: %v", err)
	}
}

// The hypervisor stops when the guest reboots. run boots the guest again, with a new
// network, and applies the forwards of the last run.
func TestRunReboot(t *testing.T) {
	w := newRunWorld(t)
	w.start()
	first := w.waitReady()
	w.guest("reboot")
	w.waitFor("second boot", func() bool {
		return len(w.lines("ports.log", "")) == 2 && w.exists("ready") && w.pid("krunkit.pid") != first["krunkit.pid"]
	})
	if got := w.lines("ports.log", ""); got[1] != "--connected --again" {
		t.Errorf("the ports program: %q", got)
	}
	if got := w.lines("ssh.log", "install -D"); len(got) != 2 {
		t.Errorf("the settings of the guest were sent %d times", len(got))
	}
	second := w.waitReady()
	for _, f := range []string{"krunkit.pid", "gvproxy.pid", "smokescreen.pid", "ssh.pid"} {
		if first[f] == second[f] {
			t.Errorf("%s: the same program after the reboot", f)
		}
		if syscall.Kill(first[f], 0) == nil {
			t.Errorf("%s: the program of the first boot runs", f)
		}
	}
	// The logs continue.
	for _, f := range []string{"console-filter.pid", "log-console.pid", "log-gvproxy.pid", "log-smokescreen.pid"} {
		if first[f] != second[f] {
			t.Errorf("%s: a different program after the reboot", f)
		}
	}
	w.signal()
	if err := w.wait(second); err != nil {
		t.Errorf("run: %v", err)
	}
}

// A signal during the start stops the run. There is no guest to shut down.
func TestRunSignalDuringStart(t *testing.T) {
	w := newRunWorld(t)
	w.put("proxy-wait", "")
	w.start()
	w.waitFor("proxy", func() bool { return w.pid("smokescreen.pid") > 0 })
	pids := w.pids()
	w.signal()
	if err := w.wait(pids); !errors.Is(err, context.Canceled) {
		t.Errorf("run: %v", err)
	}
	if got := w.lines("ssh.log", ""); len(got) != 0 {
		t.Errorf("the commands in the guest: %q", got)
	}
	if w.pid("krunkit.pid") != 0 {
		t.Error("the hypervisor started")
	}
}

// A guest that accepts the shutdown and does not stop: run powers it off.
func TestRunShutdownTimeout(t *testing.T) {
	defer func(d time.Duration) { shutdownTimeout = d }(shutdownTimeout)
	shutdownTimeout = time.Second
	w := newRunWorld(t)
	w.put("poweroff-ignored", "")
	w.start()
	pids := w.waitReady()
	w.signal()
	if err := w.wait(pids); err != nil {
		t.Errorf("run: %v", err)
	}
	if got := w.lines("ssh.log", "sudo poweroff"); len(got) != 1 {
		t.Errorf("the shutdown of the guest: %q", got)
	}
}

// A guest that does not answer SSH is not ready. The signal powers it off.
func TestRunGuestWithoutSSH(t *testing.T) {
	w := newRunWorld(t)
	w.put("ssh-down", "")
	w.start()
	w.waitFor("hypervisor", func() bool { return len(w.lines("ssh.log", " true")) > 0 && w.pid("krunkit.pid") > 0 })
	if w.exists("ready") {
		t.Error("the VM is ready without SSH")
	}
	pids := w.pids()
	w.signal()
	if err := w.wait(pids); err != nil {
		t.Errorf("run: %v", err)
	}
}

// The programs of a run that did not stop correctly: the subsequent run stops them.
func TestRunStopsOlderPrograms(t *testing.T) {
	w := newRunWorld(t)
	w.start()
	older := w.waitReady()
	w.signal()
	// The first run is not complete: these programs continue, as after a run that was
	// killed. They have the names and the pid files of the programs of a run.
	if err := w.wait(older); err != nil {
		t.Fatalf("run: %v", err)
	}
	left := map[string]int{}
	for pidfile, name := range map[string]string{"krunkit.pid": "krunkit", "gvproxy.pid": "gvproxy", "smokescreen.pid": "smokescreen", "ssh.pid": "ssh"} {
		cmd := exec.Command(w.file(name), "-M", "-S", w.cfg.path("ctl"), "--listen-port", "0", "-ssh-port", "0",
			"-listen-vfkit", "unixgram://"+w.cfg.path("net.sock"))
		os.Remove(w.cfg.path("ctl"))
		os.Remove(w.cfg.path("net.sock"))
		c, err := startDetached(cmd, w.cfg.path(pidfile))
		if err != nil {
			t.Fatal(err)
		}
		left[pidfile] = c.pid
	}
	if err := os.WriteFile(w.cfg.path("ready"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	w.start()
	w.waitFor("new hypervisor", func() bool { pid := w.pid("krunkit.pid"); return pid > 0 && pid != left["krunkit.pid"] })
	for pidfile, pid := range left {
		if syscall.Kill(pid, 0) == nil {
			t.Errorf("%s: the program of the older run (%d) runs", pidfile, pid)
		}
	}
	pids := w.waitReady()
	w.signal()
	if err := w.wait(pids); err != nil {
		t.Errorf("run: %v", err)
	}
}

// The connection for the forwards stops while the VM runs: run opens it again and
// applies the forwards of the last run.
func TestRunForwardsConnectionStops(t *testing.T) {
	w := newRunWorld(t)
	w.start()
	pids := w.waitReady()
	if err := syscall.Kill(pids["ssh.pid"], syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	w.waitFor("new connection", func() bool {
		pid := w.pid("ssh.pid")
		return pid != pids["ssh.pid"] && syscall.Kill(pid, 0) == nil && len(w.lines("ports.log", "")) == 2
	})
	if got := w.lines("ports.log", ""); got[1] != "--connected --again" {
		t.Errorf("the ports program: %q", got)
	}
	if !w.exists("ready") || w.pid("krunkit.pid") != pids["krunkit.pid"] {
		t.Error("the VM did not continue")
	}
	pids = w.pids()
	w.signal()
	if err := w.wait(pids); err != nil {
		t.Errorf("run: %v", err)
	}
}

// The VM of a different project, the guest itself, and names that are not correct.
func TestRunRefuses(t *testing.T) {
	w := newRunWorld(t)
	c := w.cfg
	expect := func(what, message string) {
		t.Helper()
		err := run(c)
		if err == nil || !strings.Contains(err.Error(), message) {
			t.Errorf("%s: %v", what, err)
		}
		if got := w.pids(); len(got) != 0 {
			t.Errorf("%s: programs run: %v", what, got)
		}
	}
	if err := os.WriteFile(c.path("root"), []byte("/other/project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	expect("VM of a different project", "is the VM of the project /other/project")
	os.Remove(c.path("root"))

	if err := os.WriteFile(c.guestMarker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	expect("guest", "this is the VM itself")
	os.Remove(c.guestMarker)

	for _, tt := range []struct {
		set     func(*config)
		message string
	}{
		{func(c *config) { c.name = "a b" }, "is not a valid name of a VM"},
		{func(c *config) { c.hostname = "-vm" }, "is not a valid hostname"},
		{func(c *config) { c.user = "Root;id" }, "is not a valid Linux user name"},
		{func(c *config) { c.dir += strings.Repeat("/long", 20) }, "is too long for a unix socket path"},
	} {
		changed := *w.cfg
		tt.set(&changed)
		c = &changed
		expect(tt.message, tt.message)
	}
}

// newVM makes the VM of the world a VM that is not installed, with an installer ISO and a
// project in a git repo.
func (w *runWorld) newVM() {
	w.t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		w.t.Skip(err)
	}
	c := w.cfg
	for _, f := range []string{"installed", "disk.img"} {
		os.Remove(c.path(f))
	}
	if err := os.MkdirAll(c.root+"/sandbox-vm", 0o755); err != nil {
		w.t.Fatal(err)
	}
	for name, text := range map[string]string{c.root + "/sandbox-vm/flake.nix": "{ }\n", w.file("installer.iso"): "iso"} {
		if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
			w.t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "files"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", c.root}, args...)...).CombinedOutput(); err != nil {
			w.t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	c.installerISO = w.file("installer.iso")
	c.nixOptions = `--option extra-substituters "https://cache.example"`
}

// A new VM with an installer ISO: the installer gets the files of the host and installs
// NixOS, and then the installed system boots.
func TestRunInstallsFromISO(t *testing.T) {
	w := newRunWorld(t)
	w.newVM()
	c := w.cfg
	// The home-manager program fails the first time. It removes the file home-pending when it
	// succeeds, as home.sh does.
	w.put("home", "#!"+tool(t, "sh")+"\necho run >>"+shellQuote(w.file("home.log"))+
		"\nif [ -e "+shellQuote(w.file("home-fail"))+" ]; then exit 1; fi\nrm "+shellQuote(c.path("home-pending"))+"\n")
	if err := os.Chmod(w.file("home"), 0o755); err != nil {
		t.Fatal(err)
	}
	w.put("home-fail", "")
	c.homeProgram = w.file("home")
	w.start()
	pids := w.waitReady()
	w.waitFor("the home-manager program", func() bool { return len(w.lines("home.log", "")) == 1 })

	tree, err := exec.Command("git", "-C", c.root, "rev-parse", "HEAD:sandbox-vm").Output()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(c.path("id_ed25519.pub"))
	env, _ := os.ReadFile(c.path("env"))
	proxy := "http://192.168.127.254:" + strconv.Itoa(envProxyPorts(string(env))[0])
	// The commands in the installer, in sequence, and the input of each one.
	steps := []struct{ command, input string }{
		{"mkdir /tmp/devenv-sandbox-vm && tar -x -m -C /tmp/devenv-sandbox-vm", "src/sandbox-vm/flake.nix"},
		{"cat > /tmp/devenv-sandbox-vm/env", string(env)},
		{"cat > /tmp/devenv-sandbox-vm/authorized_keys", string(key)},
		{"cat > /tmp/devenv-sandbox-vm/provisioned", string(tree)},
		{"http_proxy=" + proxy + " https_proxy=" + proxy + " HTTPS_PROXY=" + proxy +
			" bash -s -- '/tmp/devenv-sandbox-vm' '/var/lib/devenv-sandbox-vm'" +
			" 'path:/tmp/devenv-sandbox-vm/src/sandbox-vm#devenv-sandbox-vm' " + c.nixOptions, string(guestInstall)},
		{"poweroff", ""},
	}
	var installer []string
	inputs := map[string]string{}
	for i, line := range strings.Split(w.text("ssh.log"), "\n") {
		// The installer has the user root. The first commands only wait for it.
		if _, command, ok := strings.Cut(line, " root@127.0.0.1 "); ok && command != "true" {
			installer = append(installer, command)
			inputs[command] = w.text("stdin-" + strconv.Itoa(i+1))
		}
		// The host knows the key of the installer before the boot, and uses only the key
		// of the VM.
		if strings.Contains(line, "root@127.0.0.1") {
			for _, option := range []string{"StrictHostKeyChecking=yes", "-F /dev/null", "IdentitiesOnly=yes"} {
				if !strings.Contains(line, option) {
					t.Errorf("ssh for the installer has no %s: %s", option, line)
				}
			}
		}
	}
	if len(installer) != len(steps) {
		t.Fatalf("the commands in the installer: %q", installer)
	}
	for i, step := range steps {
		if installer[i] != step.command {
			t.Errorf("command %d in the installer: %q, want %q", i, installer[i], step.command)
		}
		// The first input is an archive: it has the name of the file.
		if got := inputs[step.command]; got != step.input && (i != 0 || !strings.Contains(got, step.input)) {
			t.Errorf("the input of %q: %q, want %q", step.command, got, step.input)
		}
	}
	// The installed system has a different host key: it is not in a file of the installer.
	if !w.exists("installed") || w.exists("install") || w.exists("first-boot") {
		t.Error("the files of the directory of the VM after the installation")
	}
	if st, err := os.Stat(c.path("disk.img")); err != nil || st.Size() != 1<<30 || st.Mode().Perm() != 0o644&^0o022 && st.Mode().Perm() != 0o600 {
		t.Errorf("the disk: %v %v", st, err)
	}
	w.signal()
	if err := w.wait(pids); err != nil {
		t.Errorf("run: %v", err)
	}

	// The subsequent run does not install again. The home-manager program failed, thus it
	// runs again.
	if !w.exists("home-pending") {
		t.Error("the file home-pending is not there after the program failed")
	}
	os.Remove(w.file("home-fail"))
	w.put("ssh.log", "")
	w.start()
	pids = w.waitReady()
	if got := w.lines("ssh.log", "root@127.0.0.1"); len(got) != 0 {
		t.Errorf("the second run used the installer: %q", got)
	}
	w.waitFor("the home-manager program again", func() bool { return !w.exists("home-pending") })
	w.signal()
	if err := w.wait(pids); err != nil {
		t.Errorf("run: %v", err)
	}

	// After it succeeded, a start does not run it.
	w.start()
	pids = w.waitReady()
	w.signal()
	if err := w.wait(pids); err != nil {
		t.Errorf("run: %v", err)
	}
	if got := w.lines("home.log", ""); len(got) != 2 {
		t.Errorf("the home-manager program ran %d times, want 2", len(got))
	}
}

// An installation that fails stops the installer, and the VM is not installed.
func TestRunInstallFails(t *testing.T) {
	w := newRunWorld(t)
	w.newVM()
	w.put("ssh-fail", "bash -s")
	w.start()
	w.waitFor("installer", func() bool { return w.pid("krunkit.pid") > 0 })
	pids := w.pids()
	err := w.wait(pids)
	if err == nil || !strings.Contains(err.Error(), "install failed; the installer's console is in") {
		t.Errorf("run: %v", err)
	}
	if w.exists("installed") {
		t.Error("the VM is installed")
	}
	if got := w.lines("ssh.log", "poweroff"); len(got) != 0 {
		t.Errorf("the commands in the installer: %q", got)
	}

	// Changes of the configuration directory that are not committed stop the installation
	// before it starts a program.
	if err := os.WriteFile(w.cfg.root+"/sandbox-vm/new.nix", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(w.cfg); err == nil || !strings.Contains(err.Error(), "sandbox-vm/ has uncommitted changes") {
		t.Errorf("run with changes: %v", err)
	}
	if got := w.pids(); len(got) != 0 {
		t.Errorf("programs run: %v", got)
	}
}
