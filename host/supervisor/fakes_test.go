package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"sandbox-vm/internal/termsafe"
)

// The fake programs of the VM for the tests of the sandbox-vm process (run_darwin_test.go).
// A copy of the test binary with the name of a program is that program: the
// sandbox-vm process stops a program by its pid file and its name.
//
// The copies are in one directory. The files of that directory tell the programs what to
// do, and the programs record there what they got:
//
//	proxy-wait         the proxy does not open its port while this file exists
//	vm-command         "reboot PID" or "poweroff PID": the guest of the hypervisor with this
//	                   pid writes the line of systemd on its console, and the hypervisor
//	                   stops
//	ssh-down           ssh cannot connect. It also cannot connect while no hypervisor runs:
//	                   the hypervisor writes its pid to the disk of the VM.
//	ssh-fail           ssh fails for a command that contains the text of this file
//	poweroff-ignored   the guest accepts `poweroff` and does not stop
//	ssh.log            the arguments of each ssh
//	guest-env          the settings that the guest got
//	stdin-N            the input of the ssh command of line N of ssh.log, if it has input
var fakeNames = []string{"krunkit", "gvproxy", "smokescreen", "ssh", "console-filter"}

// fakeProgram runs as the program with this name. It is false for a different name.
func fakeProgram(name string) bool {
	if !slices.Contains(fakeNames, name) {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		syscall.Exit(1)
	}
	dir := filepath.Dir(self)
	file := func(name string) string { return filepath.Join(dir, name) }
	exists := func(name string) bool { _, err := os.Stat(file(name)); return err == nil }
	// The argument after an option.
	option := func(name string) string {
		if i := slices.Index(os.Args, name); i >= 0 && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		return ""
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	// stopped is true when a signal came in this time.
	stopped := func(d time.Duration) bool {
		select {
		case <-stop:
			return true
		case <-time.After(d):
			return false
		}
	}
	listen := func(network, address string) io.Closer {
		var l io.Closer
		var err error
		if network == "unixgram" {
			l, err = net.ListenPacket(network, address)
		} else {
			var tcp net.Listener
			if tcp, err = net.Listen(network, address); err == nil {
				go func() {
					for {
						conn, err := tcp.Accept()
						if err != nil {
							return
						}
						conn.Close()
					}
				}()
				l = tcp
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			syscall.Exit(1)
		}
		return l
	}

	switch name {
	case "console-filter":
		termsafe.Copy(os.Stdout, os.Stdin)

	case "smokescreen":
		for exists("proxy-wait") {
			if stopped(50 * time.Millisecond) {
				return true
			}
		}
		listen("tcp", net.JoinHostPort("127.0.0.1", option("--listen-port")))
		<-stop

	case "gvproxy":
		listen("unixgram", strings.TrimPrefix(option("-listen-vfkit"), "unixgram://"))
		listen("tcp", net.JoinHostPort("127.0.0.1", option("-ssh-port")))
		<-stop

	case "krunkit":
		// The guest runs: the disk has the pid of the hypervisor. The sandbox of the
		// hypervisor permits no other file.
		for _, arg := range os.Args {
			if disk, ok := strings.CutPrefix(arg, "virtio-blk,path="); ok && strings.HasSuffix(disk, "/disk.img,format=raw") {
				f, err := os.OpenFile(strings.TrimSuffix(disk, ",format=raw"), os.O_WRONLY, 0)
				if err != nil {
					fmt.Fprintf(os.Stderr, "krunkit: %v\n", err)
					syscall.Exit(1)
				}
				_, _ = fmt.Fprintf(f, "pid %d\n", os.Getpid())
				f.Close()
			}
		}
		// The output is the console of the guest.
		fmt.Println("[  OK  ] Reached target Multi-User System.")
		for !stopped(50 * time.Millisecond) {
			command, _ := os.ReadFile(file("vm-command"))
			switch strings.TrimSpace(string(command)) {
			case fmt.Sprintf("reboot %d", os.Getpid()):
				fmt.Println("[  OK  ] Reached target System Reboot.")
				return true
			case fmt.Sprintf("poweroff %d", os.Getpid()):
				fmt.Println("[  OK  ] Reached target System Power Off.")
				return true
			}
		}

	case "ssh":
		command := strings.Join(os.Args[1:], " ")
		log, err := os.OpenFile(file("ssh.log"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
		if err != nil {
			syscall.Exit(1)
		}
		// The lock gives each command its line number.
		_ = syscall.Flock(int(log.Fd()), syscall.LOCK_EX)
		text, _ := os.ReadFile(file("ssh.log"))
		line := strings.Count(string(text), "\n") + 1
		_, _ = fmt.Fprintln(log, command)
		log.Close()
		// The guest answers while its hypervisor runs. The options of ssh name a file of
		// the directory of the VM: the configuration, or the key for the installer.
		config := option("-F")
		if config == "/dev/null" {
			config = option("-i")
		}
		var guest int
		disk, _ := os.ReadFile(filepath.Join(filepath.Dir(config), "disk.img"))
		_, _ = fmt.Sscanf(string(disk[:min(len(disk), 32)]), "pid %d", &guest)
		if exists("ssh-down") || guest == 0 || syscall.Kill(guest, 0) != nil {
			syscall.Exit(255)
		}
		if fail, err := os.ReadFile(file("ssh-fail")); err == nil && strings.Contains(command, strings.TrimSpace(string(fail))) {
			syscall.Exit(1)
		}
		switch {
		case slices.Contains(os.Args, "-M"):
			// The connection for the forwards.
			ctl := listen("unix", option("-S"))
			<-stop
			_ = ctl.Close()
		case strings.HasSuffix(command, " true"):
		case strings.Contains(command, "poweroff"):
			if !exists("poweroff-ignored") {
				_ = os.WriteFile(file("vm-command"), fmt.Appendf(nil, "poweroff %d\n", guest), 0o644)
			}
		case strings.Contains(command, "systemctl"):
		default:
			// A command that reads the files of the host.
			input, _ := io.ReadAll(os.Stdin)
			_ = os.WriteFile(file(fmt.Sprintf("stdin-%d", line)), input, 0o644)
			if strings.Contains(command, "install -D") {
				_ = os.WriteFile(file("guest-env"), input, 0o644)
			}
		}
	}
	return true
}
