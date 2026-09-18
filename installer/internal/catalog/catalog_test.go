package catalog

import (
	"slices"
	"strings"
	"testing"

	"voidbleed/installer/internal/hw"
)

// Tests run against the real catalog so a catalog edit that breaks the
// installer's assumptions fails here.
func load(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load("../../../catalog")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoadRealCatalog(t *testing.T) {
	c := load(t)
	if !slices.Contains(c.Desktop, "noctalia-greeter") || !slices.Contains(c.Defaults, "papers") {
		t.Fatalf("lists not parsed: desktop=%v defaults=%v", c.Desktop, c.Defaults)
	}
	if c.KernelSeries() != "linux6.18" {
		t.Fatalf("kernel series = %q", c.KernelSeries())
	}
	if len(c.Groups) == 0 || c.Groups[0].ID == "" {
		t.Fatal("no optional groups")
	}
}

func referenceLaptop() hw.Facts {
	return hw.Facts{
		UEFI: true, UEFI64: true, CPU: hw.Intel, Laptop: true, Bluetooth: true,
		GPUs: []hw.GPU{
			{Vendor: hw.Intel, DeviceID: 0x3e94},
			{Vendor: hw.NVIDIA, DeviceID: 0x1bbb, NvidiaGen: hw.GenPascal},
		},
	}
}

func TestDefaultSelectionReferenceLaptop(t *testing.T) {
	got := load(t).DefaultSelection(referenceLaptop())
	want := []string{"gpu-nvidia580", "gpu-intel", "microcode-intel", "bluetooth", "power-ppd", "flatpak", "firefox"}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestIntelGraphicsBringVulkan(t *testing.T) {
	c := load(t)
	// An Intel iGPU is the only display adapter in most laptops and mini PCs,
	// and it also sits alongside the discrete card in the reference laptop.
	igpuOnly := hw.Facts{CPU: hw.Intel, GPUs: []hw.GPU{{Vendor: hw.Intel, DeviceID: 0x9a49}}}
	for name, f := range map[string]hw.Facts{"iGPU only": igpuOnly, "hybrid": referenceLaptop()} {
		ids := c.DefaultSelection(f)
		if !slices.Contains(ids, "gpu-intel") {
			t.Errorf("%s: Intel graphics not selected: %v", name, ids)
			continue
		}
		pkgs := c.Expand(ids).Packages
		// The driver is the ICD; the loader is what applications link against.
		for _, pkg := range []string{"mesa-vulkan-intel", "intel-media-driver"} {
			if !slices.Contains(pkgs, pkg) {
				t.Errorf("%s: %s missing from %v", name, pkg, pkgs)
			}
		}
	}
}

func TestDefaultSelectionAMDDesktopWithRTX(t *testing.T) {
	f := hw.Facts{CPU: hw.AMD, GPUs: []hw.GPU{{Vendor: hw.NVIDIA, DeviceID: 0x2684, NvidiaGen: hw.GenTuring}}}
	got := load(t).DefaultSelection(f)
	if !slices.Contains(got, "gpu-nvidia") || slices.Contains(got, "gpu-nvidia580") {
		t.Fatalf("RTX 4090 should get the current driver: %v", got)
	}
	for _, id := range []string{"power-ppd", "bluetooth", "microcode-intel", "gpu-intel"} {
		if slices.Contains(got, id) {
			t.Fatalf("%s selected on an AMD desktop without Bluetooth: %v", id, got)
		}
	}
}

func TestResolveAddsRequiredGroups(t *testing.T) {
	got, err := load(t).Resolve([]string{"steam"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"flatpak", "steam"}) {
		t.Fatalf("got %v", got)
	}
}

func TestResolveRejectsExclusivePair(t *testing.T) {
	_, err := load(t).Resolve([]string{"power-ppd", "power-tlp"})
	if err == nil || !strings.Contains(err.Error(), "can't both") {
		t.Fatalf("want exclusivity error, got %v", err)
	}
}

func TestResolveRejectsUnknown(t *testing.T) {
	if _, err := load(t).Resolve([]string{"emacs"}); err == nil {
		t.Fatal("unknown group accepted")
	}
}

func TestExpand(t *testing.T) {
	c := load(t)
	s := c.Expand([]string{"gpu-nvidia580", "microcode-intel", "steam", "flatpak"})
	if !slices.Equal(s.Repos, []string{"nonfree"}) {
		t.Fatalf("repos deduplicated wrong: %v", s.Repos)
	}
	if !slices.Contains(s.Packages, "nvidia580") || !slices.Contains(s.Packages, "linux6.18-headers") {
		t.Fatalf("packages: %v", s.Packages)
	}
	if !slices.Equal(s.Flatpaks, []string{"com.valvesoftware.Steam"}) || !slices.Equal(s.Overlays, []string{"overlays/nvidia"}) {
		t.Fatalf("flatpaks %v overlays %v", s.Flatpaks, s.Overlays)
	}
}
