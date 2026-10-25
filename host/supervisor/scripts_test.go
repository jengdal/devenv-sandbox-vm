package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The tests of the shell scripts in host/scripts. They start a script with fakes in
// the place of ssh, nc and borg. The fakes record their arguments, and files
// in the directory $STUB tell them what to do.

// The directory of the shell scripts. The build of the supervisor does not have it.
const scriptsDir = "../scripts"

// ssh: the control connection for the forwards (ports.sh) and the commands in the guest.
//
//	connected        the control connection answers if this file exists
//	refuse           the forwards that cannot be opened, one in each line
//	provisioned      the file `provisioned` of the guest
//	archive.tar      the stream that the guest unpacks
//	system           the system profile of the guest (readlink)
//	rebuild-status   the status of nixos-rebuild
//	rebuild-system   the profile after nixos-rebuild; without this file, the profile does
//	                 not change
//	rollback-status  the status of the switch back to the previous system
//	stream, stream-status
//	                 the output and the status of the backup command
//	home-status      the status of the build and activation of the home-manager configuration
const stubSSH = `printf '%s\n' "$*" >>"$STUB/ssh.log"
case " $* " in
  *" -O check "*) [ -e "$STUB/connected" ] ;;
  *" -O forward "*)
    for a in "$@"; do
      if grep -qxF -- "$a" "$STUB/refuse" 2>/dev/null; then exit 255; fi
    done ;;
  *" -O cancel "*) ;;
  *" cat "*provisioned*) cat "$STUB/provisioned" 2>/dev/null ;;
  *"tar -x"*) cat >"$STUB/archive.tar" ;;
  *readlink*) cat "$STUB/system" 2>/dev/null ;;
  *nixos-rebuild*)
    echo "building the system"
    if [ -e "$STUB/rebuild-system" ]; then cp "$STUB/rebuild-system" "$STUB/system"; fi
    exit "$(cat "$STUB/rebuild-status" 2>/dev/null || echo 0)" ;;
  *switch-to-configuration*) exit "$(cat "$STUB/rollback-status" 2>/dev/null || echo 0)" ;;
  *"tee "*provisioned*) cat >"$STUB/provisioned" ;;
  *"nix build"*) echo "activating"; exit "$(cat "$STUB/home-status" 2>/dev/null || echo 0)" ;;
  *backup-stream*) cat "$STUB/stream"; exit "$(cat "$STUB/stream-status" 2>/dev/null || echo 0)" ;;
  *) echo "ssh of the test: unknown command: $*" >&2; exit 2 ;;
esac
`

// nc: a port of the host is in use if the file `busy` has it.
const stubNC = `for port; do :; done
grep -qx -- "$port" "$STUB/busy" 2>/dev/null
`

// A fake supervisor, as backup.sh starts it: `confine OPTION... --
// BORG ARGUMENT...`. It records the options and the arguments of borg, and it reads the
// stream of import-tar.
const stubSupervisor = `options=
while [ "$1" != -- ]; do options="$options $1"; shift; done
shift 2
printf '%s\n' "$options" >>"$STUB/confine.log"
printf '%s\n' "$*" >>"$STUB/borg.log"
case "$1" in
  import-tar) wc -c >"$STUB/imported" ;;
esac
grep -qxF -- "$1" "$STUB/borg-fails" 2>/dev/null && exit 2
exit 0
`

// scriptWorld is the home directory, the programs and the variables for one script.
type scriptWorld struct {
	t                *testing.T
	home, stub, dir  string
	bash, scriptsDir string
	env              map[string]string
}

