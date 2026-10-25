package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseConfineArgs(t *testing.T) {
	c, argv, err := parseConfineArgs([]string{
		"--egress", "none", "--store-paths", "paths", "--exec-closure",
		"--write-dir", "/a", "--write-dir", "/b",
		"--env", "A", "--env", "B", "--", "borg", "--", "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := confinement{
		storePaths: "paths", execClosure: true, writeDirs: []string{"/a", "/b"},
		egress: "none", env: []string{"A", "B"},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("confinement: %+v", c)
	}
	if !reflect.DeepEqual(argv, []string{"borg", "--", "x"}) {
		t.Errorf("command: %q", argv)
	}

	for _, args := range [][]string{
		{"--egress", "none", "borg"},
		{"--egress", "none", "--"},
		{"--egress"},
		{"--other", "x", "--", "borg"},
		{"--listen", "1", "--", "borg"},
	} {
		if _, _, err := parseConfineArgs(args); err == nil {
			t.Errorf("%q gave no error", args)
		}
	}
}

// A program in a sandbox does not get the passwords and keys of the shell of the project.
func TestConfinedEnv(t *testing.T) {
	environ := []string{
		"SECRET_KEY=1", "TZ=Europe/Helsinki", "SSH_AUTH_SOCK=/s", "LANG=C", "LC_ALL=C",
		"TMPDIR=/t", "BORG_BASE_DIR=/b", "BORG_PASSPHRASE=p", "TZX=1", "PATH=/bin",
	}
	got := confinedEnv(environ, []string{"BORG_BASE_DIR"})
	want := []string{"TZ=Europe/Helsinki", "LANG=C", "LC_ALL=C", "TMPDIR=/t", "BORG_BASE_DIR=/b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("environment: %q", got)
	}
	// For exec.Cmd, nil is the environment of this program.
	if got := confinedEnv([]string{"A=1"}, nil); got == nil || len(got) != 0 {
		t.Errorf("environment: %#v", got)
	}
}

// The arguments come from the shell scripts. Each list of arguments gives an error or a
// program to start. To look for new inputs: go test -fuzz FuzzParseConfineArgs .
func FuzzParseConfineArgs(f *testing.F) {
	for _, seed := range []string{
		"--egress\nnone\n--\nprog", "--exec-closure\n--\nprog\narg", "--", "--egress", "prog",
		"--write-dir\n--\n--\nprog", "--env\nA\n--env\nB\n--store-paths\n1\n--\n--\n--",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, lines string) {
		args := strings.Split(lines, "\n")
		c, argv, err := parseConfineArgs(args)
		if (err == nil) != (len(argv) > 0) {
			t.Fatalf("%q: the program %q and the error %v", args, argv, err)
		}
		if err != nil {
			return
		}
		// The program and its arguments are the end of the list, after a "--".
		at := len(args) - len(argv)
		if at < 1 || args[at-1] != "--" || strings.Join(argv, "\n") != strings.Join(args[at:], "\n") {
			t.Fatalf("%q: the program %q", args, argv)
		}
		// Each option before that is in the confinement.
		values := len(c.writeDirs) + len(c.env)
		if values > at {
			t.Fatalf("%q: %d values of options", args, values)
		}
	})
}
