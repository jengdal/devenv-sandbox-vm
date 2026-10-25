package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// decode returns the name and the value of the credential in an OEM string.
func decode(t *testing.T, oem string) (name, value string) {
	t.Helper()
	rest, ok := strings.CutPrefix(oem, "io.systemd.credential.binary:")
	name, encoded, found := strings.Cut(rest, "=")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if !ok || !found || err != nil {
		t.Fatalf("not a credential: %q", oem)
	}
	return name, string(raw)
}

func TestInstallerCredentials(t *testing.T) {
	creds := installerCredentials([]byte("ssh-ed25519 AAAA vm\n"), []byte("-----BEGIN KEY-----\nabc\n"))
	if len(creds) != 2 {
		t.Fatalf("credentials: %q", creds)
	}
	if name, value := decode(t, creds[0]); name != "ssh.authorized_keys.root" || value != "ssh-ed25519 AAAA vm\n" {
		t.Errorf("key: %q %q", name, value)
	}
	// systemd-tmpfiles decodes the argument of a line with `~`.
	name, line := decode(t, creds[1])
	hostKey := base64.StdEncoding.EncodeToString([]byte("-----BEGIN KEY-----\nabc\n"))
	if want := "f~ /etc/ssh/ssh_host_ed25519_key 0600 root root - " + hostKey + "\n"; name != "tmpfiles.extra" || line != want {
		t.Errorf("host key: %q %q", name, line)
	}
}

func TestBuilderSystems(t *testing.T) {
	files := map[string]string{
		"/etc/nix/machines": "# builders\nssh-ng://builder aarch64-linux,x86_64-linux - 8 1 kvm - -\n\nssh://other riscv64-linux\n",
	}
	readFile := func(name string) ([]byte, error) {
		if text, ok := files[name]; ok {
			return []byte(text), nil
		}
		return nil, errors.New("no file")
	}
	for builders, want := range map[string][]string{
		"":                   nil,
		"@/etc/nix/machines": {"aarch64-linux", "x86_64-linux", "riscv64-linux"},
		"@/etc/nix/none":     nil,
		"ssh://a x86_64-linux ; ssh://b aarch64-linux /key 4": {"x86_64-linux", "aarch64-linux"},
		"ssh://only-a-name": nil,
	} {
		if got := builderSystems(builders, readFile); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q, want %q", builders, got, want)
		}
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 3<<20+5)
	copy(data, "start")
	copy(data[len(data)-3:], "end")
	if err := os.WriteFile(dir+"/from", data, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(context.Background(), dir+"/from", dir+"/to"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dir + "/to"); err != nil || !bytes.Equal(got, data) {
		t.Errorf("the copy is different: %d bytes, %v", len(got), err)
	}
	if err := copyFile(context.Background(), dir+"/from", dir+"/to"); err == nil {
		t.Error("a copy to a file that exists gave no error")
	}
}
