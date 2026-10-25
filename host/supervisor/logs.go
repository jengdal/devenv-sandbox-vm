package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// The logs.
//
// The guest controls how much the console, gvproxy and the proxy write. Thus each log goes
// through a pipe to s6-log, and s6-log limits its size. The directory log/<name> has the
// file `current` and one older file, each of SVM_LOG_BYTES at most.
var logNames = []string{"console", "gvproxy", "smokescreen"}

// openPipe opens a pipe of a log for a program that this program starts.
//
// Go opens a pipe in non-blocking mode, and a program that gets the file continues in that
// mode. Thus this function opens the file directly.
//
// The function opens only a pipe. It does not follow a symbolic link: this program must
// not write to a different file if something put a link in the place of the pipe.
func openPipe(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		syscall.Close(fd)
		return nil, fmt.Errorf("%s is not a pipe", path)
	}
	return os.NewFile(uintptr(fd), path), nil
}

// logWriter opens the pipe of a log for a program that writes to it. It fails if no
// program reads the pipe: s6-log, or console-filter for the console.
func (s *supervisor) logWriter(name string) (*os.File, error) {
	f, err := openPipe(s.cfg.path(name+".fifo"), syscall.O_WRONLY|syscall.O_NONBLOCK)
	if err != nil {
		return nil, err
	}
	if err := syscall.SetNonblock(int(f.Fd()), false); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// startLogs starts s6-log for each log. It reads the bytes of the guest, thus it also has
// a sandbox: no network, and it can write only its own directory. s6-log gets the pipe
// open for reading and writing. Such a pipe does not wait for a program that writes, and
// it does not end when that program stops. Thus s6-log starts before a program writes, and
// continues when gvproxy and the proxy start again. The console has no time in its lines,
// thus s6-log adds it. The console also goes to the output of this program, and it goes
// through console-filter first (startConsole).
func (s *supervisor) startLogs() error {
	c := s.cfg
	for _, n := range logNames {
		dir, fifo := c.path("log", n), c.path(n+".fifo")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if st, err := os.Lstat(fifo); err != nil || st.Mode()&os.ModeNamedPipe == 0 {
			os.Remove(fifo)
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				return &os.PathError{Op: "mkfifo", Path: fifo, Err: err}
			}
		}
		args := []string{"--", "n1", "s" + c.logBytes}
		if n == "console" {
			// 1 also writes each line to stdout, for the output of this program. It is
			// before T: the file gets the time, the output does not.
			args = append(args, "1", "T")
		}
		cmd, err := confinedCommand(confinement{
			egress:     egressNone,
			storePaths: c.s6LogStorePaths,
			writeDirs:  []string{dir},
		}, c.s6Log, append(args, dir)...)
		if err != nil {
			return err
		}
		if n == "console" {
			// Only this log goes to the output of this program, and its text went through
			// console-filter.
			cmd.Stdout = os.Stdout
			if err := s.startConsole(cmd, fifo); err != nil {
				return err
			}
			continue
		}
		in, err := openPipe(fifo, syscall.O_RDWR)
		if err != nil {
			return err
		}
		cmd.Stdin = in
		_, err = startDetached(cmd, c.path("log-"+n+".pid"))
		in.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// startConsole starts s6-log for the console, and console-filter between the pipe of the
// console and s6-log.
//
// The guest controls the bytes of its console, and they go to the file of the log and to
// the terminal of the user. console-filter removes all control characters and escape
// sequences from them. It reads the bytes of the guest, thus it has a sandbox: no network,
// and it can write no file.
//
// console-filter gets the pipe of the console open only for reading, and this program
// keeps the pipe open for writing (consoleKeep). Thus the pipe ends for console-filter when
// the hypervisor stopped and this program closes consoleKeep. console-filter then stops
// after the last byte, and s6-log stops after console-filter. If this program is killed,
// the hypervisor keeps the pipe open, and the console continues.
func (s *supervisor) startConsole(log *exec.Cmd, fifo string) error {
	c := s.cfg
	filter, err := confinedCommand(confinement{
		egress:     egressNone,
		storePaths: c.consoleFilterStorePaths,
	}, c.consoleFilter)
	if err != nil {
		return err
	}
	// Without O_NONBLOCK, the open waits for a program that writes.
	in, err := openPipe(fifo, syscall.O_RDONLY|syscall.O_NONBLOCK)
	if err != nil {
		return err
	}
	defer in.Close()
	if s.consoleKeep, err = openPipe(fifo, syscall.O_WRONLY); err != nil {
		return err
	}
	if err := syscall.SetNonblock(int(in.Fd()), false); err != nil {
		return err
	}
	filtered, out, err := os.Pipe()
	if err != nil {
		return err
	}
	defer filtered.Close()
	defer out.Close()
	log.Stdin = filtered
	if _, err := startDetached(log, c.path("log-console.pid")); err != nil {
		return err
	}
	filter.Stdin, filter.Stdout = in, out
	s.consoleFilter, err = startDetached(filter, c.path("console-filter.pid"))
	return err
}

// stopLogs stops console-filter and s6-log. s6-log stops at the end of a line. The empty
// line ends a line that is not complete.
func (s *supervisor) stopLogs() {
	// The last lines of the console must be in the output before this program stops. Thus
	// console-filter gets the time to read them (startConsole). The signal is for a
	// console-filter of a run that did not stop correctly, or if the hypervisor did not stop.
	if s.consoleKeep != nil {
		s.consoleKeep.Close()
		s.consoleKeep = nil
		if s.consoleFilter != nil {
			select {
			case <-s.consoleFilter.done:
			case <-time.After(stopGrace):
			}
		}
	}
	stopPid(s.cfg.path("console-filter.pid"), "console-filter", nil)
	for _, n := range logNames {
		fifo := s.cfg.path(n + ".fifo")
		stopPid(s.cfg.path("log-"+n+".pid"), "s6-log", func() {
			if f, err := openPipe(fifo, syscall.O_RDWR|syscall.O_NONBLOCK); err == nil {
				_, _ = f.Write([]byte("\n"))
				f.Close()
			}
		})
	}
}

// bootMark is the line that this program writes to the log of the console before each boot.
const bootMark = "[sandbox-vm] boot"

// markBoot writes bootMark to the log of the console.
func (s *supervisor) markBoot() error {
	f, err := s.logWriter("console")
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(bootMark + "\n")
	return err
}

// The lines on the console that show how the guest stops. systemd writes the lines of its
// targets. With krunkit, the messages of the kernel do not go to the console, but a
// different guest can write them.
var (
	consoleReboot = [][]byte{
		[]byte("Reached target System Reboot"),
		[]byte("reboot: Restarting system"),
	}
	consolePowerOff = [][]byte{
		[]byte("Reached target System Power Off"),
		[]byte("Reached target System Halt"),
		[]byte("reboot: Power down"),
		[]byte("reboot: System halted"),
	}
)

// consoleSaysReboot checks the end of the log of the console after the hypervisor
// stopped. decided is true if the guest wrote in this boot how it stops, and reboot is true
// if it wrote the line of a reboot. A line before bootMark is from an earlier boot.
func consoleSaysReboot(tail []byte) (decided, reboot bool) {
	last := -1
	for _, line := range consoleReboot {
		if i := bytes.LastIndex(tail, line); i > last {
			last, reboot = i, true
		}
	}
	for _, line := range consolePowerOff {
		if i := bytes.LastIndex(tail, line); i > last {
			last, reboot = i, false
		}
	}
	if last < bytes.LastIndex(tail, []byte(bootMark)) {
		return false, false
	}
	return last >= 0, reboot
}

// guestRebooted is true if the hypervisor stopped because the guest rebooted. The
// hypervisor stops in the same way when the guest powers off, thus the console of the
// guest gives the answer. s6-log can write the last line a moment after the hypervisor
// stops. Without a last line, for example after a failure of the hypervisor, it is false.
//
// The guest controls its console. That gives it nothing new: it can already reboot and
// power off.
func (s *supervisor) guestRebooted() bool {
	var reboot bool
	waitUntil(context.Background(), 2*time.Second, func() bool {
		var decided bool
		decided, reboot = consoleSaysReboot(s.consoleTail())
		return decided
	})
	return reboot
}

// consoleTail reads the end of the log of the console. If it cannot go to the end, it reads
// the full log.
func (s *supervisor) consoleTail() []byte {
	f, err := os.Open(s.cfg.path("log", "console", "current"))
	if err != nil {
		return nil
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 16<<10 {
		_, _ = f.Seek(-16<<10, io.SeekEnd)
	}
	tail, _ := io.ReadAll(f)
	return tail
}
