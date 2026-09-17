package engine

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"voidbleed/installer/internal/catalog"
	"voidbleed/installer/internal/config"
	"voidbleed/installer/internal/hw"
	"voidbleed/installer/internal/sys"
)

var update = flag.Bool("update", false, "rewrite golden transcripts")

func loadCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load("../../../catalog")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func laptop() hw.Facts {
	return hw.Facts{
		UEFI: true, UEFI64: true, CPU: hw.Intel, Laptop: true, Bluetooth: true, MemoryMiB: 16000,
		GPUs: []hw.GPU{{Vendor: hw.Intel}, {Vendor: hw.NVIDIA, DeviceID: 0x1bbb, NvidiaGen: hw.GenPascal}},
		Disks: []hw.Disk{
			{Path: "/dev/sda", SizeBytes: 32 << 30, Transport: "usb", LiveMedium: true},
			{Path: "/dev/nvme0n1", SizeBytes: 1000 << 30, Transport: "nvme"},
		},
	}
}

func baseConfig() config.Config {
	c := config.Default()
	c.System.Timezone = "Asia/Tehran"
	c.System.KeyboardLayouts = []string{"us", "ir"}
	c.Disk.Device = "/dev/nvme0n1"
	c.User = config.User{Name: "reza", FullName: "Reza", Password: "user-pass-1", RootPassword: ""}
	return c
}

// dryRun returns a runner whose target looks like a freshly copied or
// installed Voidbleed root.
func dryRun(p *Plan) *sys.DryRun {
	d := sys.NewDryRun()
	d.DefaultOutput = func(cmd sys.Cmd) string {
		if cmd.Name == "blkid" {
			return "uuid-of-" + filepath.Base(cmd.Args[len(cmd.Args)-1]) + "\n"
		}
		return ""
	}
	seed := map[string]string{
		p.Paths.LiveConf:                             "USERNAME=anon\n",
		p.Paths.LiveGroups:                           "gpu-intel\ngpu-amd\nbluetooth\npower-ppd\nfirefox\n",
		p.T("etc/default/libc-locales"):              "#en_US.UTF-8 UTF-8  \n#fa_IR UTF-8  \n",
		p.T("etc/rc.conf"):                           "# rc.conf\n#HOSTNAME=\"void\"\n#HARDWARECLOCK=\"UTC\"\nKEYMAP=\"us\"\n",
		p.T("etc/group"):                             "root:x:0:\nwheel:x:4:\naudio:x:12:\nvideo:x:13:\ninput:x:25:\nnetwork:x:21:\nkvm:x:24:\nplugdev:x:26:\n",
		p.T("etc/default/grub"):                      "GRUB_DEFAULT=0\nGRUB_TIMEOUT=5\nGRUB_DISTRIBUTOR=\"Void\"\nGRUB_CMDLINE_LINUX_DEFAULT=\"loglevel=4\"\n",
		p.T("var/lib/noctalia-greeter/greeter.toml"): "# noctalia-greeter greeter.toml\n",
	}
	for path, content := range seed {
		d.Files[path] = content
	}
	for _, svc := range append(append([]string{}, p.Services...), p.ManagedServices...) {
		d.Files[p.T("etc/sv", svc)] = ""
	}
	return d
}

func install(t *testing.T, cfg config.Config, facts hw.Facts) (*Plan, *sys.DryRun, error) {
	t.Helper()
	p, err := NewPlan(cfg, facts, loadCatalog(t), DefaultPaths())
	if err != nil {
		t.Fatal(err)
	}
	d := dryRun(p)
	x := &Exec{Plan: p, R: d}
	return p, d, Run(context.Background(), x, Steps(p), nil)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/engine -update)", err)
	}
	if got != string(want) {
		t.Errorf("transcript differs from %s (run with -update after checking):\n%s", path, diff(string(want), got))
	}
}

// diff shows the first differing lines, enough to find the change.
func diff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return "line " + strconv.Itoa(i+1) + ":\n- " + wl + "\n+ " + gl
		}
	}
	return ""
}

// The reference laptop, offline, all defaults: ext4, zram, detected groups.
func TestGoldenOfflineExt4Zram(t *testing.T) {
	_, d, err := install(t, baseConfig(), laptop())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "offline-ext4-zram", d.Transcript())
}

