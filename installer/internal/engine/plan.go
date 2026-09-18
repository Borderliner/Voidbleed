// Package engine turns an install config into ordered steps and runs them.
package engine

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"voidbleed/installer/internal/catalog"
	"voidbleed/installer/internal/config"
	"voidbleed/installer/internal/hw"
)

// Paths are locations on the live system and the target mount point.
type Paths struct {
	Target      string // where the new system is mounted
	LiveRepo    string // voidbleed-* packages shipped on the ISO
	LiveGroups  string // optional groups baked into the live image
	LiveConf    string // mklive's live user record
	XbpsKeys    string
	XbpsConfDir string
	Log         string // install log, copied into the target at the end
}

func DefaultPaths() Paths {
	return Paths{
		Target:      "/mnt/voidbleed",
		LiveRepo:    "/usr/share/voidbleed/repo",
		LiveGroups:  "/usr/share/voidbleed/live-groups.txt",
		LiveConf:    "/etc/default/live.conf",
		XbpsKeys:    "/var/db/xbps/keys",
		XbpsConfDir: "/usr/share/xbps.d",
		Log:         "/var/log/voidbleed-install.log",
	}
}

const (
	MapperName   = "voidbleed-root"
	VoidersRepo  = "https://repo.voiders.dev"
	FlathubRepo  = "https://dl.flathub.org/repo/flathub.flatpakrepo"
	espSizeMiB   = 1024
	MinDiskBytes = 16 << 30
	btrfsOptions = "compress=zstd:1,noatime"
	nvidiaConfig = "voidbleed-nvidia-config"
	// FirstbootFlatpaks lists Flathub apps voidbleed-firstboot installs once
	// the installed system is online.
	FirstbootFlatpaks = "var/lib/voidbleed/firstboot-flatpaks"
)

// Overlay directories from the catalog map to the packages that ship them.
var overlayPackages = map[string]string{
	"overlays/nvidia": nvidiaConfig,
}

// Groups a new user joins, if the target system defines them.
var userGroups = []string{"wheel", "audio", "video", "input", "network", "kvm", "plugdev", "optical", "storage", "lp", "scanner", "bluetooth"}

type Subvolume struct {
	Name, Mountpoint string
	NoCompress       bool
}

type Plan struct {
	Config  config.Config
	Facts   hw.Facts
	Catalog *catalog.Catalog
	Paths   Paths

	Groups    []string
	Selection catalog.Selection
	Kernel    string

	ESP, SwapPart, RootPart string
	// RootDev is the device the root filesystem lives on: the partition, or
	// the opened LUKS mapping.
	RootDev    string
	Subvolumes []Subvolume
	SwapFile   string // path inside the target, when swap is a file

	// Packages must be installed on the target when the install finishes.
	Packages []string
	Services []string
	// ManagedServices are services the installer may disable when unwanted.
	ManagedServices []string
	Cmdline         []string
}

