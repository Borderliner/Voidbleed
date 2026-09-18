package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Overview is what the machine looks like at a glance, and what is asking for
// attention.
type Overview struct {
	Hostname string
	Kernel   string
	Uptime   string
	CPU      string
	Memory   string
	Disk     string
	Root     string // filesystem of /

	Packages       int
	Manual         int
	Orphans        int
	Updates        int
	Flatpaks       int
	FlatpakUpdates int
	CacheSize      string
	CacheBytes     int64
	Services       int
	Stale          []string // kernels left in /boot that nothing needs
	Counted        bool     // the counts above have been read
}

// MachineFacts is everything that can be read straight from the kernel, with
// no command to run and nothing to wait for. The overview draws these
// immediately and fills in the counts when they arrive.
func MachineFacts() Overview {
	return Overview{
		Hostname: hostname(),
		Kernel:   strings.TrimSpace(readFile("/proc/sys/kernel/osrelease")),
		Uptime:   uptime(),
		CPU:      cpuModel(),
		Memory:   memory(),
		Root:     RootFilesystem(),
		Disk:     diskUsage("/"),
	}
}

// Counts asks xbps, flatpak and runit what they have. Checking for updates
// reaches the network, so this is the slow half.
func (c *Client) Counts(ctx context.Context) Overview {
	var o Overview
	if pkgs, err := c.Installed(ctx); err == nil {
		o.Packages = len(pkgs)
		for _, p := range pkgs {
			if p.Manual {
				o.Manual++
			}
			if p.Orphan {
				o.Orphans++
			}
		}
	}
	if updates, err := c.Updates(ctx); err == nil {
		o.Updates = len(updates)
	}
	if Have("flatpak") {
		if apps, err := c.Flatpaks(ctx); err == nil {
			for _, a := range apps {
				if !a.Runtime {
					o.Flatpaks++
				}
				if a.Update {
					o.FlatpakUpdates++
				}
			}
		}
	}
	o.CacheBytes = dirSize(CacheDir)
	o.CacheSize = humanBytes(o.CacheBytes)
	if services, err := c.Services(ctx); err == nil {
		for _, s := range services {
			if s.Enabled {
				o.Services++
			}
		}
	}
	o.Stale, _ = c.StaleKernels(ctx)
	o.Counted = true
	return o
}

// RootFilesystem is the type of the filesystem mounted at /.
func RootFilesystem() string {
	for _, line := range strings.Split(readFile("/proc/self/mounts"), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == "/" {
			return fields[2]
		}
	}
	return ""
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "void"
	}
	return name
}

func readFile(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(body)
}

func uptime() string {
	seconds, _, _ := strings.Cut(strings.TrimSpace(readFile("/proc/uptime")), " ")
	value, err := strconv.ParseFloat(seconds, 64)
	if err != nil {
		return ""
	}
	d := time.Duration(value) * time.Second
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d days", int(d.Hours())/24)
	}
}

func cpuModel() string {
	for _, line := range strings.Split(readFile("/proc/cpuinfo"), "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "model name" {
			return tidyCPU(value)
		}
	}
	return ""
}

// tidyCPU drops the noise vendors put in the model string, which is most of
// it: "Intel(R) Xeon(R) E-2176M  CPU @ 2.70GHz" is a column and a half of
// trademark symbols wrapped around a name and a number.
func tidyCPU(model string) string {
	for _, noise := range []string{"(R)", "(TM)", "(tm)", "CPU", "Processor", "@"} {
		model = strings.ReplaceAll(model, noise, " ")
	}
	return strings.Join(strings.Fields(model), " ")
}

func memory() string {
	var total, available int64
	for _, line := range strings.Split(readFile("/proc/meminfo"), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "kB")), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			total = kb * 1024
		case "MemAvailable":
			available = kb * 1024
		}
	}
	if total == 0 {
		return ""
	}
	return humanBytes(total-available) + " of " + humanBytes(total)
}

func diskUsage(path string) string {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return ""
	}
	total := int64(fs.Blocks) * fs.Bsize
	free := int64(fs.Bavail) * fs.Bsize
	used := total - free
	percent := 0
	if total > 0 {
		percent = int(used * 100 / total)
	}
	return fmt.Sprintf("%s of %s (%d%%)", humanBytes(used), humanBytes(total), percent)
}

// dirSize adds up a directory without following it anywhere else; the package
// cache is flat, so this stays cheap.
func dirSize(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	value, exp := float64(n)/unit, 0
	for value >= unit && exp < 3 {
		value /= unit
		exp++
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + [...]string{"KB", "MB", "GB", "TB"}[exp]
}

// ConfigPath is where Voidbleed's own xbps rules live, which is how the
// kernel pin is expressed.
var ConfigPath = filepath.Join("/usr/share/xbps.d", "05-voidbleed-kernel.conf")
