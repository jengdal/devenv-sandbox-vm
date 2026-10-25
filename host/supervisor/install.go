package main

// The installation of a new VM. There are two procedures:
//
//   - Without an installer ISO, the host builds a disk image of the system on a Linux
//     builder (system.build.sandboxVmImage of the guest module). The disk of the VM is a
//     copy of the image. The first boot gets the files of the host as systemd credentials.
//     Nix keeps the software and the image in the store of the host. Thus Nix downloads
//     only the changed software for a subsequent new VM.
//   - With an installer ISO (services.sandbox-vm.installer.iso), the VM boots the ISO one
//     time. The host sends the configuration directory and guest-install.sh to the
//     installer, and the installer installs NixOS on the disk. The host uses no builder.
//
// The host gives files to a boot as systemd credentials in OEM strings of the SMBIOS
// table. The ISO needs no change for this: systemd reads the strings.

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// The script that installs NixOS. It runs as root in the installer.
//
//go:embed guest-install.sh
var guestInstall []byte

// The directory in the installer with the files from the host.
const installStage = "/tmp/devenv-sandbox-vm"

// credential returns the OEM string that gives one systemd credential to the guest.
//
// The strings are arguments of the hypervisor, thus other programs of this computer can
// read them. They contain no secret of the installed system.
func credential(name string, value []byte) string {
	return "io.systemd.credential.binary:" + name + "=" + base64.StdEncoding.EncodeToString(value)
}

// installerCredentials returns the credentials for the installer of the ISO: the key that
// root accepts, and the SSH host key of the installer. Thus the host knows the host key
// before the boot. systemd-tmpfiles writes the host key from its line.
func installerCredentials(authorizedKey, hostKey []byte) []string {
	line := "f~ /etc/ssh/ssh_host_ed25519_key 0600 root root - " + base64.StdEncoding.EncodeToString(hostKey) + "\n"
	return []string{
		credential("ssh.authorized_keys.root", authorizedKey),
		credential("tmpfiles.extra", []byte(line)),
	}
}

// firstBootCredentials returns the credentials for the first boot of a VM that is a copy of
// the disk image. The service devenv-sandbox-vm-provision of the guest stores them.
func (s *supervisor) firstBootCredentials() ([]string, error) {
	c := s.cfg
	env, err := os.ReadFile(c.path("env"))
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(c.path("id_ed25519.pub"))
	if err != nil {
		return nil, err
	}
	return []string{
		credential("devenv-sandbox-vm.env", env),
		credential("devenv-sandbox-vm.authorized_keys", key),
		credential("devenv-sandbox-vm.provisioned", []byte(s.configTree+"\n")),
	}, nil
}

// builderSystems returns the systems that the builders of Nix build for. builders is the
// value of the `builders` setting: a list of builders, or @ and the path of a file with
// that list. One builder is a line, or a part between semicolons. Its second field is the
// list of its systems.
func builderSystems(builders string, readFile func(string) ([]byte, error)) []string {
	if file, ok := strings.CutPrefix(strings.TrimSpace(builders), "@"); ok {
		text, err := readFile(file)
		if err != nil {
			return nil
		}
		builders = string(text)
	}
	var systems []string
	for _, line := range strings.FieldsFunc(builders, func(r rune) bool { return r == '\n' || r == ';' }) {
		line, _, _ = strings.Cut(line, "#")
		if f := strings.Fields(line); len(f) >= 2 {
			systems = append(systems, strings.Split(f[1], ",")...)
		}
	}
	return systems
}

// checkLinuxBuilder fails if Nix on this computer cannot build for the system of the
// guest. Without this check, the build of the disk image fails late, with an error that
// gives no help.
func checkLinuxBuilder(ctx context.Context, system string) error {
	setting := func(name string) string {
		show := helper(ctx, "nix", "--extra-experimental-features", "nix-command", "config", "show", name)
		out, _ := show.Output()
		return strings.TrimSpace(string(out))
	}
	systems := append(strings.Fields(setting("system")), strings.Fields(setting("extra-platforms"))...)
	systems = append(systems, builderSystems(setting("builders"), os.ReadFile)...)
	for _, s := range systems {
		if s == system {
			return nil
		}
	}
	return fmt.Errorf("Nix on this computer has no builder for %s", system)
}

const builderHelp = `a new VM is created from a disk image that Nix builds on a Linux builder. Do one of these:
  - Set up a Linux builder for Nix. With nix-darwin: nix.linux-builder.enable = true.
  - Download a NixOS installer ISO and set services.sandbox-vm.installer.iso to its path
    (in devenv.local.nix). The VM then installs itself and the host needs no builder.`

