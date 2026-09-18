package system

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"voidbleed/internal/sys"
)

// Device is one piece of hardware fwupd knows about.
type Device struct {
	Name      string
	Vendor    string
	Version   string
	ID        string
	Summary   string
	Updatable bool
	NewVer    string // version waiting to be installed
	Reboot    bool   // the update only takes effect after a restart
}

type fwDevices struct {
	Devices []struct {
		Name     string   `json:"Name"`
		Vendor   string   `json:"Vendor"`
		Version  string   `json:"Version"`
		DeviceID string   `json:"DeviceId"`
		Summary  string   `json:"Summary"`
		Flags    []string `json:"Flags"`
		Releases []struct {
			Version string   `json:"Version"`
			Flags   []string `json:"Flags"`
		} `json:"Releases"`
	} `json:"Devices"`
}

// FirmwareAvailable reports whether fwupd is installed at all: it is not part
// of a default Voidbleed install, and a machine without it should be told so
// rather than shown an error.
func FirmwareAvailable() bool { return Have("fwupdmgr") }

// Firmware lists the devices fwupd can see, marked with any waiting update.
func (c *Client) Firmware(ctx context.Context) ([]Device, error) {
	devices, err := c.fwupd(ctx, "get-devices")
	if err != nil {
		return nil, err
	}
	// get-updates exits non-zero when there is nothing to do, which is not a
	// failure worth showing.
	updates, _ := c.fwupd(ctx, "get-updates")
	waiting := map[string]struct {
		version string
		reboot  bool
	}{}
	for _, d := range updates.Devices {
		if len(d.Releases) == 0 {
			continue
		}
		r := d.Releases[0]
		waiting[d.DeviceID] = struct {
			version string
			reboot  bool
		}{r.Version, slices.Contains(r.Flags, "is-upgrade") || slices.Contains(d.Flags, "needs-reboot")}
	}

	var out []Device
	for _, d := range devices.Devices {
		dev := Device{
			Name: d.Name, Vendor: d.Vendor, Version: d.Version,
			ID: d.DeviceID, Summary: d.Summary,
			Updatable: slices.Contains(d.Flags, "updatable"),
		}
		if u, ok := waiting[d.DeviceID]; ok {
			dev.NewVer, dev.Reboot = u.version, u.reboot
		}
		out = append(out, dev)
	}
	return out, nil
}

func (c *Client) fwupd(ctx context.Context, sub string) (fwDevices, error) {
	var parsed fwDevices
	out, err := c.output(ctx, "fwupdmgr", sub, "--json")
	if strings.TrimSpace(out) == "" {
		return parsed, err
	}
	if jsonErr := json.Unmarshal([]byte(out), &parsed); jsonErr != nil {
		return parsed, jsonErr
	}
	return parsed, nil
}

// FirmwareRefreshCmd downloads the current metadata from the LVFS.
func FirmwareRefreshCmd() sys.Cmd {
	return sys.Command("fwupdmgr", "refresh", "--force", "-y")
}

// FirmwareUpdateCmd applies waiting updates. Firmware is the one thing here
// that can leave a machine unbootable, so the interface never calls this
// without asking first, and never for every device at once by default.
func FirmwareUpdateCmd(deviceID string) sys.Cmd {
	if deviceID == "" {
		return sys.Command("fwupdmgr", "update", "-y")
	}
	return sys.Command("fwupdmgr", "update", deviceID, "-y")
}
