package system

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"voidbleed/internal/sys"
)

// Kernel is one installed kernel series.
type Kernel struct {
	Package   string // linux6.18
	Version   string // 6.18.52_1
	Series    string // 6.18
	Booted    bool
	Headers   bool // the matching -headers package is installed, which dkms needs
	Pinned    bool // the series Voidbleed installs by default
	Installed bool // only meaningful in the list of what the repositories have
	About     string
}

// kernelPackage matches linux6.18 and linux6.18-headers, but not linux-base,
// linux-firmware or the linux metapackage.
var kernelPackage = regexp.MustCompile(`^linux([0-9]+\.[0-9]+)(-headers)?$`)

// Kernels lists the kernel series installed, which one is running, and which
// one Voidbleed pins.
func (c *Client) Kernels(ctx context.Context) ([]Kernel, error) {
	out, err := c.output(ctx, "xbps-query", "-l")
	if err != nil {
		return nil, err
	}
	booted := strings.TrimSpace(readFile("/proc/sys/kernel/osrelease"))
	pinned := c.pinnedKernel(ctx)

	byName := map[string]*Kernel{}
	for _, line := range lines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, version := splitPkgver(fields[1])
		match := kernelPackage.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		series, headers := match[1], match[2] != ""
		k := byName["linux"+series]
		if k == nil {
			k = &Kernel{Package: "linux" + series, Series: series, Pinned: "linux"+series == pinned}
			byName[k.Package] = k
		}
		if headers {
			k.Headers = true
			continue
		}
		k.Version = version
		k.Booted = version == booted
	}

	kernels := make([]Kernel, 0, len(byName))
	for _, k := range byName {
		kernels = append(kernels, *k)
	}
	sortKernels(kernels)
	return kernels, nil
}

// AvailableKernels is every kernel series the repositories carry. Voidbleed
// ignores Void's "linux" metapackage, so installing a series by name is how a
// machine gets another one -- and they live side by side, with the bootloader
// offering each.
func (c *Client) AvailableKernels(ctx context.Context) ([]Kernel, error) {
	out, err := c.output(ctx, "xbps-query", "-Rs", "linux")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	byName := map[string]*Kernel{}
	for _, line := range lines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, version := splitPkgver(fields[1])
		match := kernelPackage.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		series, headers := match[1], match[2] != ""
		k := byName["linux"+series]
		if k == nil {
			k = &Kernel{Package: "linux" + series, Series: series}
			byName[k.Package] = k
		}
		if headers {
			k.Headers = true // the repositories have headers for it
			continue
		}
		k.Version, k.Installed = version, fields[0] == "[*]"
		k.About = strings.TrimSpace(strings.TrimPrefix(line, fields[0]+" "+fields[1]))
	}
	kernels := make([]Kernel, 0, len(byName))
	for _, k := range byName {
		kernels = append(kernels, *k)
	}
	sortKernels(kernels)
	return kernels, nil
}

// sortKernels puts the newest series first. The versions are numbers, not
// words: 6.6 comes before 6.18, which sorting as text gets backwards.
func sortKernels(kernels []Kernel) {
	sort.Slice(kernels, func(i, j int) bool {
		return seriesNewer(kernels[i].Series, kernels[j].Series)
	})
}

func seriesNewer(a, b string) bool {
	amaj, amin := splitSeries(a)
	bmaj, bmin := splitSeries(b)
	if amaj != bmaj {
		return amaj > bmaj
	}
	return amin > bmin
}

func splitSeries(s string) (major, minor int) {
	head, tail, _ := strings.Cut(s, ".")
	major, _ = strconv.Atoi(head)
	minor, _ = strconv.Atoi(tail)
	return major, minor
}

// pinnedKernel is the series Voidbleed installs. The xbps rule only says which
// packages to ignore, so the answer is what voidbleed-base depends on -- on a
// plain Void machine there is no pin and this is empty.
func (c *Client) pinnedKernel(ctx context.Context) string {
	out, err := c.output(ctx, "xbps-query", "-x", "voidbleed-base")
	if err != nil {
		return ""
	}
	for _, line := range lines(out) {
		name, _, _ := strings.Cut(strings.TrimSpace(line), ">")
		if match := kernelPackage.FindStringSubmatch(name); match != nil && match[2] == "" {
			return name
		}
	}
	return ""
}

// KernelMetapackageIgnored reports whether Voidbleed's rule that keeps Void's
// "linux" metapackage from dragging in another series is in place.
func KernelMetapackageIgnored() bool {
	return strings.Contains(readFile(ConfigPath), "ignorepkg=linux")
}

// StaleKernels lists kernel trees left in /boot that no installed package owns
// any more -- what vkpurge exists to clean up.
func (c *Client) StaleKernels(ctx context.Context) ([]string, error) {
	if !Have("vkpurge") {
		return nil, nil
	}
	out, err := c.output(ctx, "vkpurge", "list")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// KernelInstallCmd installs a series. Headers come with it when the machine
// builds modules against the ones it already has: a dkms driver on a kernel
// with no headers is a driver that will not build.
func KernelInstallCmd(k Kernel, withHeaders bool) sys.Cmd {
	args := []string{"-Sy", k.Package}
	if withHeaders {
		args = append(args, k.Package+"-headers")
	}
	return sys.Command("xbps-install", args...)
}

// KernelRemoveCmd removes a whole series, headers included.
func KernelRemoveCmd(k Kernel) sys.Cmd {
	args := []string{"-Ry", k.Package}
	if k.Headers {
		args = append(args, k.Package+"-headers")
	}
	return sys.Command("xbps-remove", args...)
}

// VkpurgeCmd removes one leftover kernel tree from /boot.
func VkpurgeCmd(version string) sys.Cmd {
	return sys.Command("vkpurge", "rm", version)
}

// VkpurgeAllCmd removes every leftover tree.
func VkpurgeAllCmd() sys.Cmd {
	return sys.Command("vkpurge", "rm", "all")
}
