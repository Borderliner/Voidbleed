// Package config describes one Voidbleed installation: every choice the TUI
// collects, or that an unattended install reads from a TOML file.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

type Filesystem string

const (
	Ext4  Filesystem = "ext4"
	Btrfs Filesystem = "btrfs"
)

type SwapMode string

const (
	SwapNone      SwapMode = "none"
	SwapZram      SwapMode = "zram"
	SwapFile      SwapMode = "file"
	SwapPartition SwapMode = "partition"
)

type Source string

const (
	// SourceOffline copies the running live system to the target.
	SourceOffline Source = "offline"
	// SourceNetwork installs current packages from the repositories.
	SourceNetwork Source = "network"
)

type Config struct {
	System  System  `toml:"system"`
	Disk    Disk    `toml:"disk"`
	Swap    Swap    `toml:"swap"`
	User    User    `toml:"user"`
	Install Install `toml:"install"`
}

type System struct {
	Hostname string `toml:"hostname"`
	Timezone string `toml:"timezone"`
	Locale   string `toml:"locale"`
	// Keymap is the console (kbd) keymap.
	Keymap string `toml:"keymap"`
	// KeyboardLayouts are xkb layouts for niri and the greeter, e.g. ["us", "ir"].
	KeyboardLayouts []string `toml:"keyboard_layouts"`
}

type Disk struct {
	// Device is the whole disk to erase, e.g. /dev/nvme0n1.
	Device     string     `toml:"device"`
	Filesystem Filesystem `toml:"filesystem"`
	Encrypt    bool       `toml:"encrypt"`
	Passphrase string     `toml:"passphrase"`
}

type Swap struct {
	Mode    SwapMode `toml:"mode"`
	SizeMiB int      `toml:"size_mib"`
}

type User struct {
	Name     string `toml:"name"`
	FullName string `toml:"full_name"`
	Password string `toml:"password"`
	// RootPassword empty means the root account stays locked (use sudo).
	RootPassword string `toml:"root_password"`
}

type Install struct {
	Source Source `toml:"source"`
	// Groups are optional catalog group ids to install. Nil means "use the
	// catalog defaults for this hardware"; an empty list means none.
	Groups []string `toml:"groups"`
	Mirror string   `toml:"mirror"`
}

// Default returns a config with every non-personal field filled in.
func Default() Config {
	return Config{
		System: System{
			Hostname:        "voidbleed",
			Timezone:        "UTC",
			Locale:          "en_US.UTF-8",
			Keymap:          "us",
			KeyboardLayouts: []string{"us"},
		},
		Disk: Disk{Filesystem: Ext4},
		Swap: Swap{Mode: SwapZram},
		Install: Install{
			Source: SourceOffline,
			Mirror: "https://repo-default.voidlinux.org",
		},
	}
}

// Load reads a TOML file on top of Default. Unknown keys are an error so a
// typo can't silently fall back to a default on a destructive operation.
func Load(path string) (Config, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return cfg, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return cfg, fmt.Errorf("%s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	return cfg, nil
}

var (
	hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	usernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	layoutRe   = regexp.MustCompile(`^[a-z]{2,3}(\([a-z0-9_-]+\))?$`)
	reserved   = map[string]bool{
		"root": true, "anon": true, "daemon": true, "bin": true, "sys": true,
		"nobody": true, "_greeter": true, "polkitd": true,
	}
)

// Validate checks the config on its own. Checks that need the machine (does
// the disk exist, is it big enough) live in the engine's preflight step.
func (c Config) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if !hostnameRe.MatchString(c.System.Hostname) {
		add("hostname %q: use lowercase letters, digits and hyphens", c.System.Hostname)
	}
	if c.System.Timezone == "" || strings.Contains(c.System.Timezone, "..") {
		add("timezone %q is not valid", c.System.Timezone)
	} else if _, err := os.Stat("/usr/share/zoneinfo/" + c.System.Timezone); err != nil {
		add("timezone %q not found in /usr/share/zoneinfo", c.System.Timezone)
	}
	if !strings.Contains(c.System.Locale, ".") {
		add("locale %q should look like en_US.UTF-8", c.System.Locale)
	}
	if c.System.Keymap == "" {
		add("console keymap is empty")
	}
	if len(c.System.KeyboardLayouts) == 0 {
		add("at least one keyboard layout is required")
	}
	for _, l := range c.System.KeyboardLayouts {
		if !layoutRe.MatchString(l) {
			add("keyboard layout %q is not valid", l)
		}
	}

	if !strings.HasPrefix(c.Disk.Device, "/dev/") {
		add("disk device %q must be a /dev path", c.Disk.Device)
	}
	switch c.Disk.Filesystem {
	case Ext4, Btrfs:
	default:
		add("filesystem %q: choose ext4 or btrfs", c.Disk.Filesystem)
	}
	if c.Disk.Encrypt && len(c.Disk.Passphrase) < 8 {
		add("encryption passphrase must be at least 8 characters")
	}

	switch c.Swap.Mode {
	case SwapNone, SwapZram:
	case SwapFile, SwapPartition:
		if c.Swap.SizeMiB < 256 {
			add("swap size must be at least 256 MiB")
		}
		if c.Swap.Mode == SwapPartition && c.Disk.Encrypt {
			add("a swap partition would sit outside the encrypted volume; use a swap file or zram")
		}
	default:
		add("swap mode %q: choose none, zram, file or partition", c.Swap.Mode)
	}

	if !usernameRe.MatchString(c.User.Name) {
		add("username %q: start with a letter, use lowercase letters, digits, - and _", c.User.Name)
	} else if reserved[c.User.Name] {
		add("username %q is reserved", c.User.Name)
	}
	if c.User.Password == "" {
		add("user password is empty")
	}
	if strings.ContainsAny(c.User.FullName, ":\n") {
		add("full name cannot contain ':' or newlines")
	}

	switch c.Install.Source {
	case SourceOffline, SourceNetwork:
	default:
		add("install source %q: choose offline or network", c.Install.Source)
	}
	if !strings.HasPrefix(c.Install.Mirror, "https://") && !strings.HasPrefix(c.Install.Mirror, "http://") {
		add("mirror %q must be an http(s) URL", c.Install.Mirror)
	}
	return errors.Join(errs...)
}