// NewPlan checks the config against the catalog and computes the layout.
// Machine checks (disk present, network) happen in the preflight step.
func NewPlan(cfg config.Config, facts hw.Facts, cat *catalog.Catalog, paths Paths) (*Plan, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	p := &Plan{Config: cfg, Facts: facts, Catalog: cat, Paths: paths, Kernel: cat.KernelSeries()}
	if p.Kernel == "" {
		return nil, fmt.Errorf("catalog pins no kernel series")
	}

	ids := cfg.Install.Groups
	if ids == nil {
		ids = cat.DefaultSelection(facts)
	}
	groups, err := cat.Resolve(ids)
	if err != nil {
		return nil, err
	}
	p.Groups = groups
	p.Selection = cat.Expand(groups)

	disk := cfg.Disk.Device
	part := 1
	p.ESP = hw.PartitionPath(disk, part)
	if cfg.Swap.Mode == config.SwapPartition {
		part++
		p.SwapPart = hw.PartitionPath(disk, part)
	}
	part++
	p.RootPart = hw.PartitionPath(disk, part)
	p.RootDev = p.RootPart
	if cfg.Disk.Encrypt {
		p.RootDev = "/dev/mapper/" + MapperName
	}

	if cfg.Disk.Filesystem == config.Btrfs {
		p.Subvolumes = []Subvolume{
			{Name: "@", Mountpoint: "/"},
			{Name: "@home", Mountpoint: "/home"},
			{Name: "@snapshots", Mountpoint: "/.snapshots"},
			{Name: "@var_log", Mountpoint: "/var/log"},
		}
	}
	if cfg.Swap.Mode == config.SwapFile {
		p.SwapFile = "/swapfile"
		if cfg.Disk.Filesystem == config.Btrfs {
			// Swap files must not be snapshotted or compressed.
			p.Subvolumes = append(p.Subvolumes, Subvolume{Name: "@swap", Mountpoint: "/swap", NoCompress: true})
			p.SwapFile = "/swap/swapfile"
		}
	}

	add := func(dst *[]string, items ...string) {
		for _, it := range items {
			if !slices.Contains(*dst, it) {
				*dst = append(*dst, it)
			}
		}
	}
	add(&p.Packages, "voidbleed-desktop")
	add(&p.Packages, cat.Defaults...)
	add(&p.Packages, p.Selection.Packages...)
	for _, overlay := range p.Selection.Overlays {
		if pkg, ok := overlayPackages[overlay]; ok {
			add(&p.Packages, pkg)
		}
	}
	if cfg.Swap.Mode == config.SwapZram {
		add(&p.Packages, "zramen")
	}

	add(&p.Services, cat.Services...)
	add(&p.Services, p.Selection.Services...)
	if cfg.Swap.Mode == config.SwapZram {
		add(&p.Services, "zramen")
	}
	for _, g := range cat.Groups {
		add(&p.ManagedServices, g.Services...)
	}
	add(&p.ManagedServices, "zramen", "dhcpcd", "wpa_supplicant")

	// "splash" is what tells the rest of the system a boot splash is
	// wanted; plymouth itself starts from the initramfs either way.
	add(&p.Cmdline, "loglevel=4", "quiet", "splash")
	add(&p.Cmdline, p.Selection.Cmdline...)
	return p, nil
}

// T returns a path inside the target system.
func (p *Plan) T(parts ...string) string {
	return path.Join(append([]string{p.Paths.Target}, parts...)...)
}

// Repositories returns xbps --repository arguments for installing into the target.
func (p *Plan) Repositories() []string {
	mirror := strings.TrimRight(p.Config.Install.Mirror, "/")
	repos := []string{p.Paths.LiveRepo, mirror + "/current"}
	if slices.Contains(p.Selection.Repos, "nonfree") {
		repos = append(repos, mirror+"/current/nonfree")
	}
	repos = append(repos, VoidersRepo)
	args := make([]string, len(repos))
	for i, r := range repos {
		args[i] = "--repository=" + r
	}
	return args
}

func (p *Plan) Offline() bool { return p.Config.Install.Source == config.SourceOffline }

// Summary lines for logs and the TUI's review screen.
func (p *Plan) Summary() []string {
	c := p.Config
	fs := string(c.Disk.Filesystem)
	if c.Disk.Encrypt {
		fs += " on LUKS2"
	}
	swap := string(c.Swap.Mode)
	if c.Swap.Mode == config.SwapFile || c.Swap.Mode == config.SwapPartition {
		swap = fmt.Sprintf("%s (%d MiB)", swap, c.Swap.SizeMiB)
	}
	return []string{
		"disk:     " + c.Disk.Device + " (erased)",
		"layout:   " + p.ESP + " EFI 1 GiB /boot" + ifThen(p.SwapPart != "", ", "+p.SwapPart+" swap") + ", " + p.RootPart + " " + fs,
		"swap:     " + swap,
		"source:   " + string(c.Install.Source),
		"kernel:   " + p.Kernel,
		"groups:   " + strings.Join(p.Groups, ", "),
		"user:     " + c.User.Name + ifThen(c.User.RootPassword == "", " (root locked)", ""),
		"system:   " + c.System.Hostname + ", " + c.System.Timezone + ", " + c.System.Locale + ", " + strings.Join(c.System.KeyboardLayouts, ","),
	}
}

func ifThen(cond bool, yes string, no ...string) string {
	if cond {
		return yes
	}
	if len(no) > 0 {
		return no[0]
	}
	return ""
}
