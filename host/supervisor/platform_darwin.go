//go:build darwin

package main

// The platform file for macOS: the hypervisor krunkit (libkrun), and the macOS sandbox
// (sandbox-exec).
//
// A platform file gives these definitions to the other files:
//
//	confinedArgv        the command line that starts a program in a sandbox
//	execSandbox         starts a program in a sandbox whose rules contain its pid
//	hvProgram           the name of the hypervisor program, to stop it by its pid file
//	hvNetworkDir        creates the directory for the socket of the hypervisor
//	hvInstallerCommand  boots the installer from an ISO
//	cloneFile           makes a copy of a large file
//	hvStartCommand      boots the installed system
//	hvCleanupNetwork    removes what the hypervisor left of its connection to gvproxy
//	gvproxyListenArgs   the arguments with which gvproxy connects to the hypervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// sandboxProfile returns the rules of sandbox-exec for a program.
//
// The first rule denies all operations. Each subsequent rule permits one thing that the
// program needs: its own files, the libraries of the system, and the network of its
// confinement. Thus the program gets no service of the system (Mach, XPC, IOKit, Apple
// Events), no file and no signal to a different program, unless a rule here permits it.
// A new rule needs a comment that tells which function of a program does not work
// without it.
//
// Network: for the sandbox, "localhost" is each address of this computer, not only the
// loopback address.
func sandboxProfile(c confinement, prog string) (string, error) {
	var b strings.Builder
	rule := func(format string, a ...any) {
		fmt.Fprintf(&b, format+"\n", a...)
	}
	store, err := readLines(c.storePaths)
	if err != nil {
		return "", err
	}

	rule(`(version 1)`)
	rule(`(deny default)`)

	// The program itself. The Go runtime sends signals to its own threads.
	rule(`(allow process-exec (literal %s))`, quote(prog))
	rule(`(allow file-read* (literal %s))`, quote(prog))
	rule(`(allow signal (target self))`)
	rule(`(allow process-info* (target self))`)
	// The libraries of the system read the properties of the computer at their start.
	rule(`(allow sysctl-read)`)

	// The libraries of the system. All of them are in one cache file, thus the rule is not
	// for single libraries.
	rule(`(allow file-read*`)
	for _, p := range []string{"/System", "/usr/lib", "/usr/share", "/Library/Apple"} {
		rule(`  (subpath %s)`, quote(p))
	}
	rule(`  (literal "/")`)
	rule(`  (literal "/dev/random")`)
	rule(`  (literal "/dev/urandom"))`)
	rule(`(allow file-map-executable`)
	rule(`  (subpath "/System/Library/Frameworks")`)
	rule(`  (subpath "/System/Library/PrivateFrameworks")`)
	rule(`  (subpath "/usr/lib"))`)
	rule(`(allow file-read* file-write-data`)
	rule(`  (literal "/dev/null")`)
	rule(`  (literal "/dev/zero"))`)
	// The time zone, for the time in the lines of a log. Without these rules, each line
	// of a log causes two denials.
	rule(`(allow file-read-metadata`)
	rule(`  (literal "/etc")`)
	rule(`  (literal "/tmp")`)
	rule(`  (literal "/var"))`)
	rule(`(allow file-read*`)
	rule(`  (literal "/private/etc/localtime")`)
	rule(`  (subpath "/private/var/db/timezone"))`)

	// The files of the program.
	for _, p := range store {
		rule(`(allow file-read* file-map-executable (subpath %s))`, quote(p))
	}
	if c.execClosure {
		rule(`(allow process-fork)`)
		for _, p := range store {
			rule(`(allow process-exec (subpath %s))`, quote(p))
		}
	}
	// The program cannot create or remove these files: the rule is only for their data.
	// With a rule for all writes, the program can put a symbolic link in the place of a
	// file, and a different program then writes the target of the link.
	for _, f := range c.dataFiles {
		rule(`(allow file-read* file-write-data (literal %s))`, quote(f))
	}
	for _, d := range c.writeDirs {
		rule(`(allow file-read* file-write* (subpath %s))`, quote(d))
	}
	// The unix sockets. To listen on a socket, the program creates its file. To connect to a
	// socket, the program needs no rule for its file.
	for _, f := range c.bindSockets {
		rule(`(allow file-read-metadata file-write-create (literal %s))`, quote(f))
		rule(`(allow network-bind network-inbound (local unix-socket (literal %s)))`, quote(f))
	}
	for _, f := range c.connectSockets {
		rule(`(allow network-outbound (remote unix-socket (literal %s)))`, quote(f))
	}
	for _, d := range c.connectSocketDirs {
		rule(`(allow network-outbound (remote unix-socket (subpath %s)))`, quote(d))
	}
	// A program finds a file through the directories above it. It can see that these
	// directories exist, but it cannot list them.
	paths := append([]string{prog}, store...)
	for _, p := range [][]string{c.dataFiles, c.writeDirs, c.bindSockets, c.connectSockets, c.connectSocketDirs} {
		paths = append(paths, p...)
	}
	if above := ancestors(paths); len(above) > 0 {
		rule(`(allow file-read-metadata`)
		for i, p := range above {
			end := ""
			if i == len(above)-1 {
				end = ")"
			}
			rule(`  (literal %s)%s`, quote(p), end)
		}
	}

	// The network.
	if c.listen != "" {
		rule(`(allow network-bind network-inbound (local tcp "localhost:%s"))`, c.listen)
	}
	port, local := strings.CutPrefix(c.egress, egressLocalPrefix)
	switch {
	case c.egress == egressNone:
	case c.egress == egressInternet:
		rule(`(allow network-outbound (remote tcp "*:*"))`)
		rule(`(deny network-outbound (remote ip "localhost:*"))`)
		// The resolver of the system.
		rule(`(allow network-outbound (literal "/private/var/run/mDNSResponder"))`)
	case local:
		rule(`(allow network-outbound (remote tcp "localhost:%s"))`, port)
	default:
		return "", fmt.Errorf("unknown --egress %q", c.egress)
	}
	return b.String(), nil
}