func newScriptWorld(t *testing.T) *scriptWorld {
	t.Helper()
	scripts, err := filepath.Abs(scriptsDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(scripts, "lib.sh")); err != nil {
		t.Skip(err)
	}
	bash := tool(t, "bash")
	top, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &scriptWorld{
		t: t, home: top + "/home", stub: top + "/stub", dir: top + "/home/.devenv-sandbox-vm/vm1",
		bash: bash, scriptsDir: scripts,
	}
	for _, d := range []string{w.dir, w.stub + "/bin"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range map[string]string{"bin/ssh": stubSSH, "bin/nc": stubNC, "supervisor": stubSupervisor, "filter": "exec cat\n"} {
		if err := os.WriteFile(filepath.Join(w.stub, name), []byte("#!"+tool(t, "sh")+"\n"+text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.env = map[string]string{
		"HOME":               w.home,
		"STUB":               w.stub,
		"PATH":               w.stub + "/bin" + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SVM_NAME":           "vm1",
		"SVM_GUEST_MARKER":   top + "/guest-marker",
		"SVM_GUEST_STATE":    "/var/lib/devenv-sandbox-vm",
		"SVM_PROXY_GATEWAY":  "192.168.127.254",
		"SVM_SUPERVISOR":     w.stub + "/supervisor",
		"SVM_CONSOLE_FILTER": w.stub + "/filter",
	}
	return w
}

// run starts a script and returns its status and its messages.
func (w *scriptWorld) run(script string, args ...string) (int, string) {
	w.t.Helper()
	cmd := exec.Command(w.bash, append([]string{filepath.Join(w.scriptsDir, script)}, args...)...)
	for name, value := range w.env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	cmd.Dir = w.home
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return exit.ExitCode(), string(out)
	case err != nil:
		w.t.Fatalf("%s: %v", script, err)
	}
	return 0, string(out)
}

// put writes a file of the stub directory, or of a different directory with a full path.
func (w *scriptWorld) put(name, text string) {
	w.t.Helper()
	if !filepath.IsAbs(name) {
		name = filepath.Join(w.stub, name)
	}
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

// text returns the text of a file, or nothing if the file does not exist.
func (w *scriptWorld) text(name string) string {
	if !filepath.IsAbs(name) {
		name = filepath.Join(w.stub, name)
	}
	text, _ := os.ReadFile(name)
	return string(text)
}

// calls returns the lines of a log of the stub directory that contain the text, and removes
// the log.
func (w *scriptWorld) calls(log, part string) []string {
	var lines []string
	for _, line := range strings.Split(w.text(log), "\n") {
		if line != "" && strings.Contains(line, part) {
			lines = append(lines, line)
		}
	}
	os.Remove(filepath.Join(w.stub, log))
	return lines
}

// The forwards as host/scripts.nix creates them.
const (
	forwardWeb = "L web 127.0.0.1:8080:127.0.0.1:80 8080 127.0.0.1:8080 -> port 80 in the VM"
	forwardAPI = "L api 127.0.0.1:9090:127.0.0.1:90 9090 127.0.0.1:9090 -> port 90 in the VM"
	forwardDB  = "R db 127.0.0.1:5432:db.example:5432 - port 5432 in the VM -> db.example:5432"
)

func TestPortsScript(t *testing.T) {
	w := newScriptWorld(t)
	forwards := func() string { return w.text(w.dir + "/forwards") }
	expect := func(what string, status int, out string, wantStatus int, messages ...string) {
		t.Helper()
		if status != wantStatus {
			t.Errorf("%s: status %d, want %d\n%s", what, status, wantStatus, out)
		}
		for _, m := range messages {
			if !strings.Contains(out, m) {
				t.Errorf("%s: no message %q in:\n%s", what, m, out)
			}
		}
	}

	// Without the control connection, the VM does not run. For the task that is not a
	// failure. For the sandbox-vm process it is.
	w.env["SVM_FORWARDS"] = forwardWeb + "\n" + forwardDB + "\n"
	status, out := w.run("ports.sh")
	expect("no connection", status, out, 0)
	if len(w.calls("ssh.log", "-O forward")) != 0 || forwards() != "" {
		t.Errorf("no connection: the script opened forwards")
	}
	status, out = w.run("ports.sh", "--connected")
	expect("no connection, --connected", status, out, 1, "the connection for the forwards does not answer")

	// The first run opens all forwards.
	w.put("connected", "")
	status, out = w.run("ports.sh", "--connected")
	expect("first run", status, out, 0, "web: 127.0.0.1:8080 -> port 80 in the VM", "db: port 5432 in the VM -> db.example:5432")
	if got := w.calls("ssh.log", "-O forward"); len(got) != 2 ||
		!strings.Contains(got[0], "-O forward -L 127.0.0.1:8080:127.0.0.1:80 vm1") ||
		!strings.Contains(got[1], "-O forward -R 127.0.0.1:5432:db.example:5432 vm1") {
		t.Errorf("first run: the forwards %q", got)
	}
	if forwards() != forwardWeb+"\n"+forwardDB+"\n" {
		t.Errorf("first run: the file forwards has %q", forwards())
	}

	// A run with the same options changes nothing.
	status, out = w.run("ports.sh")
	expect("same options", status, out, 0)
	if got := w.calls("ssh.log", "-O "); len(got) != 1 || !strings.Contains(got[0], "-O check") {
		t.Errorf("same options: the commands %q", got)
	}

	// New options: one forward closes, one opens, one stays.
	w.env["SVM_FORWARDS"] = forwardAPI + "\n" + forwardDB + "\n"
	status, out = w.run("ports.sh")
	expect("new options", status, out, 0, "web: closed", "api: 127.0.0.1:9090")
	if got := w.calls("ssh.log", "-O cancel"); len(got) != 1 || !strings.Contains(got[0], "-O cancel -L 127.0.0.1:8080:127.0.0.1:80 vm1") {
		t.Errorf("new options: the closed forwards %q", got)
	}
	if forwards() != forwardDB+"\n"+forwardAPI+"\n" {
		t.Errorf("new options: the file forwards has %q", forwards())
	}

	// --again applies the forwards of the last run, not those of the variable.
	w.env["SVM_FORWARDS"] = forwardWeb + "\n"
	os.Remove(w.dir + "/forwards")
	status, out = w.run("ports.sh", "--again")
	expect("--again", status, out, 0, "api:", "db:")
	if forwards() != forwardAPI+"\n"+forwardDB+"\n" {
		t.Errorf("--again: the file forwards has %q", forwards())
	}
	w.calls("ssh.log", "")

	// A port of the host that is in use, and forwards that cannot be opened: each one gets
	// a message, and the script does not fail.
	w.env["SVM_FORWARDS"] = forwardWeb + "\n" + forwardAPI + "\n" + forwardDB + "\n"
	os.Remove(w.dir + "/forwards")
	w.put("busy", "8080\n")
	w.put("refuse", "127.0.0.1:9090:127.0.0.1:90\n127.0.0.1:5432:db.example:5432\n")
	status, out = w.run("ports.sh")
	expect("ports in use", status, out, 0,
		"web: 127.0.0.1:8080 is in use on this Mac; not forwarded",
		"api: could not be opened on this Mac",
		"db: could not be opened, is the port in use in the VM?")
	if got := w.calls("ssh.log", "-O forward"); len(got) != 2 {
		t.Errorf("ports in use: the forwards %q", got)
	}
	if forwards() != "" {
		t.Errorf("ports in use: the file forwards has %q", forwards())
	}
	// The port is free again: the subsequent run opens the forward.
	w.put("busy", "")
	status, out = w.run("ports.sh")
	expect("port is free again", status, out, 0, "web: 127.0.0.1:8080 -> port 80 in the VM")
	if forwards() != forwardWeb+"\n" {
		t.Errorf("port is free again: the file forwards has %q", forwards())
	}
	w.calls("ssh.log", "")

	// The script cannot write its files.
	os.Remove(w.dir + "/wanted")
	if err := os.Mkdir(w.dir+"/wanted", 0o755); err != nil {
		t.Fatal(err)
	}
	status, out = w.run("ports.sh")
	if status == 0 || !strings.Contains(out, "the forwards were not applied correctly") {
		t.Errorf("no write: status %d\n%s", status, out)
	}
	os.Remove(w.dir + "/wanted")
	w.calls("ssh.log", "")

	// The clone in the guest has the same task. It does nothing there.
	w.put(w.env["SVM_GUEST_MARKER"], "")
	status, out = w.run("ports.sh", "--connected")
	expect("guest", status, out, 0)
	if got := w.calls("ssh.log", ""); len(got) != 0 {
		t.Errorf("guest: the commands %q", got)
	}
}

func TestBackupRepo(t *testing.T) {
	w := newScriptWorld(t)
	for _, tt := range []struct{ option, want string }{
		{"", w.home + "/.devenv-sandbox-vm-backups/vm1"},
		{"~/backups/vm", w.home + "/backups/vm"},
		{"/Volumes/disk/vm", "/Volumes/disk/vm"},
		// A relative path is refused.
		{"backups", ""},
		{"~user/backups", ""},
	} {
		cmd := exec.Command(w.bash, "-c", `. "$1/lib.sh"; backup_repo`, "sh", w.scriptsDir)
		cmd.Env = []string{"HOME=" + w.home, "SVM_NAME=vm1", "SVM_BACKUP_REPO=" + tt.option}
		out, err := cmd.Output()
		if got := strings.TrimSuffix(string(out), "\n"); got != tt.want || (err == nil) != (tt.want != "") {
			t.Errorf("%q: %q %v, want %q", tt.option, got, err, tt.want)
		}
	}
}

func TestBackupScript(t *testing.T) {
	w := newScriptWorld(t)
	repo := w.home + "/backups"
	for name, value := range map[string]string{
		"SVM_BORG": "/nix/store/borg/bin/borg", "SVM_BORG_STORE_PATHS": "/nix/store/borg-paths",
		"SVM_BACKUP_REPO": repo, "SVM_BACKUP_COMMAND": "backup-stream", "SVM_BACKUP_MAX_BYTES": "10",
		"SVM_BACKUP_COMPRESSION": "zstd", "SVM_BACKUP_KEEP": "--keep-daily 7 --keep-weekly 4",
	} {
		w.env[name] = value
	}
	stamp := func() bool { _, err := os.Stat(w.dir + "/backup.stamp"); return err == nil }
	// The commands of borg, without the name of the archive, which has the time.
	borg := func() string {
		var commands []string
		for _, line := range w.calls("borg.log", "") {
			commands = append(commands, strings.Fields(line)[0])
		}
		return strings.Join(commands, " ")
	}

	w.put("stream", "0123456789")
	status, out := w.run("backup.sh")
	if status != 1 || !strings.Contains(out, "the VM is not ready") || borg() != "" {
		t.Errorf("not ready: status %d\n%s", status, out)
	}

	// A stream that has the maximum size.
	w.put(w.dir+"/ready", "")
	status, out = w.run("backup.sh")
	if status != 0 || !strings.Contains(out, ": done ("+repo+")") || !stamp() {
		t.Errorf("backup: status %d, stamp %v\n%s", status, stamp(), out)
	}
	if got := strings.TrimSpace(w.text("imported")); got != "10" {
		t.Errorf("backup: borg got %s bytes", got)
	}
	// borg has no network, and it can write only the repository and its own directory.
	for _, options := range w.calls("confine.log", "") {
		want := " confine --egress none --store-paths /nix/store/borg-paths --exec-closure --write-dir " + repo + " --write-dir " + w.dir + "/borg"
		if !strings.HasPrefix(options, want) {
			t.Errorf("backup: the sandbox of borg has the options %q", options)
		}
	}
	lines := w.text("borg.log")
	if !strings.Contains(lines, "prune --glob-archives *[0-9] --keep-daily 7 --keep-weekly 4 "+repo) ||
		!strings.Contains(lines, "import-tar --compression zstd "+repo+"::") {
		t.Errorf("backup: the commands of borg:\n%s", lines)
	}
	if got := borg(); got != "init delete import-tar rename prune compact" {
		t.Errorf("backup: the commands of borg: %s", got)
	}
	if st, err := os.Stat(repo); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("backup: the repository: %v %v", st, err)
	}

	// The guest controls the stream. A stream that is too large, a command that fails and a
	// borg that fails make no backup, and the archive that is not complete is deleted.
	for _, tt := range []struct {
		what, file, text, message string
	}{
		{"too large", "stream", "0123456789a", "the backup is larger than services.sandbox-vm.backup.maxSize"},
		{"command fails", "stream-status", "255", ": failed"},
		{"borg fails", "borg-fails", "import-tar\n", ": failed"},
	} {
		os.Remove(w.dir + "/backup.stamp")
		w.put(repo+"/config", "")
		w.put("stream", "0123456789")
		w.put(tt.file, tt.text)
		status, out = w.run("backup.sh")
		if status != 1 || !strings.Contains(out, tt.message) || stamp() {
			t.Errorf("%s: status %d, stamp %v\n%s", tt.what, status, stamp(), out)
		}
		if got := borg(); got != "delete import-tar delete" {
			t.Errorf("%s: the commands of borg: %s", tt.what, got)
		}
		os.Remove(filepath.Join(w.stub, tt.file))
	}

	// No rule keeps backups: the script deletes no backup.
	w.env["SVM_BACKUP_KEEP"] = ""
	w.put("stream", "")
	if status, out = w.run("backup.sh"); status != 0 {
		t.Errorf("no rules: status %d\n%s", status, out)
	}
	if got := borg(); got != "delete import-tar rename compact" {
		t.Errorf("no rules: the commands of borg: %s", got)
	}
}

func TestUpdateScript(t *testing.T) {
	tool(t, "git")
	// The project can be the top of its git repo, or a directory below the top.
	for _, project := range []string{".", "apps/api"} {
		t.Run(project, func(t *testing.T) {
			w := newScriptWorld(t)
			top := w.home + "/repo"
			root := filepath.Join(top, project)
			git := func(args ...string) string {
				t.Helper()
				args = append([]string{"-C", top, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)
				out, err := exec.Command("git", args...).CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			if err := os.MkdirAll(root+"/sandbox-vm", 0o755); err != nil {
				t.Fatal(err)
			}
			w.put(root+"/sandbox-vm/flake.nix", "{ }\n")
			w.put(top+"/other", "")
			git("init", "-q")
			git("add", ".")
			git("commit", "-q", "-m", "files")
			tree := git("rev-parse", "HEAD:"+filepath.Join(project, "sandbox-vm"))
			for name, value := range map[string]string{
				"SVM_ROOT": root, "SVM_CONFIG_DIR": "sandbox-vm", "SVM_CONFIG_NAME": "devenv-sandbox-vm",
			} {
				w.env[name] = value
			}
			expect := func(what string, wantStatus int, message, wantProvisioned string) {
				t.Helper()
				status, out := w.run("update.sh")
				if status != wantStatus || !strings.Contains(out, message) {
					t.Errorf("%s: status %d, want %d and %q\n%s", what, status, wantStatus, message, out)
				}
				if got := w.text("provisioned"); got != wantProvisioned {
					t.Errorf("%s: the guest records %q, want %q", what, got, wantProvisioned)
				}
			}
			rebuilds := func() []string { return w.calls("ssh.log", "nixos-rebuild") }

			expect("not ready", 1, "the VM is not ready", "")
			w.put(w.dir+"/ready", "")

			// The file `env` has the port of the proxy of this run.
			w.put(w.dir+"/env", "hostname=\"vm\"\n")
			expect("no port", 1, "does not have the port of the proxy", "")
			w.put(w.dir+"/env", "hostname=\"vm\"\nproxy_port=\"23007\"\n")

			// The system of the guest before the update. The script accepts only a store path.
			previous := "/nix/store/" + strings.Repeat("a", 32) + "-nixos-system-vm"
			next := "/nix/store/" + strings.Repeat("b", 32) + "-nixos-system-vm"
			w.put("provisioned", "an older tree\n")
			w.put("system", "not a store path\n")
			expect("bad system", 1, "cannot read the current system of the VM", "an older tree\n")
			if got := rebuilds(); len(got) != 0 {
				t.Errorf("bad system: the commands in the guest %q", got)
			}
			w.put("system", previous+"\n")

			// The guest builds the system from the committed files.
			w.put("rebuild-status", "1")
			expect("build fails", 1, "system update failed; the VM is still on its previous system", "an older tree\n")
			w.put("rebuild-status", "0")
			expect("update", 0, "the system of the VM is updated", tree+"\n")
			got := rebuilds()
			proxy := "http_proxy=http://192.168.127.254:23007 https_proxy=http://192.168.127.254:23007"
			flake := `--flake "path:/var/lib/devenv-sandbox-vm/src/sandbox-vm#devenv-sandbox-vm-$(uname -m)"`
			if len(got) != 2 || !strings.Contains(got[1], proxy) || !strings.Contains(got[1], flake) {
				t.Errorf("update: the commands in the guest %q", got)
			}
			var names []string
			for r := tar.NewReader(bytes.NewReader([]byte(w.text("archive.tar")))); ; {
				h, err := r.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if h.Typeflag == tar.TypeReg {
					names = append(names, h.Name)
				}
			}
			if strings.Join(names, " ") != "sandbox-vm/flake.nix" {
				t.Errorf("update: the guest got the files %q", names)
			}

			expect("up to date", 0, "the system of the VM is up to date", tree+"\n")
			if got := rebuilds(); len(got) != 0 {
				t.Errorf("up to date: the commands in the guest %q", got)
			}
			if got := w.calls("ssh.log", "nix build"); len(got) != 0 {
				t.Errorf("no home-manager configuration: the commands in the guest %q", got)
			}

			// The home-manager configuration comes from a different flake. Thus the task
			// applies it also when the system is up to date.
			w.env["SVM_HOME_MANAGER"] = "git+https://example.com/dotfiles.git#me"
			expect("home, up to date", 0, "the home-manager configuration is applied", tree+"\n")
			w.put("provisioned", "an older tree\n")
			expect("home, update", 0, "the home-manager configuration is applied", tree+"\n")
			if got := w.calls("ssh.log", "nix build"); len(got) != 2 {
				t.Errorf("home: the commands in the guest %q", got)
			}
			w.put("home-status", "1")
			expect("home fails", 1, "the home-manager configuration was not applied", tree+"\n")
			os.Remove(filepath.Join(w.stub, "home-status"))
			delete(w.env, "SVM_HOME_MANAGER")

			// A failure after the guest changed its profile: the script goes back to the
			// exact previous system. Status 4: the new system is active, but a unit of it did
			// not start. That is also a failure.
			rollbacks := func() []string { return w.calls("ssh.log", "switch-to-configuration") }
			w.put("rebuild-system", next+"\n")
			for _, status := range []string{"4", "1"} {
				w.put("system", previous+"\n")
				w.put("provisioned", "an older tree\n")
				w.put("rebuild-status", status)
				expect("activation fails "+status, 1, "the VM is back on its previous system", "an older tree\n")
				want := "nix-env -p /nix/var/nix/profiles/system --set " + previous + " && sudo " + previous + "/bin/switch-to-configuration switch"
				if got := rollbacks(); len(got) != 1 || !strings.Contains(got[0], want) {
					t.Errorf("activation fails %s: the commands in the guest %q", status, got)
				}
			}
			w.put("rebuild-status", "0")

			// The switch back also fails.
			w.put("system", previous+"\n")
			w.put("provisioned", "an older tree\n")
			w.put("rebuild-status", "1")
			w.put("rollback-status", "1")
			expect("rollback fails", 1, "could not go back to its previous system", "an older tree\n")
			w.put("rebuild-status", "0")
			w.put("rollback-status", "0")
			os.Remove(filepath.Join(w.stub, "rebuild-system"))
			w.put("system", previous+"\n")

			// A change that is not committed stops the task. A change to a different
			// directory does not.
			w.put("provisioned", "an older tree\n")
			w.put(top+"/other", "change\n")
			expect("other change", 0, "the system of the VM is updated", tree+"\n")
			w.put("provisioned", "an older tree\n")
			w.put(root+"/sandbox-vm/new.nix", "")
			expect("change", 1, "sandbox-vm/ has uncommitted changes", "an older tree\n")
		})
	}
}

func TestHomeScript(t *testing.T) {
	w := newScriptWorld(t)
	pending := w.dir + "/home-pending"
	expect := func(what string, wantStatus int, message string, wantPending bool) {
		t.Helper()
		status, out := w.run("home.sh")
		if status != wantStatus || !strings.Contains(out, message) {
			t.Errorf("%s: status %d, want %d and %q\n%s", what, status, wantStatus, message, out)
		}
		if _, err := os.Stat(pending); (err == nil) != wantPending {
			t.Errorf("%s: the file home-pending is there: %v, want %v", what, err == nil, wantPending)
		}
	}
	builds := func() []string { return w.calls("ssh.log", "nix build") }

	// Without the option, the script does nothing.
	w.put(pending, "")
	expect("no option", 0, "homeManager is not set", false)
	if got := w.calls("ssh.log", ""); len(got) != 0 {
		t.Errorf("no option: the commands in the guest %q", got)
	}

	w.put(pending, "")
	w.env["SVM_HOME_MANAGER"] = "git+https://example.com/dotfiles.git#me@vm-aarch64-linux"
	expect("not ready", 1, "the VM is not ready", true)
	w.put(w.dir+"/ready", "")
	expect("no port", 1, "does not have the port of the proxy", true)
	w.put(w.dir+"/env", "hostname=\"vm\"\nproxy_port=\"23007\"\n")

	for _, bad := range []string{"git+https://example.com/dotfiles.git", "#me", "x#a b", "x#a\"b"} {
		w.env["SVM_HOME_MANAGER"] = bad
		expect("bad option "+bad, 1, "must be <flake>#<configuration>", true)
	}
	if got := builds(); len(got) != 0 {
		t.Errorf("bad option: the commands in the guest %q", got)
	}

	// The guest builds the activation package with the proxy and runs it. The flake
	// reference is one quoted word.
	w.env["SVM_HOME_MANAGER"] = "git+https://example.com/dot'files.git?ref=main#me@vm-aarch64-linux"
	w.put("home-status", "1")
	expect("fails", 1, "the home-manager configuration was not applied", true)
	w.put("home-status", "0")
	expect("applies", 0, "the home-manager configuration is applied", false)
	got := builds()
	proxy := "http_proxy=http://192.168.127.254:23007 https_proxy=http://192.168.127.254:23007"
	attr := `'git+https://example.com/dot'\''files.git?ref=main#homeConfigurations."me@vm-aarch64-linux".activationPackage'`
	if len(got) != 2 || !strings.Contains(got[1], proxy) || !strings.Contains(got[1], attr) || !strings.Contains(got[1], `nix build --refresh `) || !strings.Contains(got[1], `"$out/activate"`) {
		t.Errorf("applies: the commands in the guest %q", got)
	}
}

// The scripts that need a host stop in the guest, and on a host that is not supported.
func TestRequireHost(t *testing.T) {
	w := newScriptWorld(t)
	w.env["SVM_BACKUP_REPO"] = ""
	for _, script := range []string{"host-only.sh", "backup.sh", "backup-loop.sh", "update.sh"} {
		w.env["SVM_SUPERVISOR"] = ""
		if status, out := w.run(script); status != 1 || !strings.Contains(out, "only Apple silicon Macs are supported as hosts") {
			t.Errorf("%s, no host: status %d\n%s", script, status, out)
		}
		w.env["SVM_SUPERVISOR"] = w.stub + "/supervisor"
		w.put(w.env["SVM_GUEST_MARKER"], "")
		if status, out := w.run(script); status != 1 || !strings.Contains(out, "this is the VM itself") {
			t.Errorf("%s, guest: status %d\n%s", script, status, out)
		}
		os.Remove(w.env["SVM_GUEST_MARKER"])
	}
	// The probe is for the file `ready`.
	if status, _ := w.run("ready.sh"); status == 0 {
		t.Error("ready.sh: status 0 without the file")
	}
	w.put(w.dir+"/ready", "")
	if status, out := w.run("ready.sh"); status != 0 {
		t.Errorf("ready.sh: status %d\n%s", status, out)
	}
}