func TestGoldenNetworkBtrfsLuksSwapfile(t *testing.T) {
	cfg := baseConfig()
	cfg.Disk.Filesystem = config.Btrfs
	cfg.Disk.Encrypt = true
	cfg.Disk.Passphrase = "correct horse battery"
	cfg.Swap = config.Swap{Mode: config.SwapFile, SizeMiB: 8192}
	cfg.Install.Source = config.SourceNetwork
	cfg.Install.Groups = []string{"gpu-intel", "steam"}
	cfg.User.RootPassword = "root-pass-1"
	_, d, err := install(t, cfg, laptop())
	if err != nil {
		t.Fatal(err)
	}
	out := d.Transcript()
	for _, secret := range []string{"correct horse battery", "user-pass-1", "root-pass-1"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret %q leaked into the transcript", secret)
		}
	}
	golden(t, "network-btrfs-luks-swapfile", out)
}

func TestGoldenNetworkExt4SwapPartition(t *testing.T) {
	cfg := baseConfig()
	cfg.Swap = config.Swap{Mode: config.SwapPartition, SizeMiB: 4096}
	cfg.Install.Source = config.SourceNetwork
	cfg.Install.Groups = []string{}
	_, d, err := install(t, cfg, laptop())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "network-ext4-swappartition", d.Transcript())
}

