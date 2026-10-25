package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// confinement tells what a program in a sandbox can do. The programs that read bytes from
// the guest use a sandbox, in case the guest gets control of one of them.
//
// It is the intent only. The platform file creates the rules of the sandbox from it.
//
// The program can do only what the confinement names. All other things are denied: files,
// connections, other programs and the services of the operating system.
//
// Files and programs: the program can read only the store paths in storePaths and the
// libraries of the system. It can write only the files in dataFiles and the directories in
// writeDirs. It cannot start a different program.
//
// Unix sockets: each socket has its own rule, with its full path. A rule for the directory
// of the VM is not safe: that directory also has the control socket of ssh (forwards.go),
// and a program that connects to it can open forwards on the host.
//
// Environment: the program gets only the variables that confinedEnv returns.
type confinement struct {
	// A file with the paths of the Nix store that the program can read, one in each line.
	// Option: --store-paths FILE
	storePaths string
	// The program can also start the programs in these paths. Option: --exec-closure
	execClosure bool
	// Files that exist, and that the program can read and write. It cannot remove them or
	// put a different file in their place.
	dataFiles []string //nolint:unused // Only platform_darwin.go uses it.
	// Directories that the program can read and write. Option: --write-dir DIR
	writeDirs []string
	// The only port that the program can bind. Empty: no limit.
	listen string
	// The network. Option: --egress
	//   none        no network
	//   internet    connections to all hosts but this computer
	//   local:PORT  connections only to this port of this computer
	egress string
	// Unix sockets that the program creates and listens on.
	bindSockets []string
	// Unix sockets that the program connects to.
	connectSockets []string //nolint:unused // Only platform_darwin.go uses it.
	// The program connects to the unix sockets in these directories.
	connectSocketDirs []string
	// More environment variables that the program gets. Option: --env NAME
	env []string
}

// The values of confinement.egress.
const (
	egressNone        = "none"
	egressInternet    = "internet"
	egressLocalPrefix = "local:"
)

// egressLocal returns the egress that permits only one port of this computer.
func egressLocal(port int) string {
	return egressLocalPrefix + strconv.Itoa(port)
}

// parseConfineArgs reads the arguments of `sandbox-vm confine`. It returns the confinement
// and the command after "--". It has only the options that the shell scripts use (borg in
// host/scripts/backup.sh). The supervisor sets the other fields directly.
func parseConfineArgs(args []string) (confinement, []string, error) {
	var c confinement
	for i := 0; i < len(args); i++ {
		opt := args[i]
		switch opt {
		case "--":
			if i+1 >= len(args) {
				return c, nil, fmt.Errorf("no program after --")
			}
			return c, args[i+1:], nil
		case "--exec-closure":
			c.execClosure = true
			continue
		}
		if i+1 >= len(args) {
			return c, nil, fmt.Errorf("option %s has no value", opt)
		}
		i++
		switch opt {
		case "--store-paths":
			c.storePaths = args[i]
		case "--egress":
			c.egress = args[i]
		case "--write-dir":
			c.writeDirs = append(c.writeDirs, args[i])
		case "--env":
			c.env = append(c.env, args[i])
		default:
			return c, nil, fmt.Errorf("unknown option %s", opt)
		}
	}
	return c, nil, fmt.Errorf("no -- before the program")
}

// confinedEnv returns the variables of environ that a program in a sandbox gets: the time
// zone, the language, the directory for temporary files, and the variables in names.
//
// The environment of this program is that of the shell of the project, and it can contain
// passwords and keys. A program that the guest controls can read its own environment.
func confinedEnv(environ, names []string) []string {
	keep := map[string]bool{"TZ": true, "LANG": true, "TMPDIR": true}
	for _, n := range names {
		keep[n] = true
	}
	// Not nil: for exec.Cmd, nil is the environment of this program.
	env := []string{}
	for _, v := range environ {
		if name, _, _ := strings.Cut(v, "="); keep[name] || strings.HasPrefix(name, "LC_") {
			env = append(env, v)
		}
	}
	return env
}

// confinedCommand returns the command that starts a program in a sandbox.
func confinedCommand(c confinement, prog string, args ...string) (*exec.Cmd, error) {
	argv, err := confinedArgv(c, append([]string{prog}, args...))
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = confinedEnv(os.Environ(), c.env)
	return cmd, nil
}
