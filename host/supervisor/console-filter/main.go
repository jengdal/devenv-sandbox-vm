// console-filter copies the console of the guest from its input to its output, and removes
// what a terminal must not get (internal/termsafe).
//
// The guest controls these bytes. They go to the log of the console and to the terminal of
// the user.
//
// It is a program of its own, and not a part of the sandbox-vm process: it runs in a
// sandbox, and it has its own name for the pid file. The shell scripts of host/scripts also
// use it, for the output of the commands of the guest, with -lines: a carriage return
// replaces the line (termsafe.Filter.Lines). The console does not use -lines.
package main

import (
	"os"

	"sandbox-vm/internal/termsafe"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-lines" {
		termsafe.CopyLines(os.Stdout, os.Stdin)
		return
	}
	termsafe.Copy(os.Stdout, os.Stdin)
}
