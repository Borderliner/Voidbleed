// Package hw detects the hardware facts the installer needs: disks, GPUs,
// CPU vendor, chassis, Bluetooth, firmware and memory.
package hw

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Vendor string

const (
	Intel   Vendor = "intel"
	AMD     Vendor = "amd"
	NVIDIA  Vendor = "nvidia"
	Virtual Vendor = "virtual" // virtio-gpu, QXL, VMware, Bochs
	Other   Vendor = "other"
)

// NVIDIA architecture families, as used by catalog detect rules.
const (
	GenOlder   = "older" // Kepler and before: no driver Voidbleed ships
	GenMaxwell = "maxwell"
	GenPascal  = "pascal"
	GenTuring  = "turing" // Turing and newer ("turing+")
)

type GPU struct {
	Slot     string // PCI address, e.g. 0000:01:00.0
	Vendor   Vendor
	DeviceID uint16
	// NvidiaGen is set for NVIDIA GPUs only.
	NvidiaGen string
}

type Disk struct {
	Path      string
	Model     string
	SizeBytes uint64
	Transport string // nvme, sata, usb, virtio…
	Removable bool
	// LiveMedium is true for the disk the live system booted from.
	LiveMedium bool
	Partitions []Partition
}

type Partition struct {
	Path       string
	FSType     string
	Label      string
	SizeBytes  uint64
	Mountpoint string
}

type Facts struct {
	UEFI      bool
	UEFI64    bool
	CPU       Vendor
	Laptop    bool
	Bluetooth bool
	MemoryMiB int
	GPUs      []GPU
	Disks     []Disk
}

// HasGPU reports whether any GPU from vendor is present.
func (f Facts) HasGPU(v Vendor) bool {
	for _, g := range f.GPUs {
		if g.Vendor == v {
			return true
		}
	}
	return false
}

// NvidiaGens lists the architecture families of the NVIDIA GPUs present.
func (f Facts) NvidiaGens() []string {
	var gens []string
	for _, g := range f.GPUs {
		if g.Vendor == NVIDIA {
			gens = append(gens, g.NvidiaGen)
		}
	}
	return gens
}

// Detector reads facts relative to Root ("/" on a real system) so tests can
// point it at a fixture tree. Lsblk returns `lsblk -J` output.
type Detector struct {
	Root  string
	Lsblk func() ([]byte, error)
}

func New() Detector {
	return Detector{Root: "/", Lsblk: runLsblk}
}

func (d Detector) path(p string) string { return filepath.Join(d.Root, p) }

func (d Detector) Detect() (Facts, error) {
	f := Facts{
		UEFI:      exists(d.path("/sys/firmware/efi")),
		CPU:       d.cpuVendor(),
		Laptop:    d.laptop(),
		Bluetooth: nonEmptyDir(d.path("/sys/class/bluetooth")),
		MemoryMiB: d.memoryMiB(),
		GPUs:      d.gpus(),
	}
	if size, err := os.ReadFile(d.path("/sys/firmware/efi/fw_platform_size")); err == nil {
		f.UEFI64 = strings.TrimSpace(string(size)) == "64"
	}
	disks, err := d.disks()
	if err != nil {
		return f, fmt.Errorf("listing disks: %w", err)
	}
	f.Disks = disks
	return f, nil
}

func (d Detector) cpuVendor() Vendor {
	file, err := os.Open(d.path("/proc/cpuinfo"))
	if err != nil {
		return Other
	}
	defer file.Close()
	s := bufio.NewScanner(file)
	for s.Scan() {
		key, value, ok := strings.Cut(s.Text(), ":")
		if !ok || strings.TrimSpace(key) != "vendor_id" {
			continue
		}
		switch strings.TrimSpace(value) {
		case "GenuineIntel":
			return Intel
		case "AuthenticAMD":
			return AMD
		}
		return Other
	}
	return Other
}

// laptop: a battery, or a DMI chassis type that means portable.
func (d Detector) laptop() bool {
	supplies, _ := filepath.Glob(d.path("/sys/class/power_supply/*/type"))
	for _, t := range supplies {
		if b, err := os.ReadFile(t); err == nil && strings.TrimSpace(string(b)) == "Battery" {
			return true
		}
	}
	if b, err := os.ReadFile(d.path("/sys/class/dmi/id/chassis_type")); err == nil {
		switch strings.TrimSpace(string(b)) {
		case "8", "9", "10", "11", "14", "30", "31", "32":
			return true
		}
	}
	return false
}

func (d Detector) memoryMiB() int {
	file, err := os.Open(d.path("/proc/meminfo"))
	if err != nil {
		return 0
	}
	defer file.Close()
	s := bufio.NewScanner(file)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kib, _ := strconv.Atoi(fields[1])
			return kib / 1024
		}
	}
	return 0
}

func (d Detector) gpus() []GPU {
	var gpus []GPU
	devices, _ := filepath.Glob(d.path("/sys/bus/pci/devices/*"))
	sort.Strings(devices)
	for _, dev := range devices {
		class := readHex(filepath.Join(dev, "class"))
		if class>>16 != 0x03 { // display controller
			continue
		}
		vendor := readHex(filepath.Join(dev, "vendor"))
		device := uint16(readHex(filepath.Join(dev, "device")))
		g := GPU{Slot: filepath.Base(dev), DeviceID: device}
		switch vendor {
		case 0x8086:
			g.Vendor = Intel
		case 0x1002:
			g.Vendor = AMD
		case 0x10de:
			g.Vendor = NVIDIA
			g.NvidiaGen = NvidiaGeneration(device)
		case 0x1af4, 0x1b36, 0x15ad, 0x1234, 0x80ee:
			g.Vendor = Virtual
		default:
			g.Vendor = Other
		}
		gpus = append(gpus, g)
	}
	return gpus
}