func (s *supervisor) installNewVM(ctx context.Context) error {
	c := s.cfg
	if err := s.committedConfig(); err != nil {
		return err
	}
	installDir := c.path("install")
	for _, f := range []string{installDir, c.path("disk.img"), c.path("known_hosts"), c.path("first-boot"), c.path("home-pending")} {
		os.RemoveAll(f)
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}
	var err error
	if c.installerISO == "" {
		err = s.installFromImage(ctx)
	} else {
		err = s.installFromISO(ctx)
	}
	if err != nil {
		return err
	}
	os.RemoveAll(installDir)
	// The new VM does not have the home-manager configuration yet. connectGuest applies it.
	if c.homeProgram != "" {
		if err := os.WriteFile(c.path("home-pending"), nil, 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(c.path("installed"), nil, 0o644); err != nil {
		return err
	}
	say("installed")
	return nil
}

// installFromImage creates the disk of the VM from the disk image of the system. The host
// builds the image from the committed configuration directory.
func (s *supervisor) installFromImage(ctx context.Context) error {
	c := s.cfg
	installDir := c.path("install")
	say("new VM: building the disk image of the system on the Linux builder")
	if err := checkLinuxBuilder(ctx, c.guestArch+"-linux"); err != nil {
		warn("%v", err)
		return errors.New(builderHelp)
	}

	if err := os.Mkdir(installDir+"/src", 0o755); err != nil {
		return err
	}
	archive := s.archiveConfig(ctx, "")
	archive.Stderr = os.Stderr
	if err := pipe(archive, show(helper(ctx, "tar", "-x", "-C", installDir+"/src"))); err != nil {
		return fmt.Errorf("could not copy %s/: %w", c.configDir, err)
	}
	// The out-link prevents garbage collection while this function copies the image.
	flake := "path:" + installDir + "/src/" + c.configDir + "#nixosConfigurations." + c.configName + "-" + c.guestArch +
		".config.system.build.sandboxVmImage"
	build := helper(ctx, "nix", "--extra-experimental-features", "nix-command flakes", "build",
		"--out-link", installDir+"/image", "--no-write-lock-file",
		"--option", "extra-substituters", c.substituters,
		"--option", "extra-trusted-public-keys", c.trustedPublicKeys, flake)
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		warn("could not build the disk image. If the error is that the attribute sandboxVmImage is missing, update devenv-sandbox-vm in %s/flake.lock", c.configDir)
		return errors.New(builderHelp)
	}

	image := installDir + "/image/nixos.img"
	st, err := os.Stat(image)
	if err != nil {
		return err
	}
	if st.Size() > c.diskGB<<30 {
		return fmt.Errorf("the disk image is %d GiB: services.sandbox-vm.disk is too small", st.Size()>>30+1)
	}
	disk := c.path("disk.img")
	if err := cloneFile(ctx, image, disk); err != nil {
		return err
	}
	// The file in the Nix store is read-only.
	if err := os.Chmod(disk, 0o600); err != nil {
		return err
	}
	if err := os.Truncate(disk, c.diskGB<<30); err != nil {
		return err
	}
	// The first boot gets the files of the host (firstBootCredentials). The file stays until
	// SSH is available in the installed system.
	return os.WriteFile(c.path("first-boot"), nil, 0o644)
}

// installFromISO boots the installer ISO one time, and the installer installs NixOS.
func (s *supervisor) installFromISO(ctx context.Context) error {
	c := s.cfg
	installDir := c.path("install")
	say("new VM: installing NixOS from %s", c.installerISO)
	if _, err := os.Stat(c.installerISO); err != nil {
		return fmt.Errorf("services.sandbox-vm.installer.iso: %w", err)
	}
	// The hypervisor opens each disk for writes, thus it gets a copy.
	iso := installDir + "/installer.iso"
	if err := cloneFile(ctx, c.installerISO, iso); err != nil {
		return err
	}
	if err := os.Chmod(iso, 0o600); err != nil {
		return err
	}
	disk, err := os.Create(c.path("disk.img"))
	if err == nil {
		err = disk.Truncate(c.diskGB << 30)
		disk.Close()
	}
	if err != nil {
		return err
	}

	key, err := os.ReadFile(c.path("id_ed25519.pub"))
	if err != nil {
		return err
	}
	hostKeyFile := installDir + "/ssh_host_ed25519_key"
	if err := show(helper(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "installer", "-f", hostKeyFile)).Run(); err != nil {
		return fmt.Errorf("ssh-keygen: %w", err)
	}
	hostKey, err := os.ReadFile(hostKeyFile)
	if err != nil {
		return err
	}

	if err := s.startNetwork(ctx); err != nil {
		return err
	}
	pub, err := os.ReadFile(hostKeyFile + ".pub")
	if err != nil {
		return err
	}
	f := strings.Fields(string(pub))
	if len(f) < 2 {
		return fmt.Errorf("%s.pub is not a public key", hostKeyFile)
	}
	known := fmt.Sprintf("[127.0.0.1]:%d %s %s\n", s.sshPort, f[0], f[1])
	if err := os.WriteFile(installDir+"/known_hosts", []byte(known), 0o644); err != nil {
		return err
	}

	installer, err := hvInstallerCommand(c, s.hvSockDir, iso, installerCredentials(key, hostKey))
	if err != nil {
		return err
	}
	vm, err := s.startHypervisor(installer)
	if err != nil {
		return err
	}
	driveCtx, stopDrive := context.WithCancel(ctx)
	defer stopDrive()
	result := make(chan error, 1)
	go func() { result <- s.driveInstaller(driveCtx) }()

	select {
	case err = <-result:
		// The last step of the installation stops the VM. Without the stop here, a VM
		// with an installation that failed continues to run.
		if err == nil {
			select {
			case <-vm.done:
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
			}
		}
		s.hvStop()
		<-vm.done
	case <-vm.done:
		// The VM stopped before the end of the installation. The installation fails
		// immediately, or it does not continue.
		select {
		case err = <-result:
		case <-time.After(10 * time.Second):
			stopDrive()
			err = <-result
		}
	case <-ctx.Done():
		s.hvStop()
		<-vm.done
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			warn("%v", err)
		}
		return fmt.Errorf("install failed; the installer's console is in %s", c.path("log", "console", "current"))
	}
	return nil
}

