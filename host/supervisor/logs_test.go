package main

import "testing"

func TestConsoleSaysReboot(t *testing.T) {
	const (
		mark     = "2026-10-12 19:45:25.100000000  [sandbox-vm] boot\n"
		status   = "2026-10-12 19:45:36.358738500  [  OK  ] Reached target Multi-User System.\n"
		restart  = "2026-10-12 20:05:01.000000000  [ 1175.123456] reboot: Restarting system\n"
		powerOff = "2026-10-12 20:05:01.000000000  [ 1175.123456] reboot: Power down\n"
		halted   = "2026-10-12 20:05:01.000000000  [ 1175.123456] reboot: System halted\n"
		// The lines of krunkit 1.3.2 with NixOS 26.05, as console-filter writes them.
		sdReboot   = "2026-10-12 21:08:58.485906500  [  OK  ] Reached target System Reboot.\n"
		sdPowerOff = "2026-10-12 21:08:58.485906500  [  OK  ] Reached target System Power Off.\n"
		sdHalt     = "2026-10-12 21:08:58.485906500  [  OK  ] Reached target System Halt.\n"
		sdEnd      = "2026-10-12 21:09:02.172038500  \n"
	)
	for _, tc := range []struct {
		name            string
		tail            string
		decided, reboot bool
	}{
		{"reboot", mark + status + restart, true, true},
		{"power off", mark + status + powerOff, true, false},
		{"halt", mark + status + halted, true, false},
		{"reboot, mark not in the tail", status + restart, true, true},
		{"last line not written yet", mark + status, false, false},
		{"empty", "", false, false},
		{"reboot of an earlier boot", mark + status + restart + mark, false, false},
		{"reboot of an earlier boot, then output", mark + restart + mark + status, false, false},
		{"power off after a reboot", mark + restart + mark + status + powerOff, true, false},
		{"reboot after the installer stopped", mark + powerOff + mark + status + restart, true, true},
		{"line that is cut", restart[40:] + mark + status, false, false},
		{"systemd reboot", mark + status + sdReboot + sdEnd, true, true},
		{"systemd power off", mark + status + sdPowerOff + sdEnd, true, false},
		{"systemd halt", mark + status + sdHalt + sdEnd, true, false},
		{"systemd reboot of an earlier boot", mark + sdReboot + sdEnd + mark + status, false, false},
		{"systemd power off after a reboot", mark + sdReboot + mark + status + sdPowerOff, true, false},
	} {
		decided, reboot := consoleSaysReboot([]byte(tc.tail))
		if decided != tc.decided || reboot != tc.reboot {
			t.Errorf("%s: got decided=%v reboot=%v, want decided=%v reboot=%v",
				tc.name, decided, reboot, tc.decided, tc.reboot)
		}
	}
}
