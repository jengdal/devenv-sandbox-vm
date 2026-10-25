package main

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The project can be the top of its git repo, or a directory below the top. The archive has
// the committed files of the configuration directory in the two cases.
func TestCommittedConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip(err)
	}
	for _, project := range []string{".", "apps/api"} {
		top := t.TempDir()
		root := filepath.Join(top, project)
		if err := os.MkdirAll(filepath.Join(root, "sandbox-vm"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "sandbox-vm", "flake.nix"), []byte("{ }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"init", "-q"},
			{"add", "."},
			{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "files"},
		} {
			if out, err := exec.Command("git", append([]string{"-C", top}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v: %s", args, err, out)
			}
		}
		s := &supervisor{cfg: &config{root: root, configDir: "sandbox-vm"}}
		if err := s.committedConfig(); err != nil {
			t.Fatalf("%s: %v", project, err)
		}
		archive, err := s.archiveConfig(context.Background(), "src/").Output()
		if err != nil {
			t.Fatalf("%s: %v", project, err)
		}
		var names []string
		for r := tar.NewReader(bytes.NewReader(archive)); ; {
			h, err := r.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s: %v", project, err)
			}
			if h.Typeflag == tar.TypeReg {
				names = append(names, h.Name)
			}
		}
		if strings.Join(names, " ") != "src/sandbox-vm/flake.nix" {
			t.Errorf("%s: the archive has the files %q", project, names)
		}

		// A file that is not committed stops the installation.
		if err := os.WriteFile(filepath.Join(root, "sandbox-vm", "new.nix"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := s.committedConfig(); err == nil {
			t.Errorf("%s: a file that is not committed gave no error", project)
		}
	}
}

// A VM is the VM of one project: a second project with the same VM name must not boot it.
func TestCheckProject(t *testing.T) {
	c := &config{name: "devenv-api", root: "/work/a/api", dir: t.TempDir()}
	// A new VM, or a VM of an older version: it becomes the VM of this project.
	if err := checkProject(c); err != nil {
		t.Fatal(err)
	}
	if text, _ := os.ReadFile(c.path("root")); string(text) != "/work/a/api\n" {
		t.Errorf("root: %q", text)
	}
	if err := checkProject(c); err != nil {
		t.Errorf("the project of the VM: %v", err)
	}
	other := &config{name: c.name, root: "/work/b/api", dir: c.dir}
	err := checkProject(other)
	if err == nil || !strings.Contains(err.Error(), "/work/a/api") || !strings.Contains(err.Error(), "/work/b/api") {
		t.Errorf("a different project: %v", err)
	}
	if text, _ := os.ReadFile(c.path("root")); string(text) != "/work/a/api\n" {
		t.Errorf("root after a different project: %q", text)
	}
}

func TestLoadConfig(t *testing.T) {
	env := map[string]string{"HOME": "/home/u"}
	for _, name := range []string{
		"SVM_NAME", "SVM_HOSTNAME", "SVM_ROOT", "SVM_CONFIG_DIR", "SVM_CONFIG_NAME", "SVM_CPUS",
		"SVM_MEMORY_MIB", "SVM_LOG_BYTES", "SVM_GUEST_MARKER", "SVM_GUEST_STATE", "SVM_GUEST_MAC",
		"SVM_PROXY_GATEWAY", "SVM_TIMESYNC_PORT", "SVM_GUEST_ARCH",
		"SVM_SMOKESCREEN", "SVM_SMOKESCREEN_CONFIG", "SVM_SMOKESCREEN_STORE_PATHS", "SVM_GVPROXY",
		"SVM_GVPROXY_STORE_PATHS", "SVM_S6_LOG", "SVM_S6_LOG_STORE_PATHS", "SVM_CONSOLE_FILTER",
		"SVM_CONSOLE_FILTER_STORE_PATHS", "SVM_KRUNKIT", "SVM_KRUNKIT_STORE_PATHS", "SVM_PORTS",
	} {
		env[name] = "x"
	}
	env["SVM_NAME"] = "vm1"
	env["SVM_DISK_GB"] = "20"
	getenv := func(name string) string { return env[name] }

	// SVM_USER, SVM_NIX_OPTIONS, SVM_INSTALLER_ISO and the binary caches can be empty.
	c, err := loadConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if c.installerISO != "" || c.diskGB != 20 || c.dir != "/home/u/.devenv-sandbox-vm/vm1" || c.path("log", "a") != c.dir+"/log/a" {
		t.Errorf("config: %+v", c)
	}

	// The installer ISO is an absolute path, or a path in the home directory.
	env["SVM_INSTALLER_ISO"] = "~/Downloads/nixos.iso"
	if c, err := loadConfig(getenv); err != nil || c.installerISO != "/home/u/Downloads/nixos.iso" {
		t.Errorf("installer ISO: %+v %v", c, err)
	}
	env["SVM_INSTALLER_ISO"] = "nixos.iso"
	if _, err := loadConfig(getenv); err == nil {
		t.Error("an installer ISO with a relative path gave no error")
	}
	delete(env, "SVM_INSTALLER_ISO")

	env["SVM_DISK_GB"] = "big"
	if _, err := loadConfig(getenv); err == nil {
		t.Error("a disk size that is not a number gave no error")
	}
	delete(env, "SVM_DISK_GB")
	delete(env, "SVM_GVPROXY")
	_, err = loadConfig(getenv)
	if err == nil || !strings.Contains(err.Error(), "SVM_GVPROXY SVM_DISK_GB") {
		t.Errorf("missing variables: %v", err)
	}
}

// prepare checks the name of the VM and the hostname with the rules of
// host/options.nix. The two files must have the same rules: with a rule that is only in
// this program, the options accept a value that stops the sandbox-vm process.
func TestOptionRules(t *testing.T) {
	// The build of the supervisor does not have the file.
	text, err := os.ReadFile("../options.nix")
	if err != nil {
		t.Skip(err)
	}
	for option, rule := range map[string]*regexp.Regexp{"name": vmName, "hostName": hostName} {
		m := regexp.MustCompile(`(?m)^    ` + option + ` = mkOption \{\n +type = types\.strMatching "(.*)";$`).FindSubmatch(text)
		if m == nil {
			t.Errorf("options.nix has no rule for %s", option)
			continue
		}
		if want := "^" + string(m[1]) + "$"; rule.String() != want {
			t.Errorf("%s: this program has the rule %s, options.nix has %s", option, rule, want)
		}
	}
}
