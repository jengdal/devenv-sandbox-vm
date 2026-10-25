// The supervisor: the program of the sandbox-vm process (services.sandbox-vm of devenv).
//
//	sandbox-vm run
//	    Creates the VM if it does not exist, starts it and runs it until the process
//	    gets a signal or the guest powers off. It boots a guest again that reboots.
//	    run.go has the sequence.
//	sandbox-vm confine [OPTION]... -- PROGRAM [ARGUMENT]...
//	    Starts a program in a sandbox. The shell scripts of host/scripts use it.
//	    confine.go describes the options.
//	sandbox-vm detach [--quiet] PIDFILE PROGRAM [ARGUMENT]...
//	    Starts a program in its own session and stops. Only `run` uses it (proc.go).
//	sandbox-vm exec-sandbox PROFILE PROGRAM [ARGUMENT]...
//	    Starts a program in a sandbox whose rules contain the pid of the program. Only
//	    `run` uses it, for the hypervisor (the platform file).
//
// host/scripts.nix gives the options of services.sandbox-vm in environment variables.
// host/scripts/lib.sh describes them.
//
// The files platform_*.go contain the parts that depend on the operating system of the
// host: the hypervisor and the sandbox. No other file names these programs.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"sandbox-vm/internal/termsafe"
)

// The guest cannot write a line of its console or of the output of its commands that
// starts with this text (internal/termsafe).
const prefix = termsafe.Mark + " "

// say writes a message for the user to stdout.
func say(format string, a ...any) {
	_, _ = fmt.Fprintf(os.Stdout, prefix+format+"\n", a...)
}

// warn writes a message for the user to stderr.
func warn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, prefix+format+"\n", a...)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: sandbox-vm run | confine [OPTION]... -- PROGRAM [ARGUMENT]... | detach ... | exec-sandbox ...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		cfg, err := loadConfig(os.Getenv)
		if err == nil {
			err = run(cfg)
		}
		if err != nil {
			// A signal stopped the start. That needs no message.
			if !errors.Is(err, context.Canceled) {
				warn("%v", err)
			}
			os.Exit(1)
		}
	case "detach":
		if err := detach(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sandbox-vm detach: %v\n", err)
			os.Exit(1)
		}
	case "confine":
		c, argv, err := parseConfineArgs(os.Args[2:])
		if err == nil {
			argv, err = confinedArgv(c, argv)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "sandbox-vm confine: %v\n", err)
			os.Exit(2)
		}
		err = syscall.Exec(argv[0], argv, confinedEnv(os.Environ(), c.env))
		fmt.Fprintf(os.Stderr, "sandbox-vm confine: %s: %v\n", argv[0], err)
		os.Exit(1)
	case "exec-sandbox":
		err := execSandbox(os.Args[2:])
		fmt.Fprintf(os.Stderr, "sandbox-vm exec-sandbox: %v\n", err)
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "sandbox-vm: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}
