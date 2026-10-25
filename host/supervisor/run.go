package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"sandbox-vm/internal/termsafe"
)

// supervisor is the state of one run of the sandbox-vm process.
type supervisor struct {
	cfg *config
	// The git tree of the configuration directory. The installation creates the system of a
	// new VM from it (committedConfig).
	configTree string
	// The top directory of the git repo of the project. The project can be below it.
	repoTop string
	// The login name in the guest.
	vmUser string
	// The port of the proxy on the host.
	proxyPort int
	// The port on the host that gvproxy forwards to SSH in the guest.
	sshPort int
	// The proxy and gvproxy of the current boot (watchNetwork).
	proxy, gvproxy *child
	// The port of a proxy that stopped (holdProxyPort).
	proxyHold net.Listener
	// The directory for the socket of the hypervisor (hvNetworkDir).
	hvSockDir string
	// The hypervisor of the installed system, of the current boot.
	vm atomic.Pointer[child]
	// A signal asked for the shutdown. run does not boot the guest again.
	stopping atomic.Bool
	// The pipe of the console, open for writing, and console-filter that reads it
	// (startConsole).
	consoleKeep   *os.File
	consoleFilter *child
}

var (
	userName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	vmName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	hostName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,62}[A-Za-z0-9])?$`)
)

// The path of a unix socket has a maximum of 104 bytes on macOS. The longest name of a
// socket in the directory of the VM must fit.
const maxDirLength = 90

// The parts of a run. They decide what a signal does.
const (
	starting int32 = iota // a signal stops the run
	running               // a signal shuts down the guest
	cleaning              // a signal has no effect
)

// run is the sandbox-vm process.
func run(cfg *config) error {
	s := &supervisor{cfg: cfg}
	// The clone in the VM has the same devenv.nix, thus the clone also enables this service.
	// The NixOS module of the guest creates a marker file. This program exits when it finds the
	// file.
	if _, err := os.Stat(cfg.guestMarker); err == nil {
		return errors.New("this is the VM itself; start it from the host")
	}
	if err := s.prepare(); err != nil {
		return err
	}
	lock, err := lockVM(cfg)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := checkProject(cfg); err != nil {
		return err
	}

	// Stop a VM that stayed after a run that did not stop correctly.
	s.stopForwards()
	s.hvStop()
	s.stopLogs()
	os.Remove(cfg.path("ready"))

	ctx, cancel := context.WithCancel(context.Background())
	var phase atomic.Int32
	var shutdown sync.Once
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		// The process manager can send the signal again.
		for range signals {
			switch phase.Load() {
			case starting:
				cancel()
			case running:
				shutdown.Do(func() { go s.shutdown() })
			}
		}
	}()

	var jobs sync.WaitGroup
	defer func() {
		phase.Store(cleaning)
		ignoreSignals()
		s.hvStop()
		s.stopNetwork()
		os.RemoveAll(cfg.path("install"))
		os.Remove(cfg.path("ready"))
		cancel()
		jobs.Wait()
		s.stopForwards()
		s.stopLogs()
		os.Remove(cfg.path("forwards"))
		os.Remove(cfg.path("wanted"))
	}()

	if err := s.startLogs(); err != nil {
		return err
	}
	if _, err := os.Stat(cfg.path("installed")); err != nil {
		if err := s.installNewVM(ctx); err != nil {
			return err
		}
	}
	// The hypervisor does not start a guest again that reboots: it stops, the same as when
	// the guest powers off. Thus this program boots the guest again after a reboot.
	for again := false; ; again = true {
		vm, err := s.boot(ctx)
		if err != nil {
			return err
		}
		bootCtx, endBoot := context.WithCancel(ctx)
		jobs.Add(2)
		go func() {
			defer jobs.Done()
			s.connectGuest(bootCtx, vm, again)
		}()
		go func() {
			defer jobs.Done()
			s.watchNetwork(bootCtx, vm)
		}()
		phase.Store(running)
		// A signal that came before this point stopped the start, or it came between two
		// boots. But the VM runs now.
		if ctx.Err() != nil || s.stopping.Load() {
			shutdown.Do(func() { go s.shutdown() })
			if s.stopping.Load() {
				vm.kill()
			}
		}
		<-vm.done
		endBoot()
		jobs.Wait()
		os.Remove(cfg.path("ready"))
		s.stopForwards()
		if s.stopping.Load() || ctx.Err() != nil || !s.guestRebooted() {
			return nil
		}
		say("the VM rebooted: starting it again")
	}
}

// boot starts the network and the hypervisor of the installed system.
func (s *supervisor) boot(ctx context.Context) (*child, error) {
	cfg := s.cfg
	if err := s.startNetwork(ctx); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// A VM that is a copy of the disk image has no files from the host before its first boot.
	var credentials []string
	if _, err := os.Stat(cfg.path("first-boot")); err == nil {
		if credentials, err = s.firstBootCredentials(); err != nil {
			return nil, err
		}
	}
	start, err := hvStartCommand(cfg, s.hvSockDir, credentials)
	if err != nil {
		return nil, err
	}
	if err := s.markBoot(); err != nil {
		return nil, err
	}
	vm, err := s.startHypervisor(start)
	if err != nil {
		return nil, err
	}
	s.vm.Store(vm)
	return vm, nil
}

// startHypervisor starts the command of the hypervisor. This program writes the pid file:
// the hypervisor cannot write files of the directory of the VM.
//
// The guest can get control of the hypervisor, thus the messages of the hypervisor do not
// go directly to the output of this program. They go to the pipe of the console, and
// through its filter (startConsole).
func (s *supervisor) startHypervisor(cmd *exec.Cmd) (*child, error) {
	out, err := s.logWriter("console")
	if err != nil {
		return nil, err
	}
	defer out.Close()
	cmd.Stdout, cmd.Stderr = out, out
	return startDetached(cmd, s.cfg.path(hvProgram+".pid"))
}

// prepare checks the configuration and creates the directory and the key of the VM.
func (s *supervisor) prepare() error {
	c := s.cfg
	// The name is a directory name and a host alias of ssh. The hostname goes into a file
	// that the guest reads as shell text. host/options.nix has the same rules.
	if !vmName.MatchString(c.name) {
		return fmt.Errorf("%q is not a valid name of a VM; set services.sandbox-vm.name", c.name)
	}
	if !hostName.MatchString(c.hostname) {
		return fmt.Errorf("%q is not a valid hostname; set services.sandbox-vm.hostName", c.hostname)
	}
	if len(c.dir) > maxDirLength {
		return fmt.Errorf("%s is too long for a unix socket path; set a shorter services.sandbox-vm.name", c.dir)
	}
	s.vmUser = c.user
	if s.vmUser == "" {
		if u, err := user.Current(); err == nil {
			s.vmUser = u.Username
		}
	}
	if !userName.MatchString(s.vmUser) {
		return fmt.Errorf("%q is not a valid Linux user name; set services.sandbox-vm.user", s.vmUser)
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(c.dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(c.path("id_ed25519")); err != nil {
		keygen := show(helper(context.Background(), "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", c.name, "-f", c.path("id_ed25519")))
		if err := keygen.Run(); err != nil {
			return fmt.Errorf("ssh-keygen: %w", err)
		}
	}
	return nil
}

// connectGuest does the work that needs SSH in the installed system. It sends the settings to
// the guest again, and keeps the connection for the forwards open. It creates the file
// `ready`. The probe of the process checks that file.
//
// again is true after a reboot of the guest. The options of this program are from its
// start, thus the ports program then uses the forwards of its last run.
func (s *supervisor) connectGuest(ctx context.Context, vm *child, again bool) {
	c := s.cfg
	for s.vmSSH(ctx, "true").Run() != nil {
		if !sleep(ctx, 2*time.Second) {
			return
		}
	}
	// The guest stored the files of its first boot.
	os.Remove(c.path("first-boot"))
	if err := s.pushSettings(ctx); err != nil {
		if ctx.Err() == nil {
			warn("%v", err)
		}
		return
	}
	var forwardArgs []string
	if again {
		forwardArgs = []string{"--again"}
	}
	closed := s.openForwards(ctx, forwardArgs...)
	if err := os.WriteFile(c.path("ready"), nil, 0o644); err != nil {
		warn("%v", err)
		return
	}
	say("ready")
	s.sayHints()
	homeDone := s.applyHome(ctx)
	defer func() { <-homeDone }()
	for vm.alive() {
		select {
		case <-closed:
		case <-ctx.Done():
			return
		}
		if !sleep(ctx, 2*time.Second) {
			return
		}
		// Do not connect again during shutdown.
		if _, err := os.Stat(c.path("ready")); err != nil {
			return
		}
		if vm.alive() {
			// The options of this program are from its start. The ports program uses the
			// forwards of its last run.
			closed = s.openForwards(ctx, "--again")
		}
	}
}

// shutdown shuts down the guest correctly. It powers off the guest only if the host cannot
// connect to the guest, or if the guest does not stop in shutdownTimeout. Between two boots
// there is no guest: then run stops the new boot.
func (s *supervisor) shutdown() {
	s.stopping.Store(true)
	ignoreSignals()
	say("shutting down")
	os.Remove(s.cfg.path("ready"))
	poweroff := s.vmSSH(context.Background(), "sudo", "poweroff")
	poweroff.Stdout = termsafe.NewWriter(os.Stdout)
	vm := s.vm.Load()
	if poweroff.Run() == nil {
		select {
		case <-vm.done:
			return
		case <-time.After(shutdownTimeout):
			warn("the VM did not shut down in %s: powering it off", shutdownTimeout)
		}
	}
	// The pid of a hypervisor that stopped can be the pid of a different program.
	if vm.alive() {
		vm.kill()
	}
}

// The time that the guest gets to shut down after it accepted the command. It is less than
// processes.sandbox-vm.shutdown.grace (host/devenv.nix): after that time devenv kills
// this program, and the hypervisor, the network and the logs then continue without it.
// The difference is the time that this program needs to stop them. The tests change it.
var shutdownTimeout = 45 * time.Second

// ignoreSignals makes the signals of the process manager have no effect on this program and
// on the commands that it starts after this. The process manager can send the signal again,
// and the signal also goes to these commands. Nothing must stop too early then.
func ignoreSignals() {
	signal.Ignore(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
}

func (s *supervisor) hvStop() {
	stopPid(s.cfg.path(hvProgram+".pid"), hvProgram, nil)
}

func (s *supervisor) stopForwards() {
	stopPid(s.cfg.path("ssh.pid"), "ssh", nil)
}

// committedConfig checks the configuration directory and sets configTree and repoTop. The
// installation creates the system of a new VM only from the committed files of the directory.
// Thus the directory must not contain other changes. The task sandbox-vm:update has the
// same rule.
//
// The project can be in a directory below the top of its git repo. Thus the path of the
// tree starts with "./": without it, git reads the path from the top of the repo.
func (s *supervisor) committedConfig() error {
	c := s.cfg
	status := helper(context.Background(), "git", "-C", c.root, "status", "--porcelain", "--", c.configDir)
	status.Stderr = os.Stderr
	if changes, _ := status.Output(); len(changes) > 0 {
		return fmt.Errorf("%s/ has uncommitted changes; commit or revert them first", c.configDir)
	}
	revParse := helper(context.Background(), "git", "-C", c.root, "rev-parse", "HEAD:./"+c.configDir)
	revParse.Stderr = os.Stderr
	tree, err := revParse.Output()
	if err != nil {
		return fmt.Errorf("%s/ must be committed", c.configDir)
	}
	s.configTree = strings.TrimSpace(string(tree))
	showTop := helper(context.Background(), "git", "-C", c.root, "rev-parse", "--show-toplevel")
	showTop.Stderr = os.Stderr
	top, err := showTop.Output()
	if err != nil {
		return fmt.Errorf("could not find the top of the git repo of %s", c.root)
	}
	s.repoTop = strings.TrimSuffix(string(top), "\n")
	return nil
}

// archiveConfig returns the command that writes the configuration directory as a tar stream.
// The names in the stream start with prefix and the name of the directory.
//
// The stream has the files of configTree, and not those of HEAD at this moment. Thus the
// guest gets the files that it records as its version, also if a commit came after
// committedConfig.
//
// git starts at the top of the repo: in a directory below it, the archive of a tree is
// empty.
func (s *supervisor) archiveConfig(ctx context.Context, prefix string) *exec.Cmd {
	return helper(ctx, "git", "-C", s.repoTop, "archive", "--prefix="+prefix+s.cfg.configDir+"/", s.configTree)
}

// lockVM makes sure that only one sandbox-vm process runs this VM. A second process
// stops the hypervisor of the first as "a VM that stayed". The lock ends when this program
// stops: the programs that it starts do not get the file.
func lockVM(c *config) (*os.File, error) {
	f, err := os.OpenFile(c.path("lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("a sandbox-vm process already runs the VM %s", c.name)
	}
	return f, nil
}

// checkProject makes sure that the VM is the VM of this project. The default name of a VM
// comes from the name of the directory of the project, thus two projects in directories
// with the same name get the same VM. Without this check, the second project boots the
// disk of the first, with its own rules for the network: its programs then get the repos
// and the keys of the first project.
//
// The file `root` in the directory of the VM records the project. A VM without the file is
// new: it becomes the VM of this project.
func checkProject(c *config) error {
	file := c.path("root")
	text, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(file, []byte(c.root+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	if owner := strings.TrimSuffix(string(text), "\n"); owner != c.root {
		return fmt.Errorf("the VM %s is the VM of the project %s, and this project is %s. "+
			"Projects in directories with the same name get the same VM: set services.sandbox-vm.name in one of them. "+
			"If you moved the project, remove the file %s", c.name, owner, c.root, file)
	}
	return nil
}

// sayHints tells the user how to log in and where the logs are. The message has the alias
// of ssh.config and not the port, because the port can change between two runs. This
// program does not read the SSH configuration of the user, thus it always shows the Include
// line that the alias needs.
func (s *supervisor) sayHints() {
	c := s.cfg
	say("Log in to the VM with: ssh %s", c.name)
	say("  \"ssh %s\" needs this line at the top of ~/.ssh/config:", c.name)
	say("  Include ~/.devenv-sandbox-vm/*/ssh.config")
	say("logs: ~/.devenv-sandbox-vm/%s/log/", c.name)
}

// applyHome applies the home-manager configuration (host/scripts/home.sh) to a new VM, while
// the VM is already ready. The program removes the file `home-pending` when it succeeds. Thus
// after a failure, the next start tries again. The channel closes when the program stops.
func (s *supervisor) applyHome(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	c := s.cfg
	if _, err := os.Stat(c.path("home-pending")); c.homeProgram == "" || err != nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		if err := show(helper(ctx, c.homeProgram)).Run(); err != nil && ctx.Err() == nil {
			warn("could not apply the home-manager configuration: %v. To try again: devenv tasks run sandbox-vm:home", err)
		}
	}()
	return done
}