// driveInstaller waits for the installer, sends the files of the host to it, installs
// NixOS and stops the installer.
func (s *supervisor) driveInstaller(ctx context.Context) error {
	c := s.cfg
	start := time.Now()
	warned := false
	for s.installerSSH(ctx, "true").Run() != nil {
		if !sleep(ctx, 3*time.Second) {
			return ctx.Err()
		}
		if !warned && time.Since(start) > 3*time.Minute {
			warned = true
			warn("the installer does not answer. The ISO must be a current NixOS installer for %s; its console is in %s",
				c.guestArch, c.path("log", "console", "current"))
		}
	}
	say("installer is up")

	archive := s.archiveConfig(ctx, "src/")
	archive.Stderr = os.Stderr
	unpack := showGuest(s.installerSSH(ctx, "mkdir "+installStage+" && tar -x -m -C "+installStage))
	if err := pipe(archive, unpack); err != nil {
		return fmt.Errorf("could not send %s/ to the installer: %w", c.configDir, err)
	}
	env, err := os.ReadFile(c.path("env"))
	if err != nil {
		return err
	}
	key, err := os.ReadFile(c.path("id_ed25519.pub"))
	if err != nil {
		return err
	}
	flake := "path:" + installStage + "/src/" + c.configDir + "#" + c.configName
	steps := []struct {
		what, command string
		input         []byte
	}{
		{"send the settings", "cat > " + installStage + "/env", env},
		{"send the key", "cat > " + installStage + "/authorized_keys", key},
		{"send the version", "cat > " + installStage + "/provisioned", []byte(s.configTree + "\n")},
		{"install NixOS", s.proxyEnv() + " bash -s -- " + shellQuote(installStage) + " " +
			shellQuote(c.guestState) + " " + shellQuote(flake) + " " + c.nixOptions, guestInstall},
	}
	for _, step := range steps {
		cmd := showGuest(s.installerSSH(ctx, step.command))
		if step.input != nil {
			cmd.Stdin = bytes.NewReader(step.input)
		}
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("could not %s: %w", step.what, err)
		}
	}
	// The connection can stop before the command gives its status. Thus the status of the
	// command does not show if the installer stops.
	_ = s.installerSSH(ctx, "poweroff").Run()
	return nil
}

// copyFile makes a copy of a file. The copy stops when the context ends.
func copyFile(ctx context.Context, from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, ctxReader{ctx, in})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// ctxReader is a reader that fails when the context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