// NvidiaGeneration maps a PCI device id to an architecture family. NVIDIA
// allocates ids roughly by generation; the ranges are coarse, so the TUI lets
// the user override the resulting driver choice.
func NvidiaGeneration(id uint16) string {
	switch {
	case id >= 0x1e00:
		return GenTuring
	case id >= 0x1500:
		return GenPascal
	case id >= 0x1340:
		return GenMaxwell
	default:
		return GenOlder
	}
}

type lsblkOutput struct {
	Blockdevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name        string        `json:"name"`
	PKName      *string       `json:"pkname"`
	Path        string        `json:"path"`
	Type        string        `json:"type"`
	Model       *string       `json:"model"`
	Size        uint64        `json:"size"`
	Tran        *string       `json:"tran"`
	RM          bool          `json:"rm"`
	RO          bool          `json:"ro"`
	FSType      *string       `json:"fstype"`
	Label       *string       `json:"label"`
	Mountpoints []*string     `json:"mountpoints"`
	Children    []lsblkDevice `json:"children"`
}

const lsblkColumns = "NAME,PKNAME,PATH,TYPE,MODEL,SIZE,TRAN,RM,RO,FSTYPE,LABEL,MOUNTPOINTS"

func runLsblk() ([]byte, error) {
	return exec.Command("lsblk", "-J", "-b", "-o", lsblkColumns).Output()
}

func (d Detector) disks() ([]Disk, error) {
	raw, err := d.Lsblk()
	if err != nil {
		return nil, err
	}
	return ParseLsblk(raw)
}

// ParseLsblk turns `lsblk -J -b -o PATH,TYPE,…` output into installable disks.
// Loop, zram and optical devices and read-only disks are skipped.
func ParseLsblk(raw []byte) ([]Disk, error) {
	var out lsblkOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	var disks []Disk
	for _, dev := range nest(out.Blockdevices) {
		if dev.Type != "disk" || dev.RO || strings.HasPrefix(dev.Path, "/dev/zram") {
			continue
		}
		disk := Disk{
			Path:      dev.Path,
			Model:     strings.TrimSpace(deref(dev.Model)),
			SizeBytes: dev.Size,
			Transport: deref(dev.Tran),
			Removable: dev.RM,
		}
		var walk func(devs []lsblkDevice)
		walk = func(devs []lsblkDevice) {
			for _, child := range devs {
				mount := firstMount(child.Mountpoints)
				label := deref(child.Label)
				// dmsquash-live mounts the ISO filesystem at /run/initramfs/live.
				if label == "VOID_LIVE" || strings.HasPrefix(mount, "/run/initramfs/live") {
					disk.LiveMedium = true
				}
				if child.Type == "part" {
					disk.Partitions = append(disk.Partitions, Partition{
						Path:       child.Path,
						FSType:     deref(child.FSType),
						Label:      label,
						SizeBytes:  child.Size,
						Mountpoint: mount,
					})
				}
				walk(child.Children)
			}
		}
		if deref(dev.Label) == "VOID_LIVE" || strings.HasPrefix(firstMount(dev.Mountpoints), "/run/initramfs/live") {
			disk.LiveMedium = true
		}
		walk(dev.Children)
		disks = append(disks, disk)
	}
	return disks, nil
}

// nest attaches devices to their parent by PKNAME. lsblk prints a tree when
// the NAME column leads, but a flat list for some column sets; handle both.
func nest(devs []lsblkDevice) []lsblkDevice {
	flat := false
	for _, d := range devs {
		if d.PKName != nil && *d.PKName != "" {
			flat = true
			break
		}
	}
	if !flat {
		return devs
	}
	byName := map[string]*lsblkDevice{}
	for i := range devs {
		byName[devs[i].Name] = &devs[i]
	}
	// Attach deepest-first so grandchildren (partition → dm-crypt) end up
	// inside their parent before that parent is copied into its disk.
	var roots []string
	children := map[string][]string{}
	for _, d := range devs {
		if parent := deref(d.PKName); parent != "" && byName[parent] != nil {
			children[parent] = append(children[parent], d.Name)
		} else {
			roots = append(roots, d.Name)
		}
	}
	var build func(name string) lsblkDevice
	build = func(name string) lsblkDevice {
		d := *byName[name]
		d.Children = nil
		for _, c := range children[name] {
			d.Children = append(d.Children, build(c))
		}
		return d
	}
	var tree []lsblkDevice
	for _, r := range roots {
		tree = append(tree, build(r))
	}
	return tree
}

// PartitionPath returns the n-th partition of disk: /dev/sda → /dev/sda2,
// /dev/nvme0n1 → /dev/nvme0n1p2.
func PartitionPath(disk string, n int) string {
	if last := disk[len(disk)-1]; last >= '0' && last <= '9' {
		return fmt.Sprintf("%sp%d", disk, n)
	}
	return fmt.Sprintf("%s%d", disk, n)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstMount(m []*string) string {
	for _, p := range m {
		if p != nil && *p != "" {
			return *p
		}
	}
	return ""
}

func readHex(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(string(b)), "0x"), 16, 32)
	return v
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func nonEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}
