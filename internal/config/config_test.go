package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func valid() Config {
	c := Default()
	c.Disk.Device = "/dev/vda"
	c.User.Name = "reza"
	c.User.Password = "secret"
	return c
}

func TestValidDefaults(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Config)
		want   string
	}{
		"hostname":         {func(c *Config) { c.System.Hostname = "Bad_Host" }, "hostname"},
		"timezone":         {func(c *Config) { c.System.Timezone = "Mars/Olympus" }, "timezone"},
		"traversal":        {func(c *Config) { c.System.Timezone = "../etc/passwd" }, "timezone"},
		"layout":           {func(c *Config) { c.System.KeyboardLayouts = []string{"US!"} }, "keyboard layout"},
		"no layouts":       {func(c *Config) { c.System.KeyboardLayouts = nil }, "keyboard layout"},
		"device":           {func(c *Config) { c.Disk.Device = "sda" }, "/dev path"},
		"filesystem":       {func(c *Config) { c.Disk.Filesystem = "xfs" }, "ext4 or btrfs"},
		"short passphrase": {func(c *Config) { c.Disk.Encrypt = true; c.Disk.Passphrase = "short" }, "passphrase"},
		"swap size":        {func(c *Config) { c.Swap = Swap{Mode: SwapFile, SizeMiB: 10} }, "swap size"},
		"luks swap part": {func(c *Config) {
			c.Disk.Encrypt = true
			c.Disk.Passphrase = "longenough"
			c.Swap = Swap{Mode: SwapPartition, SizeMiB: 2048}
		}, "outside the encrypted"},
		"username": {func(c *Config) { c.User.Name = "Reza" }, "username"},
		"reserved": {func(c *Config) { c.User.Name = "anon" }, "reserved"},
		"password": {func(c *Config) { c.User.Password = "" }, "password"},
		"source":   {func(c *Config) { c.Install.Source = "usb" }, "offline or network"},
		"mirror":   {func(c *Config) { c.Install.Mirror = "ftp://x" }, "mirror"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := valid()
			tc.mutate(&c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.toml")
	os.WriteFile(path, []byte("[disk]\ndevice = \"/dev/vda\"\nfilesytem = \"btrfs\"\n"), 0o600)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "filesytem") {
		t.Fatalf("typo not reported: %v", err)
	}
}

func TestLoadOverlaysDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.toml")
	os.WriteFile(path, []byte("[disk]\ndevice = \"/dev/vda\"\nfilesystem = \"btrfs\"\n"), 0o600)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Disk.Filesystem != Btrfs || c.Swap.Mode != SwapZram || c.Install.Source != SourceOffline {
		t.Fatalf("defaults not kept: %+v", c)
	}
	if c.Install.Groups != nil {
		t.Fatalf("groups should stay nil (auto) when omitted, got %v", c.Install.Groups)
	}
}
