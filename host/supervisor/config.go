package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// config has the options of services.sandbox-vm. host/scripts/lib.sh describes the
// environment variables.
type config struct {
	name, hostname string
	// The login name in the guest. Empty: the user name on the host.
	user string
	// The repo on the host, and the flake of the guest in it.
	root, configDir, configName string

	cpus, memoryMiB string
	diskGB          int64
	// The size limit of one log file.
	logBytes string

	// Values that the guest module also has (constants.nix).
	guestMarker, guestState, guestMAC, proxyGateway, timesyncPort string

	// The installer ISO that a new VM boots (services.sandbox-vm.installer.iso). Empty: the
	// host builds a disk image of the system on a Linux builder.
	installerISO string
	// The architecture of the guest, as `uname -m` gives it.
	guestArch string
	// The binary caches for the build of the disk image, as lists with spaces.
	substituters, trustedPublicKeys string
	// Options for nixos-install, as shell words.
	nixOptions string

	// The programs that run in a sandbox. For each one, a file lists the store paths
	// that it can read.
	smokescreen, smokescreenConfig, smokescreenStorePaths string
	gvproxy, gvproxyStorePaths                            string
	s6Log, s6LogStorePaths                                string
	consoleFilter, consoleFilterStorePaths                string

	// The hypervisor. It runs in a sandbox.
	krunkit, krunkitStorePaths string

	// The program that applies the forwards (host/scripts/ports.sh).
	ports string
	// The program that applies the home-manager configuration (host/scripts/home.sh). Empty:
	// services.sandbox-vm.homeManager is not set.
	homeProgram string

	home string
	// The directory where the host keeps all data for this VM.
	dir string
}

// loadConfig reads the configuration from the environment. The error names each variable
// that is not set.
func loadConfig(getenv func(string) string) (*config, error) {
	var missing []string
	need := func(name string) string {
		v := getenv(name)
		if v == "" {
			missing = append(missing, name)
		}
		return v
	}
	c := &config{
		name:       need("SVM_NAME"),
		hostname:   need("SVM_HOSTNAME"),
		user:       getenv("SVM_USER"),
		root:       need("SVM_ROOT"),
		configDir:  need("SVM_CONFIG_DIR"),
		configName: need("SVM_CONFIG_NAME"),

		cpus:      need("SVM_CPUS"),
		memoryMiB: need("SVM_MEMORY_MIB"),
		logBytes:  need("SVM_LOG_BYTES"),

		guestMarker:  need("SVM_GUEST_MARKER"),
		guestState:   need("SVM_GUEST_STATE"),
		guestMAC:     need("SVM_GUEST_MAC"),
		proxyGateway: need("SVM_PROXY_GATEWAY"),
		timesyncPort: need("SVM_TIMESYNC_PORT"),

		installerISO:      getenv("SVM_INSTALLER_ISO"),
		guestArch:         need("SVM_GUEST_ARCH"),
		substituters:      getenv("SVM_SUBSTITUTERS"),
		trustedPublicKeys: getenv("SVM_TRUSTED_PUBLIC_KEYS"),
		nixOptions:        getenv("SVM_NIX_OPTIONS"),

		smokescreen:             need("SVM_SMOKESCREEN"),
		smokescreenConfig:       need("SVM_SMOKESCREEN_CONFIG"),
		smokescreenStorePaths:   need("SVM_SMOKESCREEN_STORE_PATHS"),
		gvproxy:                 need("SVM_GVPROXY"),
		gvproxyStorePaths:       need("SVM_GVPROXY_STORE_PATHS"),
		s6Log:                   need("SVM_S6_LOG"),
		s6LogStorePaths:         need("SVM_S6_LOG_STORE_PATHS"),
		consoleFilter:           need("SVM_CONSOLE_FILTER"),
		consoleFilterStorePaths: need("SVM_CONSOLE_FILTER_STORE_PATHS"),
		krunkit:                 need("SVM_KRUNKIT"),
		krunkitStorePaths:       need("SVM_KRUNKIT_STORE_PATHS"),

		ports:       need("SVM_PORTS"),
		homeProgram: getenv("SVM_HOME_PROGRAM"),
		home:        need("HOME"),
	}
	disk := need("SVM_DISK_GB")
	if len(missing) > 0 {
		return nil, fmt.Errorf("these variables are not set: %s", strings.Join(missing, " "))
	}
	var err error
	if c.diskGB, err = strconv.ParseInt(disk, 10, 64); err != nil || c.diskGB <= 0 {
		return nil, fmt.Errorf("SVM_DISK_GB is not a size: %q", disk)
	}
	c.dir = filepath.Join(c.home, ".devenv-sandbox-vm", c.name)
	if rest, ok := strings.CutPrefix(c.installerISO, "~/"); ok {
		c.installerISO = filepath.Join(c.home, rest)
	}
	if c.installerISO != "" && !filepath.IsAbs(c.installerISO) {
		return nil, fmt.Errorf("SVM_INSTALLER_ISO is not an absolute path: %q", c.installerISO)
	}
	return c, nil
}

// path returns the path of a file in the directory of the VM.
func (c *config) path(elem ...string) string {
	return filepath.Join(append([]string{c.dir}, elem...)...)
}