func TestPlanLayout(t *testing.T) {
	cfg := baseConfig()
	cfg.Swap = config.Swap{Mode: config.SwapPartition, SizeMiB: 4096}
	p, err := NewPlan(cfg, laptop(), loadCatalog(t), DefaultPaths())
	if err != nil {
		t.Fatal(err)
	}
	if p.ESP != "/dev/nvme0n1p1" || p.SwapPart != "/dev/nvme0n1p2" || p.RootPart != "/dev/nvme0n1p3" || p.RootDev != p.RootPart {
		t.Fatalf("layout: esp=%s swap=%s root=%s dev=%s", p.ESP, p.SwapPart, p.RootPart, p.RootDev)
	}
	if !contains(p.Packages, "voidbleed-nvidia-config") || !contains(p.Packages, "nvidia580") {
		t.Fatalf("nvidia overlay package missing: %v", p.Packages)
	}
	if !contains(p.Cmdline, "nvidia_drm.modeset=1") {
		t.Fatalf("cmdline: %v", p.Cmdline)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestPreflightRefusesUnsafeDisks(t *testing.T) {
	cases := map[string]struct {
		mutate func(*config.Config, *hw.Facts)
		want   string
	}{
		"live medium": {func(c *config.Config, f *hw.Facts) { c.Disk.Device = "/dev/sda" }, "live system"},
		"mounted": {func(c *config.Config, f *hw.Facts) {
			f.Disks[1].Partitions = []hw.Partition{{Path: "/dev/nvme0n1p2", Mountpoint: "/home"}}
		}, "in use"},
		"missing":   {func(c *config.Config, f *hw.Facts) { c.Disk.Device = "/dev/vdz" }, "not found"},
		"too small": {func(c *config.Config, f *hw.Facts) { f.Disks[1].SizeBytes = 8 << 30 }, "at least"},
		"bios":      {func(c *config.Config, f *hw.Facts) { f.UEFI = false }, "UEFI"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, facts := baseConfig(), laptop()
			tc.mutate(&cfg, &facts)
			p, err := NewPlan(cfg, facts, loadCatalog(t), DefaultPaths())
			if err != nil {
				t.Fatal(err)
			}
			d := dryRun(p)
			err = Run(context.Background(), &Exec{Plan: p, R: d}, Steps(p), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if strings.Contains(d.Transcript(), "wipefs") {
				t.Fatal("disk touched after preflight failed")
			}
		})
	}
}

func TestOfflineMissingPackagesNeedNetwork(t *testing.T) {
	cfg := baseConfig()
	p, err := NewPlan(cfg, laptop(), loadCatalog(t), DefaultPaths())
	if err != nil {
		t.Fatal(err)
	}
	d := dryRun(p)
	offline := errors.New("offline")
	d.Fail = func(cmd sys.Cmd) error {
		switch {
		case cmd.Name == "xbps-query" && len(cmd.Args) == 3 && cmd.Args[2] == "nvidia580":
			return errors.New("not installed")
		case cmd.Name == "curl":
			return offline
		}
		return nil
	}
	err = Run(context.Background(), &Exec{Plan: p, R: d}, Steps(p), nil)
	if err == nil || !strings.Contains(err.Error(), "nvidia580") {
		t.Fatalf("want a network error naming nvidia580, got %v", err)
	}
}

func TestOfflineWithoutNetworkDefersFlatpak(t *testing.T) {
	cfg := baseConfig()
	cfg.Install.Groups = []string{"gpu-intel", "telegram"}
	p, err := NewPlan(cfg, laptop(), loadCatalog(t), DefaultPaths())
	if err != nil {
		t.Fatal(err)
	}
	d := dryRun(p)
	d.Fail = func(cmd sys.Cmd) error {
		if cmd.Name == "curl" {
			return errors.New("offline")
		}
		return nil
	}
	x := &Exec{Plan: p, R: d}
	if err := Run(context.Background(), x, Steps(p), nil); err != nil {
		t.Fatalf("offline install without network should succeed: %v", err)
	}
	out := d.Transcript()
	if strings.Contains(out, "flatpak remote-add") {
		t.Fatal("tried to reach Flathub while offline")
	}
	if !strings.Contains(out, "write /mnt/voidbleed/"+FirstbootFlatpaks+" (0644)\n      | org.telegram.desktop") ||
		!strings.Contains(out, "link  /mnt/voidbleed/etc/runit/runsvdir/default/voidbleed-firstboot -> /etc/sv/voidbleed-firstboot") {
		t.Fatalf("flatpak apps not deferred to first boot:\n%s", out)
	}
}

func TestFailureCleansUp(t *testing.T) {
	cfg := baseConfig()
	cfg.Disk.Encrypt = true
	cfg.Disk.Passphrase = "longenough"
	p, err := NewPlan(cfg, laptop(), loadCatalog(t), DefaultPaths())
	if err != nil {
		t.Fatal(err)
	}
	d := dryRun(p)
	d.Fail = func(cmd sys.Cmd) error {
		if cmd.Name == "tar" || (cmd.Name == "sh" && strings.Contains(strings.Join(cmd.Args, " "), "tar --create")) {
			return errors.New("disk full")
		}
		return nil
	}
	var events []Event
	ch := make(chan Event, 64)
	err = Run(context.Background(), &Exec{Plan: p, R: d}, Steps(p), ch)
	close(ch)
	for e := range ch {
		events = append(events, e)
	}
	if err == nil || !strings.Contains(err.Error(), "Copying the live system") {
		t.Fatalf("want copy failure, got %v", err)
	}
	out := d.Transcript()
	umount := strings.Index(out, "umount -R /mnt/voidbleed")
	closeLuks := strings.Index(out, "cryptsetup close "+MapperName)
	if umount < 0 || closeLuks < umount {
		t.Fatalf("cleanup should unmount, then close LUKS:\n%s", out[max(0, len(out)-400):])
	}
	last := events[len(events)-1]
	if last.Kind != StepFailed || last.Step.ID != "copy" {
		t.Fatalf("last event = %+v", last)
	}
}

func TestSetShellVars(t *testing.T) {
	in := "#KEYMAP=\"fr\"\nFOO=1\nKEYMAP=\"de\"\n"
	got := setShellVars(in, [][2]string{{"KEYMAP", "us"}, {"HARDWARECLOCK", "UTC"}})
	want := "KEYMAP=\"us\"\nFOO=1\n#KEYMAP=\"de\"\nHARDWARECLOCK=\"UTC\"\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestEnableLocale(t *testing.T) {
	got, ok := enableLocale("#en_US.UTF-8 UTF-8  \n#en_US ISO-8859-1  \n", "en_US.UTF-8")
	if !ok || got != "en_US.UTF-8 UTF-8  \n#en_US ISO-8859-1  \n" {
		t.Fatalf("%v %q", ok, got)
	}
	if _, ok := enableLocale("#fa_IR UTF-8\n", "xx_YY.UTF-8"); ok {
		t.Fatal("unknown locale reported as found")
	}
}
