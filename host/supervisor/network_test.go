package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSSHConfig(t *testing.T) {
	text := sshConfig("vm1", 22007, "anna", "/home/u/.devenv-sandbox-vm/vm1")
	want := `Host vm1
  HostName 127.0.0.1
  Port 22007
  User anna
  IdentityFile "/home/u/.devenv-sandbox-vm/vm1/id_ed25519"
  IdentitiesOnly yes
  ForwardAgent no
  ForwardX11 no
  ForwardX11Trusted no
  PermitLocalCommand no
  HostKeyAlias vm1
  UserKnownHostsFile "/home/u/.devenv-sandbox-vm/vm1/known_hosts"
  StrictHostKeyChecking accept-new
`
	if text != want {
		t.Errorf("ssh.config:\n%s", text)
	}
	if got := sshConfigPorts(text); !reflect.DeepEqual(got, []int{22007}) {
		t.Errorf("ports: %v", got)
	}
	if got := sshConfigPorts("Host a\nPortal 1\n Port x\n"); got != nil {
		t.Errorf("ports: %v", got)
	}
}

func TestPortCandidates(t *testing.T) {
	// The port of the last run is first.
	got := portCandidates([]int{22500}, []int{22000, 22002}, sshPortFirst, sshPortLast)
	if want := []int{22500, 22001, 22003}; !reflect.DeepEqual(got[:3], want) {
		t.Errorf("candidates: %v, want %v", got[:3], want)
	}
	// But not if a different VM has it now.
	got = portCandidates([]int{22500}, []int{22500}, sshPortFirst, sshPortLast)
	if got[0] != 22000 || len(got) != 999 {
		t.Errorf("first candidate %d of %d", got[0], len(got))
	}
	// The range of the proxy.
	got = portCandidates(nil, []int{23000}, proxyPortFirst, proxyPortLast)
	if got[0] != 23001 || len(got) != 999 {
		t.Errorf("first candidate %d of %d", got[0], len(got))
	}
}

