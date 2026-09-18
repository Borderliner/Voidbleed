package hw

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The reference laptop: Intel UHD 630 + Quadro P3200, battery, Bluetooth.
func referenceLaptop(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "sys/firmware/efi/fw_platform_size", "64\n")
	write(t, root, "proc/cpuinfo", "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Xeon(R) E-2176M\n")
	write(t, root, "proc/meminfo", "MemTotal:       32590000 kB\nMemFree:  1 kB\n")
	write(t, root, "sys/class/power_supply/BAT0/type", "Battery\n")
	write(t, root, "sys/class/power_supply/AC/type", "Mains\n")
	write(t, root, "sys/class/bluetooth/hci0/uevent", "")
	write(t, root, "sys/bus/pci/devices/0000:00:02.0/class", "0x030000\n")
	write(t, root, "sys/bus/pci/devices/0000:00:02.0/vendor", "0x8086\n")
	write(t, root, "sys/bus/pci/devices/0000:00:02.0/device", "0x3e94\n")
	write(t, root, "sys/bus/pci/devices/0000:01:00.0/class", "0x030200\n")
	write(t, root, "sys/bus/pci/devices/0000:01:00.0/vendor", "0x10de\n")
	write(t, root, "sys/bus/pci/devices/0000:01:00.0/device", "0x1bbb\n")
	write(t, root, "sys/bus/pci/devices/0000:6f:00.0/class", "0x028000\n") // Wi-Fi, not a GPU
	write(t, root, "sys/bus/pci/devices/0000:6f:00.0/vendor", "0x8086\n")
	return root
}

func TestDetectReferenceLaptop(t *testing.T) {
	d := Detector{Root: referenceLaptop(t), Lsblk: func() ([]byte, error) { return []byte(`{"blockdevices":[]}`), nil }}
	f, err := d.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if !f.UEFI || !f.UEFI64 || f.CPU != Intel || !f.Laptop || !f.Bluetooth {
		t.Fatalf("basic facts wrong: %+v", f)
	}
	if f.MemoryMiB != 31826 {
		t.Fatalf("memory = %d MiB", f.MemoryMiB)
	}
	if len(f.GPUs) != 2 || !f.HasGPU(Intel) || !f.HasGPU(NVIDIA) || f.HasGPU(AMD) {
		t.Fatalf("gpus = %+v", f.GPUs)
	}
	if gens := f.NvidiaGens(); len(gens) != 1 || gens[0] != GenPascal {
		t.Fatalf("P3200 generation = %v, want pascal", gens)
	}
}

func TestDetectDesktopWithoutBattery(t *testing.T) {
	root := t.TempDir()
	write(t, root, "proc/cpuinfo", "vendor_id\t: AuthenticAMD\n")
	write(t, root, "sys/class/dmi/id/chassis_type", "3\n")
	d := Detector{Root: root, Lsblk: func() ([]byte, error) { return []byte(`{"blockdevices":[]}`), nil }}
	f, _ := d.Detect()
	if f.Laptop || f.UEFI || f.CPU != AMD || f.Bluetooth {
		t.Fatalf("desktop facts wrong: %+v", f)
	}
}

func TestVirtualGPU(t *testing.T) {
	root := t.TempDir()
	write(t, root, "proc/cpuinfo", "vendor_id\t: GenuineIntel\n")
	write(t, root, "sys/bus/pci/devices/0000:00:01.0/class", "0x030000\n")
	write(t, root, "sys/bus/pci/devices/0000:00:01.0/vendor", "0x1af4\n")
	write(t, root, "sys/bus/pci/devices/0000:00:01.0/device", "0x1050\n")
	d := Detector{Root: root, Lsblk: func() ([]byte, error) { return []byte(`{"blockdevices":[]}`), nil }}
	f, _ := d.Detect()
	if len(f.GPUs) != 1 || f.GPUs[0].Vendor != Virtual {
		t.Fatalf("virtio-gpu not recognised: %+v", f.GPUs)
	}
}

func TestNvidiaGeneration(t *testing.T) {
	cases := map[uint16]string{
		0x1180: GenOlder,   // GTX 680 (Kepler)
		0x13c2: GenMaxwell, // GTX 970
		0x1b80: GenPascal,  // GTX 1080
		0x1bbb: GenPascal,  // Quadro P3200
		0x1e04: GenTuring,  // RTX 2080 Ti
		0x2684: GenTuring,  // RTX 4090 (Ada, "turing+")
	}
	for id, want := range cases {
		if got := NvidiaGeneration(id); got != want {
			t.Errorf("0x%04x: got %s, want %s", id, got, want)
		}
	}
}

