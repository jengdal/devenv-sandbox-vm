package main

// The network of the VM.
//
// gvproxy is the network of the guest. It opens the connections of the guest from the host.
// Its sandbox permits only one destination, the proxy. Thus there is no other route out of
// the VM. This includes DNS: without the sandbox, gvproxy answers lookups from the guest
// through the resolver of the host. gvproxy connects to that resolver through a unix socket.
// Thus the sandbox permits only the unix sockets to the hypervisor. The proxy resolves the
// names: the resolver is the only unix socket that its sandbox permits. The guest connects
// to the proxy at SVM_PROXY_GATEWAY. gvproxy maps that address to the loopback address of
// the host.
//
// gvproxy and the proxy can each bind only their own port. The guest can tell gvproxy to
// forward more host ports, and the sandbox makes these requests fail. The proxy cannot
// connect to the host.
//
// The guest connects to the proxy only by its port. Thus the port of the proxy is the port
// of one VM (portCandidates), and the process keeps the port if the proxy stops while the
// guest runs (watchNetwork).
//
// The sandboxes deny all that they do not name, in case the guest gets control of gvproxy
// or of the proxy (refer to confinement). In the Nix store, they can read only their own
// paths. gvproxy can create only its socket, and the proxy can write nothing. Thus they
// cannot change ssh.config, which the SSH configuration of the user includes. They also
// cannot connect to the control socket of ssh (forwards.go): a program that connects to it
// can open forwards on the host.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// portInUse is true if a program accepts connections on the port of this computer.
func portInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// sshConfigPorts returns the SSH ports in the text of an ssh.config file.
func sshConfigPorts(text string) []int {
	var ports []int
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "Port" {
			if p, err := strconv.Atoi(f[1]); err == nil {
				ports = append(ports, p)
			}
		}
	}
	return ports
}

var envProxyPort = regexp.MustCompile(`(?m)^proxy_port="([0-9]+)"$`)

// envProxyPorts returns the port of the proxy in the text of an env file (startNetwork).
func envProxyPorts(text string) []int {
	var ports []int
	if m := envProxyPort.FindStringSubmatch(text); m != nil {
		if p, err := strconv.Atoi(m[1]); err == nil {
			ports = append(ports, p)
		}
	}
	return ports
}

// recordedPorts reads a file of the directory of each VM. It returns the ports in the file of
// this VM, and the ports in the files of the other VMs.
func (s *supervisor) recordedPorts(file string, ports func(text string) []int) (last, taken []int) {
	c := s.cfg
	all, _ := filepath.Glob(filepath.Join(c.home, ".devenv-sandbox-vm", "*", file))
	for _, f := range all {
		text, _ := os.ReadFile(f)
		if f == c.path(file) {
			last = ports(string(text))
		} else {
			taken = append(taken, ports(string(text))...)
		}
	}
	return last, taken
}

// portCandidates returns the ports to try, in sequence: first the ports of the last run, then
// the range from first to end. It leaves out the ports of the other VMs.
//
// Thus a VM keeps its ports, and no other VM uses them, also while the VM does not run.
// For the port of the proxy that is a protection: the guest connects to the proxy only by
// its port. If the proxy stops and the proxy of a different VM gets the port, the guest
// has the rules of that VM.
func portCandidates(last []int, taken []int, first, end int) []int {
	skip := map[int]bool{}
	for _, p := range taken {
		skip[p] = true
	}
	var ports []int
	for _, p := range last {
		if !skip[p] {
			ports = append(ports, p)
		}
	}
	for p := first; p <= end; p++ {
		if !skip[p] {
			ports = append(ports, p)
		}
	}
	return ports
}

// sshConfig returns the text of ssh.config. The paths have quotation marks, because a home
// directory can have a space in its name.
//
// The Include line is at the top of the SSH configuration of the user, and for these
// options ssh uses the first value. Thus the settings of the user for all hosts do not give
// the guest the agent, the X11 display or a local command. Do not set ClearAllForwardings:
// the ports program opens the forwards on the connection of openForwards.
func sshConfig(name string, port int, user, dir string) string {
	return strings.Join([]string{
		"Host " + name,
		"  HostName 127.0.0.1",
		"  Port " + strconv.Itoa(port),
		"  User " + user,
		`  IdentityFile "` + filepath.Join(dir, "id_ed25519") + `"`,
		"  IdentitiesOnly yes",
		"  ForwardAgent no",
		"  ForwardX11 no",
		"  ForwardX11Trusted no",
		"  PermitLocalCommand no",
		"  HostKeyAlias " + name,
		`  UserKnownHostsFile "` + filepath.Join(dir, "known_hosts") + `"`,
		"  StrictHostKeyChecking accept-new",
		"",
	}, "\n")
}

