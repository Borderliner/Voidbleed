package control

import (
	"context"
	"io/fs"
	"strings"
	"sync"

	"voidbleed/internal/sys"
)

// demoRunner answers every command from canned output. It is how the
// interface is developed and reviewed without a machine to change, and what
// the tests drive; it also remembers what it was asked to run.
type demoRunner struct {
	mu   sync.Mutex
	seen []string
}

func (d *demoRunner) record(line string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = append(d.seen, line)
}

// ran reports whether a command containing text was run.
func (d *demoRunner) ran(text string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, line := range d.seen {
		if strings.Contains(line, text) {
			return true
		}
	}
	return false
}

func (d *demoRunner) Run(ctx context.Context, cmd sys.Cmd) error {
	d.record(cmd.String())
	return nil
}

func (d *demoRunner) Output(ctx context.Context, cmd sys.Cmd) (string, error) {
	line := cmd.String()
	d.record(line)
	switch {
	case strings.Contains(line, "xbps-query -l"):
		return `ii btop-1.4.7_1                          Monitor of resources
ii ghostty-1.2.3_1                        Terminal emulator
ii niri-26.04_1                           Scrollable-tiling Wayland compositor
ii noctalia-5.2.0_1                       Desktop shell
ii linux6.18-6.18.52_1                    Linux kernel
ii linux6.18-headers-6.18.52_1            Linux kernel headers
ii linux6.12-6.12.109_1                   Linux kernel
ii dejavu-fonts-ttf-2.37_3                Font family
`, nil
	case strings.Contains(line, "xbps-query -m"):
		return "btop-1.4.7_1\nghostty-1.2.3_1\nniri-26.04_1\nnoctalia-5.2.0_1\n", nil
	case strings.Contains(line, "xbps-query -x voidbleed-base"):
		return "base-system>=0\nlinux6.18>=0\nlinux-base>=0\n", nil
	case strings.Contains(line, "vkpurge list"):
		return "6.12.11_1\n6.18.50_1\n", nil
	case strings.Contains(line, "xbps-query -O"):
		return "dejavu-fonts-ttf-2.37_3\n", nil
	case strings.Contains(line, "xbps-install -un") || strings.Contains(line, "xbps-install -Mun"):
		return `niri-26.05_1 update x86_64 https://repo-de.voidlinux.org/current 9437184 3145728
noctalia-5.3.0_1 update x86_64 https://repo.voiders.dev 4194304 1048576
`, nil
	case strings.Contains(line, "xbps-query -Rs"):
		return `[*] btop-1.4.7_1 Monitor of resources
[-] htop-3.4.1_1 Interactive process viewer
[-] bottom-0.10.2_1 Yet another cross-platform graphical process monitor
`, nil
	case strings.Contains(line, "xbps-query -R "):
		fields := strings.Fields(line)
		name := fields[len(fields)-1]
		return "pkgver: " + name + "-1.0_1\n" +
			"short_desc: " + demoDescriptions[name] + "\n" +
			"maintainer: Voidbleed <void@example.invalid>\n" +
			"license: Apache-2.0\n" +
			"installed_size: 7554KB\n" +
			"repository: https://repo-de.voidlinux.org/current\n", nil
	case strings.Contains(line, "flatpak list --app"):
		return `[{"name":"Steam","application_id":"com.valvesoftware.Steam","version":"1.0.0.85","branch":"stable","origin":"flathub","installation":"system"},
{"name":"GIMP","application_id":"org.gimp.GIMP","version":"3.2.6","branch":"stable","origin":"flathub","installation":"system"}]`, nil
	case strings.Contains(line, "flatpak list --runtime"):
		return `[{"name":"Freedesktop Platform","application_id":"org.freedesktop.Platform","version":"25.08","branch":"25.08","origin":"flathub","installation":"system"}]`, nil
	case strings.Contains(line, "remote-ls --updates"):
		return `[{"name":"GIMP","application_id":"org.gimp.GIMP","version":"3.2.8","branch":"stable","origin":"flathub"}]`, nil
	case strings.Contains(line, "flatpak remotes"):
		return `[{"name":"flathub","url":"https://dl.flathub.org/repo/"}]`, nil
	case strings.Contains(line, "flatpak search"):
		return "org.kde.krita\tKrita\t5.2.9\tflathub\nmd.obsidian.Obsidian\tObsidian\t1.9.1\tflathub\n", nil
	case strings.Contains(line, "sv status"):
		return `run: /var/service/dbus: (pid 1204) 84231s
run: /var/service/NetworkManager: (pid 1337) 84230s
down: /var/service/cupsd: 12s, normally up
`, nil
	case strings.Contains(line, "gsettings get"):
		switch {
		case strings.HasSuffix(line, "gtk-theme"):
			return "'adw-gtk3-dark'\n", nil
		case strings.HasSuffix(line, "icon-theme"):
			return "'Reversal-red-dark'\n", nil
		case strings.HasSuffix(line, "cursor-theme"):
			return "'Vanilla-DMZ'\n", nil
		case strings.HasSuffix(line, "cursor-size"):
			return "24\n", nil
		case strings.HasSuffix(line, "font-name"):
			return "'Ubuntu 10'\n", nil
		case strings.HasSuffix(line, "color-scheme"):
			return "'prefer-dark'\n", nil
		}
		return "''\n", nil
	case strings.Contains(line, "ufw status"):
		return `Status: active
Default: deny (incoming), allow (outgoing), disabled (routed)

To                         Action      From
--                         ------      ----
[ 1] 22/tcp                ALLOW IN    Anywhere
[ 2] 1714:1764/udp         ALLOW IN    Anywhere
`, nil
	case strings.Contains(line, "fwupdmgr get-devices"):
		return `{"Devices":[{"Name":"System Firmware","Vendor":"Dell","Version":"1.24.0","DeviceId":"demo-bios","Summary":"UEFI system firmware","Flags":["updatable"]},
{"Name":"SSD 970 EVO","Vendor":"Samsung","Version":"2B2QEXE7","DeviceId":"demo-ssd","Flags":["updatable"]}]}`, nil
	case strings.Contains(line, "fwupdmgr get-updates"):
		return `{"Devices":[{"Name":"System Firmware","DeviceId":"demo-bios","Releases":[{"Version":"1.31.0","Flags":["is-upgrade"]}]}]}`, nil
	}
	return "", nil
}

func (*demoRunner) WriteFile(string, []byte, fs.FileMode) error { return nil }
func (*demoRunner) AppendFile(string, []byte) error             { return nil }
func (*demoRunner) ReadFile(string) ([]byte, error)             { return nil, nil }
func (*demoRunner) MkdirAll(string, fs.FileMode) error          { return nil }
func (*demoRunner) Symlink(string, string) error                { return nil }
func (*demoRunner) RemoveAll(string) error                      { return nil }
func (*demoRunner) Exists(string) bool                          { return false }
func (*demoRunner) Glob(string) ([]string, error)               { return nil, nil }

// demoDescriptions keeps the canned detail pane honest: it answers for the
// package that was asked about.
var demoDescriptions = map[string]string{
	"btop":             "Monitor of resources",
	"ghostty":          "Terminal emulator",
	"niri":             "Scrollable-tiling Wayland compositor",
	"noctalia":         "Desktop shell",
	"linux6.18":        "Linux kernel",
	"dejavu-fonts-ttf": "Font family",
	"htop":             "Interactive process viewer",
	"bottom":           "Yet another graphical process monitor",
}
