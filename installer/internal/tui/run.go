package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"voidbleed/installer/internal/config"
	"voidbleed/installer/internal/hw"
	"voidbleed/installer/internal/sys"
)

// Run starts the interactive installer.
func Run(opts Options) error {
	st := &state{
		opts:     opts,
		styles:   newStyles(detectGlyphs()),
		cat:      opts.Catalog,
		cfg:      config.Default(),
		selected: map[string]bool{},
		detected: map[string]bool{},
	}
	if opts.Demo {
		st.facts = demoFacts()
		st.livePackages = demoLivePackages(st)
	} else {
		if os.Geteuid() != 0 {
			return errors.New("the installer needs root; run it with sudo (or use --demo to look around)")
		}
		facts, err := hw.New().Detect()
		if err != nil {
			return err
		}
		st.facts = facts
		st.livePackages = installedPackages()
	}

	st.cfg.System.Timezone = currentTimezone()
	for _, id := range st.cat.DefaultSelection(st.facts) {
		st.selected[id] = true
	}
	for _, g := range st.cat.Groups {
		if g.Matches(st.facts) {
			st.detected[g.ID] = true
		}
	}

	m := newModel(st)
	_, err := tea.NewProgram(m).Run()
	return err
}

// installedPackages lists package names on the running (live) system.
func installedPackages() map[string]bool {
	out, err := exec.Command("xbps-query", "-l").Output()
	pkgs := map[string]bool{}
	if err != nil {
		return pkgs
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pkgver := fields[1]
		if i := strings.LastIndex(pkgver, "-"); i > 0 {
			pkgs[pkgver[:i]] = true
		}
	}
	return pkgs
}

// runner returns the Runner for the install and a function closing its log.
// Every command and output line goes to the log file and the progress view.
func (m *Model) runner(msgs chan<- tea.Msg) (sys.Runner, func()) {
	var logFile *os.File
	if !m.st.opts.Demo {
		logFile, _ = os.OpenFile(m.st.opts.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	}
	logf := func(line string) {
		if logFile != nil {
			logFile.WriteString(line + "\n")
		}
		select {
		case msgs <- logMsg(line):
		default: // never block the install on a slow screen
		}
	}
	closeLog := func() {
		if logFile != nil {
			logFile.Close()
		}
	}
	if m.st.opts.Demo {
		return &demoRunner{DryRun: sys.NewDryRun(), log: logf}, closeLog
	}
	return sys.Real{Log: logf}, closeLog
}

// ── demo mode ───────────────────────────────────────────────────────────────

func demoFacts() hw.Facts {
	return hw.Facts{
		UEFI: true, UEFI64: true, CPU: hw.Intel, Laptop: true, Bluetooth: true, MemoryMiB: 15752,
		GPUs: []hw.GPU{
			{Slot: "0000:00:02.0", Vendor: hw.Intel, DeviceID: 0x3e94},
			{Slot: "0000:01:00.0", Vendor: hw.NVIDIA, DeviceID: 0x1bbb, NvidiaGen: hw.GenPascal},
		},
		Disks: []hw.Disk{
			{Path: "/dev/nvme0n1", Model: "WD Blue SN5000 1TB", SizeBytes: 1000204886016, Transport: "nvme",
				Partitions: []hw.Partition{
					{Path: "/dev/nvme0n1p1", FSType: "vfat", SizeBytes: 1 << 30},
					{Path: "/dev/nvme0n1p2", FSType: "ext4", SizeBytes: 990 << 30},
				}},
			{Path: "/dev/nvme1n1", Model: "SAMSUNG MZVLB512HAJQ-000H1", SizeBytes: 512110190592, Transport: "nvme"},
			{Path: "/dev/sda", Model: "SanDisk Ultra", SizeBytes: 32 << 30, Transport: "usb", Removable: true, LiveMedium: true},
			{Path: "/dev/sdb", Model: "Kingston DataTraveler", SizeBytes: 8 << 30, Transport: "usb", Removable: true},
		},
	}
}

// demoLivePackages pretends the live image holds everything except the
// packages that really aren't on the ISO.
func demoLivePackages(st *state) map[string]bool {
	pkgs := map[string]bool{}
	notOnISO := map[string]bool{"nvidia": true, "nvidia580": true, "linux6.18-headers": true,
		"intel-ucode": true, "tlp": true, "cups": true, "cups-filters": true, "mpvpaper": true,
		"steam-udev-rules": true, "noto-fonts-cjk": true, "font-vazirmatn": true}
	for _, g := range st.cat.Groups {
		for _, p := range g.Packages {
			if !notOnISO[p] {
				pkgs[p] = true
			}
		}
	}
	return pkgs
}

// demoRunner records like DryRun but paces commands and logs them, so the
// progress screen behaves like a real install.
type demoRunner struct {
	*sys.DryRun
	log func(string)
}

func (d *demoRunner) Run(ctx context.Context, cmd sys.Cmd) error {
	d.log("$ " + cmd.String())
	delay := 120 * time.Millisecond
	switch {
	case cmd.Name == "sh" || cmd.Name == "xbps-install":
		delay = 3 * time.Second
	case cmd.Name == "cryptsetup" || strings.HasPrefix(cmd.Name, "mkfs"):
		delay = 900 * time.Millisecond
	case cmd.Chroot != "" && (cmd.Name == "flatpak" || cmd.Name == "xbps-reconfigure"):
		delay = 2 * time.Second
	}
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	return d.DryRun.Run(ctx, cmd)
}

func (d *demoRunner) Output(ctx context.Context, cmd sys.Cmd) (string, error) {
	if cmd.Name == "blkid" {
		return "1234-ABCD\n", nil
	}
	return d.DryRun.Output(ctx, cmd)
}

// The demo target has the files the steps read and the services they link.
func (d *demoRunner) Exists(path string) bool {
	return strings.Contains(path, "/etc/sv/") || d.DryRun.Exists(path)
}

func (d *demoRunner) ReadFile(path string) ([]byte, error) {
	if b, err := d.DryRun.ReadFile(path); err == nil {
		return b, nil
	}
	switch {
	case strings.HasSuffix(path, "libc-locales"):
		return []byte("#en_US.UTF-8 UTF-8\n#de_DE.UTF-8 UTF-8\n#fa_IR.UTF-8 UTF-8\n#en_GB.UTF-8 UTF-8\n#fr_FR.UTF-8 UTF-8\n#ja_JP.UTF-8 UTF-8\n"), nil
	case strings.HasSuffix(path, "/etc/group"):
		return []byte("wheel:x:4:\naudio:x:12:\nvideo:x:13:\ninput:x:25:\n"), nil
	}
	return nil, fs.ErrNotExist
}
