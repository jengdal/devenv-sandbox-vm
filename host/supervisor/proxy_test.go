package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The proxy refuses this computer and private addresses, and the options of
// services.sandbox-vm.proxy add rules. The test starts the proxy with the configuration
// files that the host module creates for these options, and asks for connections.
//
// The test needs the proxy and the files: SVM_SMOKESCREEN is the program, and
// SVM_TEST_PROXY_CONFIGS is the directory of the files. The file devenv.nix of this repo
// sets them; tests/nix/proxy-configs.nix has the options of each file.
//
// The proxy refuses each connection here before it makes it, or the name does not exist.
// Thus the test makes no connection out of this computer.
func TestProxyRules(t *testing.T) {
	smokescreen, configs := os.Getenv("SVM_SMOKESCREEN"), os.Getenv("SVM_TEST_PROXY_CONFIGS")
	if smokescreen == "" || configs == "" {
		t.Skip("SVM_SMOKESCREEN or SVM_TEST_PROXY_CONFIGS is not set")
	}
	// A program on this computer that the guest must not get a connection to.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	local := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)

	// The status of a connection that the rules refuse, and of a name that the rules
	// permit: the proxy then looks for its address, and a name of .example has none.
	const refused, noAddress = http.StatusProxyAuthRequired, http.StatusBadGateway
	type request struct {
		target string
		status int
		// A part of the cause that the proxy gives.
		cause string
	}
	tests := []struct {
		config   string
		requests []request
	}{
		{"default.yaml", []request{
			{"127.0.0.1:" + local, refused, "Self Connection"},
			{"localhost:" + local, refused, "Self Connection"},
			{"0.0.0.0:" + local, refused, "Not Global Unicast"},
			{"[::1]:" + local, refused, ""},
			{"10.0.0.1:80", refused, "Private Range"},
			{"172.16.0.1:80", refused, "Private Range"},
			{"192.168.1.1:80", refused, "Private Range"},
			// The address of the metadata service of cloud computers.
			{"169.254.169.254:80", refused, "Not Global Unicast"},
			// All names are permitted.
			{"name.example:443", noAddress, ""},
		}},
		{"deny.yaml", []request{
			{"203.0.113.1:80", refused, "User Configured"},
			{"denied.example:443", refused, ""},
			{"127.0.0.1:" + local, refused, "Self Connection"},
			{"name.example:443", noAddress, ""},
		}},
		{"allowed-domains.yaml", []request{
			{"allowed.example:443", noAddress, ""},
			{"other.example:443", refused, "default rule"},
			{"sub.allowed.example:443", refused, "default rule"},
			// An address is not a name of the list.
			{"127.0.0.1:" + local, refused, ""},
			{"10.0.0.1:80", refused, ""},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.config, func(t *testing.T) {
			requests := tt.requests
			if tt.config == "default.yaml" {
				// The other addresses of this computer.
				if addr := lanAddress(t); addr != "" {
					requests = append(requests, request{addr, refused, ""})
				}
				// Other forms of the loopback address. The resolver of macOS reads them as
				// the address. The resolver of Go on Linux reads them as names, which do not
				// exist.
				if runtime.GOOS == "darwin" {
					requests = append(requests,
						request{"2130706433:" + local, refused, "Self Connection"},
						request{"127.1:" + local, refused, "Self Connection"})
				}
			}
			proxy := startTestProxy(t, smokescreen, filepath.Join(configs, tt.config))
			for _, r := range requests {
				status, cause, err := proxyConnect(proxy, r.target)
				if err != nil {
					t.Errorf("%s: %v", r.target, err)
					continue
				}
				if status != r.status || !strings.Contains(cause, r.cause) {
					t.Errorf("%s: status %d (%s), want %d (%s)", r.target, status, cause, r.status, r.cause)
				}
			}
		})
	}
	// No request got to the program on this computer.
	_ = l.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	if conn, err := l.Accept(); err == nil {
		conn.Close()
		t.Error("the proxy made a connection to this computer")
	}
}

// startTestProxy starts the proxy with a configuration file and returns its address. The
// proxy has no sandbox here: the test is for the rules of the proxy itself.
func startTestProxy(t *testing.T, smokescreen, config string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	logFile := filepath.Join(t.TempDir(), "log")
	log, err := os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(smokescreen, "--config-file", config, "--listen-port", strconv.Itoa(port))
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if !waitUntil(context.Background(), 10*time.Second, func() bool { return portInUse(port) }) {
		text, _ := os.ReadFile(logFile)
		t.Fatalf("the proxy did not start:\n%s", text)
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// proxyConnect asks the proxy for a connection to the target, as a program in the guest
// does. It returns the status of the answer and the cause that the proxy gives.
func proxyConnect(proxy, target string) (status int, cause string, err error) {
	conn, err := net.DialTimeout("tcp", proxy, 5*time.Second)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		return 0, "", err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		return 0, "", err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("X-Smokescreen-Error"), nil
}

// lanAddress returns the address of a program that accepts connections on an address of this
// computer that is not the loopback address. It is empty without such an address.
func lanAddress(t *testing.T) string {
	t.Helper()
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ip, ok := a.(*net.IPNet)
		if !ok || ip.IP.To4() == nil || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
			continue
		}
		l, err := net.Listen("tcp", net.JoinHostPort(ip.IP.String(), "0"))
		if err != nil {
			continue
		}
		t.Cleanup(func() { l.Close() })
		// Not each address of this computer accepts connections from this computer, for
		// example the address of a VPN.
		conn, err := net.DialTimeout("tcp", l.Addr().String(), time.Second)
		if err != nil {
			continue
		}
		conn.Close()
		return l.Addr().String()
	}
	return ""
}
