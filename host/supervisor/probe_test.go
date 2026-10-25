package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// probe is the test binary started as `probe OPERATION=ARGUMENT...`. The tests of
// the sandbox rules start it in a sandbox. For each operation it writes one line: the
// operation in quotation marks and "ok", or the operation and the error.
//
//	read=FILE      reads the file
//	list=DIR       lists the directory
//	write=FILE     creates the file
//	append=FILE    writes data to a file that exists
//	remove=FILE    removes the file
//	symlink=FILE   puts a symbolic link in the place of the file
//	connect=PORT   connects to the port of the loopback address
//	dial=ADDRESS   connects to the address (host:port)
//	listen=PORT    binds the port of the loopback address
//	unix-listen=FILE, unix-connect=FILE
//	               the same for a unix socket
//	exec=PROGRAM   starts the program, with the argument "probe": a copy of the test
//	               binary then does no operation and exits
//	env=NAME       the variable is in the environment
func probe(ops []string) {
	for _, op := range ops {
		name, arg, _ := strings.Cut(op, "=")
		var err error
		switch name {
		case "read":
			_, err = os.ReadFile(arg)
		case "list":
			_, err = os.ReadDir(arg)
		case "write":
			err = os.WriteFile(arg, []byte("x"), 0o644)
		case "append":
			var f *os.File
			if f, err = os.OpenFile(arg, os.O_WRONLY|os.O_APPEND, 0); err == nil {
				_, err = f.WriteString("x")
				f.Close()
			}
		case "remove":
			err = os.Remove(arg)
		case "symlink":
			if err = os.Rename(arg, arg+".old"); err == nil {
				err = os.Symlink("/etc/hosts", arg)
			}
		case "connect", "dial":
			if name == "connect" {
				arg = net.JoinHostPort("127.0.0.1", arg)
			}
			var conn net.Conn
			if conn, err = net.DialTimeout("tcp", arg, 2*time.Second); err == nil {
				conn.Close()
			}
		case "listen", "unix-listen":
			network := "unix"
			if name == "listen" {
				network, arg = "tcp", net.JoinHostPort("127.0.0.1", arg)
			}
			var l net.Listener
			if l, err = net.Listen(network, arg); err == nil {
				l.Close()
			}
		case "unix-connect":
			var conn net.Conn
			if conn, err = net.DialTimeout("unix", arg, 2*time.Second); err == nil {
				conn.Close()
			}
		case "exec":
			err = exec.Command(arg, "probe").Run()
		case "env":
			if _, ok := os.LookupEnv(arg); !ok {
				err = fmt.Errorf("not set")
			}
		default:
			err = fmt.Errorf("unknown operation")
		}
		if err == nil {
			fmt.Printf("%q ok\n", op)
		} else {
			fmt.Printf("%q %q\n", op, err.Error())
		}
	}
}