func (s *supervisor) stopNetwork() {
	stopPid(s.cfg.path("gvproxy.pid"), "gvproxy", nil)
	stopPid(s.cfg.path("smokescreen.pid"), "smokescreen", nil)
	s.releaseProxyPort()
	hvCleanupNetwork(s.hvSockDir)
	s.hvSockDir = ""
}

// The time that the hypervisor gets to stop after gvproxy stopped. gvproxy stops when its
// hypervisor stops, and that is not a failure. The tests change it.
var gvproxyGrace = 2 * time.Second

// watchNetwork shows a message if the proxy or gvproxy stops while the guest runs. The
// VM continues: the user decides when to stop it. It runs for one boot, until the
// hypervisor stops or the context ends.
func (s *supervisor) watchNetwork(ctx context.Context, vm *child) {
	c := s.cfg
	var proxy, gvproxy <-chan struct{}
	if s.proxy != nil {
		proxy = s.proxy.done
	}
	if s.gvproxy != nil {
		gvproxy = s.gvproxy.done
	}
	for proxy != nil || gvproxy != nil {
		select {
		case <-ctx.Done():
			return
		case <-vm.done:
			return
		case <-proxy:
			proxy = nil
			if !s.holdProxyPort() {
				// holdProxyPort stopped gvproxy and told the user.
				gvproxy = nil
			}
		case <-gvproxy:
			gvproxy = nil
			select {
			case <-ctx.Done():
				return
			case <-vm.done:
				return
			case <-time.After(gvproxyGrace):
			}
			warn("gvproxy stopped: the VM has no network, and SSH to the VM does not work. "+
				"Restart this process. Its log is %s", c.path("log", "gvproxy", "current"))
		}
	}
}