const lsblkSample = `{
  "blockdevices": [
    {"path":"/dev/loop0","type":"loop","model":null,"size":1400000000,"tran":null,"rm":false,"ro":true,"fstype":"squashfs","label":null,"mountpoints":["/run/rootfsbase"]},
    {"path":"/dev/sda","type":"disk","model":"SanDisk Ultra  ","size":32000000000,"tran":"usb","rm":true,"ro":false,"fstype":"iso9660","label":"VOID_LIVE","mountpoints":[null],
      "children":[{"path":"/dev/sda1","type":"part","model":null,"size":2000000000,"tran":null,"rm":true,"ro":false,"fstype":"iso9660","label":"VOID_LIVE","mountpoints":["/run/initramfs/live"]}]},
    {"path":"/dev/nvme0n1","type":"disk","model":"Samsung SSD 970 EVO Plus 1TB","size":1000204886016,"tran":"nvme","rm":false,"ro":false,"fstype":null,"label":null,"mountpoints":[null],
      "children":[
        {"path":"/dev/nvme0n1p1","type":"part","model":null,"size":536870912,"tran":null,"rm":false,"ro":false,"fstype":"vfat","label":null,"mountpoints":[null]},
        {"path":"/dev/nvme0n1p2","type":"part","model":null,"size":999000000000,"tran":null,"rm":false,"ro":false,"fstype":"ext4","label":null,"mountpoints":[null]}]},
    {"path":"/dev/sr0","type":"rom","model":"DVD","size":1073741312,"tran":"sata","rm":true,"ro":false,"fstype":null,"label":null,"mountpoints":[null]},
    {"path":"/dev/zram0","type":"disk","model":null,"size":4000000000,"tran":null,"rm":false,"ro":false,"fstype":"swap","label":null,"mountpoints":["[SWAP]"]}
  ]
}`

func TestParseLsblk(t *testing.T) {
	disks, err := ParseLsblk([]byte(lsblkSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 2 {
		t.Fatalf("want sda and nvme0n1, got %+v", disks)
	}
	usb, nvme := disks[0], disks[1]
	if !usb.LiveMedium || !usb.Removable || usb.Model != "SanDisk Ultra" {
		t.Fatalf("usb stick: %+v", usb)
	}
	if nvme.LiveMedium || nvme.Transport != "nvme" || len(nvme.Partitions) != 2 || nvme.Partitions[1].FSType != "ext4" {
		t.Fatalf("nvme: %+v", nvme)
	}
}

// What `lsblk -J -o NAME,PKNAME,PATH,…` printed on the reference laptop:
// a flat list linked by pkname, including a crypt device under a partition.
const lsblkFlat = `{"blockdevices":[
  {"name":"nvme0n1","pkname":null,"path":"/dev/nvme0n1","type":"disk","model":"WD Blue SN5000 1TB","size":1000204886016,"tran":"nvme","rm":false,"ro":false,"fstype":null,"label":null,"mountpoints":[]},
  {"name":"nvme0n1p1","pkname":"nvme0n1","path":"/dev/nvme0n1p1","type":"part","model":null,"size":1073741824,"tran":"nvme","rm":false,"ro":false,"fstype":"vfat","label":null,"mountpoints":["/boot/efi"]},
  {"name":"nvme0n1p2","pkname":"nvme0n1","path":"/dev/nvme0n1p2","type":"part","model":null,"size":990000000000,"tran":"nvme","rm":false,"ro":false,"fstype":"crypto_LUKS","label":null,"mountpoints":[]},
  {"name":"root","pkname":"nvme0n1p2","path":"/dev/mapper/root","type":"crypt","model":null,"size":989000000000,"tran":null,"rm":false,"ro":false,"fstype":"ext4","label":null,"mountpoints":["/"]}
]}`

func TestParseLsblkFlat(t *testing.T) {
	disks, err := ParseLsblk([]byte(lsblkFlat))
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 1 || len(disks[0].Partitions) != 2 {
		t.Fatalf("want 1 disk with 2 partitions, got %+v", disks)
	}
	if disks[0].Partitions[0].Mountpoint != "/boot/efi" || disks[0].Partitions[1].FSType != "crypto_LUKS" {
		t.Fatalf("partitions: %+v", disks[0].Partitions)
	}
}

func TestPartitionPath(t *testing.T) {
	for disk, want := range map[string]string{
		"/dev/sda":     "/dev/sda2",
		"/dev/vda":     "/dev/vda2",
		"/dev/nvme0n1": "/dev/nvme0n1p2",
		"/dev/mmcblk0": "/dev/mmcblk0p2",
	} {
		if got := PartitionPath(disk, 2); got != want {
			t.Errorf("%s: got %s, want %s", disk, got, want)
		}
	}
}