// A VM keeps its ports, and no other VM uses them.
func TestRecordedPorts(t *testing.T) {
	home := t.TempDir()
	s := &supervisor{cfg: &config{home: home, dir: filepath.Join(home, ".devenv-sandbox-vm", "vm1")}}
	for vm, port := range map[string]int{"vm1": 23004, "vm2": 23000, "vm3": 23001} {
		dir := filepath.Join(home, ".devenv-sandbox-vm", vm)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		// The text that startNetwork writes.
		env := fmt.Sprintf("hostname=\"h\"\nuser=\"u\"\nproxy_port=\"%d\"\ntimezone=\"Europe/Helsinki\"\n", port)
		if err := os.WriteFile(filepath.Join(dir, "env"), []byte(env), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	last, taken := s.recordedPorts("env", envProxyPorts)
	sort.Ints(taken)
	if !reflect.DeepEqual(last, []int{23004}) || !reflect.DeepEqual(taken, []int{23000, 23001}) {
		t.Errorf("last %v, taken %v", last, taken)
	}
	got := portCandidates(last, taken, proxyPortFirst, proxyPortLast)
	if want := []int{23004, 23002, 23003}; !reflect.DeepEqual(got[:3], want) {
		t.Errorf("candidates: %v, want %v", got[:3], want)
	}
	if ports := envProxyPorts("proxy_port=\"x\"\nxproxy_port=\"1\"\n"); ports != nil {
		t.Errorf("ports of a text without a port: %v", ports)
	}
}

// freePort returns a port that no program uses at this moment.
func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// No other program must get the port of a proxy that stopped.
func TestWatchNetworkProxy(t *testing.T) {
	newChild := func() *child { return &child{done: make(chan struct{})} }
	s := &supervisor{cfg: &config{dir: t.TempDir()}, proxyPort: freePort(t), proxy: newChild(), gvproxy: newChild()}
	vm := newChild()
	watched := make(chan struct{})
	go func() {
		s.watchNetwork(context.Background(), vm)
		close(watched)
	}()
	if portInUse(s.proxyPort) {
		t.Fatal("the port is in use while the proxy runs")
	}
	close(s.proxy.done)
	if !waitUntil(context.Background(), 5*time.Second, func() bool { return portInUse(s.proxyPort) }) {
		t.Error("the port of a proxy that stopped is free")
	}
	// The watch ends with the hypervisor, and the port is free for the subsequent boot.
	close(vm.done)
	<-watched
	s.releaseProxyPort()
	if portInUse(s.proxyPort) {
		t.Error("the port is in use after the release")
	}
}

// A different program has the port of the proxy: the function does not hold the port.
func TestHoldProxyPortTaken(t *testing.T) {
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer other.Close()
	s := &supervisor{cfg: &config{dir: t.TempDir()}, proxyPort: other.Addr().(*net.TCPAddr).Port}
	if s.holdProxyPort() || s.proxyHold != nil {
		t.Error("the function holds a port that a different program has")
	}
}

// gvproxy stops when its hypervisor stops. The watch ends then, and also with its context.
func TestWatchNetworkEnds(t *testing.T) {
	old := gvproxyGrace
	gvproxyGrace = time.Minute
	defer func() { gvproxyGrace = old }()
	newChild := func() *child { return &child{done: make(chan struct{})} }

	s := &supervisor{cfg: &config{dir: t.TempDir()}, proxy: newChild(), gvproxy: newChild()}
	vm := newChild()
	watched := make(chan struct{})
	go func() {
		s.watchNetwork(context.Background(), vm)
		close(watched)
	}()
	close(s.gvproxy.done)
	close(vm.done)
	select {
	case <-watched:
	case <-time.After(5 * time.Second):
		t.Error("the watch continues after the hypervisor stopped")
	}

	s = &supervisor{cfg: &config{dir: t.TempDir()}, proxy: newChild(), gvproxy: newChild()}
	ctx, cancel := context.WithCancel(context.Background())
	watched = make(chan struct{})
	go func() {
		s.watchNetwork(ctx, newChild())
		close(watched)
	}()
	cancel()
	select {
	case <-watched:
	case <-time.After(5 * time.Second):
		t.Error("the watch continues after its context ended")
	}
}

func TestPortInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if !portInUse(port) {
		t.Error("the port of a listener is not in use")
	}
	l.Close()
	if portInUse(port) {
		t.Error("the port is in use after the listener stopped")
	}
}

func TestTimeZone(t *testing.T) {
	for _, tc := range []struct{ tz, link, want string }{
		{"", "/var/db/timezone/zoneinfo/Europe/Helsinki", "Europe/Helsinki"},
		{"", "/etc/zoneinfo/America/Argentina/Buenos_Aires", "America/Argentina/Buenos_Aires"},
		{"", "../usr/share/zoneinfo/UTC", "UTC"},
		{"", "/usr/share/zoneinfo/Etc/GMT+2", "Etc/GMT+2"},
		{"Asia/Tokyo", "/var/db/timezone/zoneinfo/Europe/Helsinki", "Asia/Tokyo"},
		{":Asia/Tokyo", "", "Asia/Tokyo"},
		{"", "", ""},
		{"", "/etc/localtime.real", ""},
		{"", "/usr/share/zoneinfo/", ""},
		{"", "/usr/share/zoneinfo/Europe/Hel sinki", ""},
		{"", "/usr/share/zoneinfo/x\";reboot;\"", ""},
		{"$(reboot)", "/usr/share/zoneinfo/UTC", ""},
		{"", "/usr/share/zoneinfo/../../etc/passwd", ""},
	} {
		if got := timeZone(tc.tz, tc.link); got != tc.want {
			t.Errorf("timeZone(%q, %q) = %q, want %q", tc.tz, tc.link, got, tc.want)
		}
	}
}

// A home directory can have a space in its name.
func TestSSHConfigQuotesPaths(t *testing.T) {
	text := sshConfig("vm1", 22007, "anna", "/Users/Anna Smith/.devenv-sandbox-vm/vm1")
	for _, line := range []string{
		`  IdentityFile "/Users/Anna Smith/.devenv-sandbox-vm/vm1/id_ed25519"`,
		`  UserKnownHostsFile "/Users/Anna Smith/.devenv-sandbox-vm/vm1/known_hosts"`,
	} {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("no line %s:\n%s", line, text)
		}
	}
}