// holdProxyPort binds the port of a proxy that stopped, and tells the user. The guest
// connects to the proxy only by its port, and gvproxy permits that port. Thus no other
// program must get the port while the guest runs: the guest can then connect to that
// program. Connections to the port stop immediately, thus programs in the guest do not wait.
//
// It is false if a different program already has the port. Then the function stops
// gvproxy: the guest had no connection out, and now it also has no connection to that
// program.
func (s *supervisor) holdProxyPort() bool {
	c := s.cfg
	log := c.path("log", "smokescreen", "current")
	hold, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.proxyPort)))
	if err != nil {
		stopPid(c.path("gvproxy.pid"), "gvproxy", nil)
		warn("the proxy stopped, and a different program has its port %d now. The VM must not connect to that program, "+
			"thus the network of the VM is stopped, and SSH to the VM does not work. "+
			"Restart this process. The log of the proxy is %s", s.proxyPort, log)
		return false
	}
	s.proxyHold = hold
	go func() {
		for {
			conn, err := hold.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	warn("the proxy stopped: the VM has no connection out. To start the proxy again, reboot the VM (sudo reboot), "+
		"or restart this process. The log of the proxy is %s", log)
	return true
}

// releaseProxyPort ends holdProxyPort, thus the proxy of the subsequent boot can get the port.
func (s *supervisor) releaseProxyPort() {
	if s.proxyHold != nil {
		s.proxyHold.Close()
		s.proxyHold = nil
	}
}

// startNetwork starts the network of the VM: first the proxy, then gvproxy with its SSH
// forward. gvproxy stops when its hypervisor stops. Thus this function must run before
// each boot. It sets proxyPort and sshPort.
func (s *supervisor) startNetwork(ctx context.Context) error {
	s.stopNetwork()
	dir, err := hvNetworkDir()
	if err != nil {
		return err
	}
	s.hvSockDir = dir
	if err := s.startProxy(ctx); err != nil {
		return err
	}
	// Settings for the services in the guest.
	env := fmt.Sprintf("hostname=\"%s\"\nuser=\"%s\"\nproxy_port=\"%d\"\ntimezone=\"%s\"\n",
		s.cfg.hostname, s.vmUser, s.proxyPort, hostTimeZone())
	if err := os.WriteFile(s.cfg.path("env"), []byte(env), 0o644); err != nil {
		return err
	}
	return s.startGvproxy(ctx)
}

// The guest reads its settings as shell text. Thus a time zone has only these characters.
var zoneName = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)*$`)

// hostTimeZone is the time zone of the host, for example Europe/Helsinki. The guest uses
// it if its configuration sets no time zone. It is empty if the host does not show its
// time zone; the guest then keeps its time zone.
func hostTimeZone() string {
	link, _ := os.Readlink("/etc/localtime")
	return timeZone(os.Getenv("TZ"), link)
}

// timeZone returns the time zone from the variable TZ, or from the target of the symbolic
// link /etc/localtime if the variable is empty. The target ends with the name of the time
// zone, after a directory with the name zoneinfo.
func timeZone(tz, link string) string {
	zone := strings.TrimPrefix(tz, ":")
	if zone == "" {
		_, zone, _ = strings.Cut(link, "zoneinfo/")
	}
	if !zoneName.MatchString(zone) {
		return ""
	}
	return zone
}

// The ports on 127.0.0.1 of the host that the process selects from: the SSH forward of
// gvproxy and the proxy.
const (
	sshPortFirst, sshPortLast     = 22000, 22999
	proxyPortFirst, proxyPortLast = 23000, 23999
)

// The time that a program gets to open its port.
const listenTimeout = 30 * time.Second

// startOnPort starts a program on the first port of the candidates that is free, and gives
// that port and the program. start starts the program for one port. listening is true when
// the program accepts connections.
//
// A program stops if a different program took its port after this function checked it.
// Then the function tries the subsequent port.
func startOnPort(ctx context.Context, candidates []int, start func(port int) (*child, error), listening func(port int) bool) (int, *child, error) {
	for _, p := range candidates {
		if portInUse(p) {
			continue
		}
		prog, err := start(p)
		if err != nil {
			return 0, nil, err
		}
		waitUntil(ctx, listenTimeout, func() bool { return !prog.alive() || listening(p) })
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		if !prog.alive() {
			continue
		}
		if !listening(p) {
			prog.kill()
			return 0, nil, fmt.Errorf("no answer on port %d after %s", p, listenTimeout)
		}
		return p, prog, nil
	}
	return 0, nil, errors.New("no port is free")
}

// The port of the proxy is the port of the last run if it is free. The file `env` records
// the port (startNetwork).
func (s *supervisor) startProxy(ctx context.Context) error {
	c := s.cfg
	last, taken := s.recordedPorts("env", envProxyPorts)
	candidates := portCandidates(last, taken, proxyPortFirst, proxyPortLast)
	port, proxy, err := startOnPort(ctx, candidates, func(p int) (*child, error) {
		port := strconv.Itoa(p)
		cmd, err := confinedCommand(confinement{
			listen:     port,
			egress:     egressInternet,
			storePaths: c.smokescreenStorePaths,
		}, c.smokescreen, "--config-file", c.smokescreenConfig, "--listen-port", port)
		if err != nil {
			return nil, err
		}
		out, err := s.logWriter("smokescreen")
		if err != nil {
			return nil, err
		}
		defer out.Close()
		cmd.Stdout, cmd.Stderr = out, out
		return startDetached(cmd, c.path("smokescreen.pid"))
	}, portInUse)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return fmt.Errorf("could not start the proxy: %w; see %s", err, c.path("log", "smokescreen", "current"))
	}
	s.proxyPort, s.proxy = port, proxy
	return nil
}

// The SSH port is the port of the last run if it is free. If it is not free, the function
// selects a new port. Only ssh.config records the port.
func (s *supervisor) startGvproxy(ctx context.Context) error {
	c := s.cfg
	last, taken := s.recordedPorts("ssh.config", sshConfigPorts)
	candidates := portCandidates(last, taken, sshPortFirst, sshPortLast)
	port, gvproxy, err := startOnPort(ctx, candidates, func(p int) (*child, error) {
		os.Remove(c.path("net.sock"))
		port := strconv.Itoa(p)
		args := append(gvproxyListenArgs(c), "-ssh-port", port)
		// gvproxy listens on net.sock, and the hypervisor has its end of the connection in
		// hvSockDir.
		cmd, err := confinedCommand(confinement{
			listen:            port,
			egress:            egressLocal(s.proxyPort),
			storePaths:        c.gvproxyStorePaths,
			bindSockets:       []string{c.path("net.sock")},
			connectSocketDirs: []string{s.hvSockDir},
		}, c.gvproxy, args...)
		if err != nil {
			return nil, err
		}
		out, err := s.logWriter("gvproxy")
		if err != nil {
			return nil, err
		}
		defer out.Close()
		cmd.Stdout, cmd.Stderr = out, out
		return startDetached(cmd, c.path("gvproxy.pid"))
	}, func(p int) bool {
		st, err := os.Stat(c.path("net.sock"))
		return err == nil && st.Mode()&os.ModeSocket != 0 && portInUse(p)
	})
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return fmt.Errorf("could not start gvproxy: %w; see %s", err, c.path("log", "gvproxy", "current"))
	}
	s.sshPort, s.gvproxy = port, gvproxy
	return os.WriteFile(c.path("ssh.config"), []byte(sshConfig(c.name, port, s.vmUser, c.dir)), 0o644)
}

// proxyEnv returns the proxy variables for the commands that the host runs in the guest: the
// proxy address as the guest sees it. Thus the installation does not depend on the
// configuration of the guest. The task sandbox-vm:update sets the same variables
// (host/scripts/update.sh).
func (s *supervisor) proxyEnv() string {
	url := fmt.Sprintf("http://%s:%d", s.cfg.proxyGateway, s.proxyPort)
	return "http_proxy=" + url + " https_proxy=" + url + " HTTPS_PROXY=" + url
}