// ancestors returns the directories above the paths, without "/", each one time and in
// sequence.
func ancestors(paths []string) []string {
	seen := map[string]bool{}
	var above []string
	for _, p := range paths {
		for d := filepath.Dir(p); d != "/" && d != "." && !seen[d]; d = filepath.Dir(d) {
			seen[d] = true
			above = append(above, d)
		}
	}
	sort.Strings(above)
	return above
}

// quote returns a string of the profile language.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func readLines(file string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if sc.Text() != "" {
			lines = append(lines, sc.Text())
		}
	}
	return lines, sc.Err()
}

func confinedArgv(c confinement, argv []string) ([]string, error) {
	profile, err := sandboxProfile(c, argv[0])
	if err != nil {
		return nil, err
	}
	return sandboxExec(profile, argv), nil
}

const sandboxExecProgram = "/usr/bin/sandbox-exec"

func sandboxExec(profile string, argv []string) []string {
	return append([]string{sandboxExecProgram, "-p", profile}, argv...)
}

// pidToken is the text in a profile that execSandbox replaces with the pid of the program.
const pidToken = "@PID@"

// execSandbox is `sandbox-vm exec-sandbox PROFILE PROGRAM [ARGUMENT]...`. It starts a
// program in a sandbox whose rules contain the pid of the program. This process becomes
// sandbox-exec and then the program, thus the three have the same pid.
func execSandbox(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: sandbox-vm exec-sandbox PROFILE PROGRAM [ARGUMENT]...")
	}
	profile := strings.ReplaceAll(args[0], pidToken, strconv.Itoa(os.Getpid()))
	return syscall.Exec(sandboxExecProgram, sandboxExec(profile, args[1:]), os.Environ())
}

const hvProgram = "krunkit"

// hvNetworkDir creates the directory where krunkit creates its end of the network socket. The
// directory is new for each boot, and its name is random.
//
// The directory is in /tmp and not in TMPDIR, thus the path of the socket is always short.
func hvNetworkDir() (string, error) {
	dir, err := os.MkdirTemp("/tmp", "svm-")
	if err != nil {
		return "", err
	}
	// The sandbox uses the path without symbolic links.
	return filepath.EvalSymlinks(dir)
}

// hvStartCommand boots the installed system from the disk of the VM. The guest gets the
// credentials (install.go).
func hvStartCommand(c *config, sockDir string, credentials []string) (*exec.Cmd, error) {
	return krunkitCommand(c, sockDir, "", credentials)
}

// hvInstallerCommand boots the installer. The ISO is a second disk. The EFI firmware boots
// it, because the disk of a new VM is empty.
func hvInstallerCommand(c *config, sockDir, iso string, credentials []string) (*exec.Cmd, error) {
	return krunkitCommand(c, sockDir, iso, credentials)
}

// cloneFile makes a copy of a file. On one APFS volume the copy shares the data of the
// file, thus it is immediate and uses no space. The Nix store is a different volume: then
// the function copies the data. The path of cp is fixed: a cp of GNU has no option -c.
func cloneFile(ctx context.Context, from, to string) error {
	if helper(ctx, "/bin/cp", "-c", from, to).Run() == nil {
		return nil
	}
	os.Remove(to)
	if err := copyFile(ctx, from, to); err != nil {
		return fmt.Errorf("could not copy %s: %w", from, err)
	}
	return nil
}

// krunkit creates this socket for --timesync. The name contains its pid. The socket goes to
// the agent in the guest, which runs as root there. Thus the rule is for the socket of
// this krunkit only: with a rule for all pids, a krunkit that the guest controls can
// connect to the agent of a different VM.
const krunkitTimesyncSocket = "/private/tmp/krunkit_timesync_" + pidToken + ".sock"

// The value of RUST_LOG for krunkit and libkrun: the default level, without the messages
// of the vsock muxer of libkrun.
//
// --timesync adds a vsock device. On macOS, libkrun then also sends the time to the guest
// each 60 seconds, as a vsock datagram for its own kernel and init. No option stops that.
// A mainline kernel has no vsock datagrams and answers with a reset, and the muxer writes
// "unexpected dgram pkt: 3" for each answer.
const krunkitLogFilter = "info,krun_devices::virtio::vsock::muxer=off"

// krunkitCommand starts krunkit in a sandbox. gvproxy listens on net.sock. The console goes
// to the pipe of its log. krunkit tells the agent in the guest to set the clock when the
// host wakes from sleep.
//
// The guest controls the devices of krunkit, thus krunkit gets only the files of the VM
// that it uses, and no network. Its connection to gvproxy is a unix socket.
//
// The credentials are OEM strings of the SMBIOS table. systemd in the guest reads them.
//
// krunkit keeps its EFI firmware in its store path and has no file for EFI variables.
// libkrun creates its end of the network socket in TMPDIR, thus TMPDIR is sockDir
// (hvNetworkDir). The sandbox of gvproxy also permits that directory.
//
// The rules have the pid of krunkit, thus this program starts krunkit through itself
// (execSandbox).
func krunkitCommand(c *config, sockDir, iso string, credentials []string) (*exec.Cmd, error) {
	profile, err := krunkitProfile(c, sockDir, iso)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{
		c.krunkit,
		"--cpus", c.cpus, "--memory", c.memoryMiB,
		"--restful-uri", "none://",
		"--device", "virtio-blk,path=" + c.path("disk.img") + ",format=raw",
	}
	if iso != "" {
		args = append(args, "--device", "virtio-blk,path="+iso+",format=raw")
	}
	for _, cred := range credentials {
		args = append(args, "--oem-string", cred)
	}
	args = append(args,
		"--device", "virtio-net,type=unixgram,path="+c.path("net.sock")+",mac="+c.guestMAC+",offloading=on,vfkitMagic=on",
		"--device", "virtio-serial,logFilePath="+c.path("console.fifo"),
		"--timesync", "vsockPort="+c.timesyncPort)
	cmd := exec.Command(self, append([]string{"exec-sandbox", profile}, args...)...)
	cmd.Env = append(confinedEnv(os.Environ(), nil), "TMPDIR="+sockDir, "RUST_LOG="+krunkitLogFilter)
	return cmd, nil
}

func krunkitProfile(c *config, sockDir, iso string) (string, error) {
	files := []string{c.path("disk.img"), c.path("console.fifo")}
	// krunkit opens each disk for writes.
	if iso != "" {
		files = append(files, iso)
	}
	profile, err := sandboxProfile(confinement{
		egress:         egressNone,
		storePaths:     c.krunkitStorePaths,
		dataFiles:      files,
		writeDirs:      []string{sockDir},
		connectSockets: []string{c.path("net.sock")},
	}, c.krunkit)
	if err != nil {
		return "", err
	}
	// libkrun creates its end of the network socket in sockDir.
	//
	// For --timesync: krunkit creates its socket and connects to it, and it asks the system
	// to tell it when the computer wakes from sleep (RootDomainUserClient).
	return profile + fmt.Sprintf("(allow network-bind network-inbound (local unix-socket (subpath %[1]s)))\n"+
		"(allow file-read* file-write* (literal %[2]s))\n"+
		"(allow network-bind network-inbound (local unix-socket (literal %[2]s)))\n"+
		"(allow network-outbound (remote unix-socket (literal %[2]s)))\n"+
		"(allow iokit-open (iokit-user-client-class \"RootDomainUserClient\"))\n",
		quote(sockDir), quote(krunkitTimesyncSocket)), nil
}

// hvCleanupNetwork removes the directory of hvNetworkDir, if sockDir is not empty.
func hvCleanupNetwork(sockDir string) {
	if sockDir != "" {
		os.RemoveAll(sockDir)
	}
	// krunkit does not remove its socket for --timesync. The name contains the pid of a
	// krunkit that stopped.
	timesync, _ := filepath.Glob("/tmp/krunkit_timesync_*.sock")
	for _, s := range timesync {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(s), "krunkit_timesync_"), ".sock")
		if pid, err := strconv.Atoi(name); err == nil && syscall.Kill(pid, 0) == syscall.ESRCH {
			os.Remove(s)
		}
	}
}

func gvproxyListenArgs(c *config) []string {
	return []string{"-listen-vfkit", "unixgram://" + c.path("net.sock")}
}
