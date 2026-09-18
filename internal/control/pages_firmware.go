package control

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

type firmwarePage struct {
	table   Table
	devices []system.Device
	loading bool
	missing bool
}

type firmwareLoadedMsg struct {
	devices []system.Device
	err     error
}

func newFirmwarePage() *firmwarePage {
	return &firmwarePage{table: Table{
		Headers: []string{"device", "version", ""},
		Widths:  []int{0, 18, 10},
	}}
}

func (p *firmwarePage) Label() string { return "Firmware" }
func (p *firmwarePage) Title() (string, string) {
	return "Firmware", "updates from the LVFS, through fwupd"
}

func (p *firmwarePage) Load(m *Model) tea.Cmd {
	if !system.FirmwareAvailable() {
		p.missing = true
		return nil
	}
	p.missing = false
	p.loading = true
	client := m.Client
	return func() tea.Msg {
		devices, err := client.Firmware(context.Background())
		return firmwareLoadedMsg{devices: devices, err: err}
	}
}

func (p *firmwarePage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case firmwareLoadedMsg:
		p.loading = false
		if msg.err != nil {
			m.Fail(msg.err)
		}
		p.devices = msg.devices
		p.fill()
		waiting := 0
		for _, d := range p.devices {
			if d.NewVer != "" {
				waiting++
			}
		}
		// "0 updates" reads like a failure; say what it means instead.
		if waiting == 0 {
			m.Status(plural(len(p.devices), "device", "devices") +
				", nothing waiting — press f to fetch current metadata from the LVFS first")
		} else {
			m.Status(plural(len(p.devices), "device", "devices") + ", " +
				plural(waiting, "update", "updates") + " waiting")
		}
		return nil
	case tea.KeyPressMsg:
		return p.key(m, msg.String())
	}
	return nil
}

func (p *firmwarePage) key(m *Model, key string) tea.Cmd {
	if p.missing {
		if key == "i" {
			return m.Do("install fwupd", "", true, system.InstallCmd("fwupd"))
		}
		return nil
	}
	if p.table.Typing() {
		p.table.Key(key, 10)
		return nil
	}
	switch key {
	case "f":
		return m.Do("refresh firmware metadata", "", true, system.FirmwareRefreshCmd())
	case "u", "enter":
		row, ok := p.table.Current()
		if !ok {
			return nil
		}
		device := p.device(row.ID)
		if device.NewVer == "" {
			m.Status("no update waiting for this device")
			return nil
		}
		// Firmware is the one thing here that can leave a machine unable to
		// boot, so the device and the version are both spelled out.
		return m.Do("update "+device.Name,
			"Write firmware "+device.NewVer+" to "+device.Name+"?\n"+
				"Keep the machine powered until it finishes.",
			true, system.FirmwareUpdateCmd(device.ID))
	}
	p.table.Key(key, 10)
	return nil
}

func (p *firmwarePage) device(id string) system.Device {
	for _, d := range p.devices {
		if d.ID == id {
			return d
		}
	}
	return system.Device{}
}

func (p *firmwarePage) fill() {
	rows := make([]Row, 0, len(p.devices))
	for _, d := range p.devices {
		// fwupd reports internal plumbing with no name at all; those rows tell
		// a person nothing.
		if d.Name == "" {
			continue
		}
		badge := ""
		switch {
		case d.NewVer != "":
			badge = "→ " + d.NewVer
		case !d.Updatable:
			badge = "fixed"
		}
		rows = append(rows, Row{
			ID:    d.ID,
			Cols:  []string{d.Name, d.Version, ""},
			Badge: badge,
			Muted: !d.Updatable,
		})
	}
	p.table.SetRows(rows)
}

func (p *firmwarePage) View(m *Model, width, height int) string {
	s := m.Styles
	if p.missing {
		return s.Muted.Render("fwupd is not installed, so this machine cannot be told about firmware updates.") +
			"\n\n" + s.Dim.Render("press i to install it, then reload with r")
	}
	if p.loading {
		return m.spinner() + s.Dim.Render(" asking fwupd…")
	}
	list := p.table.View(s, m.Glyphs, m.listWidth(width), height)
	var detail string
	if row, ok := p.table.Current(); ok {
		d := p.device(row.ID)
		detail = d.Name + "\n\n"
		if d.Vendor != "" {
			detail += d.Vendor + "\n"
		}
		detail += "version " + d.Version + "\n"
		if d.NewVer != "" {
			detail += "update " + d.NewVer + " available\n"
		}
		if d.Summary != "" {
			detail += "\n" + d.Summary + "\n"
		}
		if !d.Updatable {
			detail += "\nthis device cannot be updated by fwupd"
		}
	}
	return m.split(width, height, list, detail)
}

func (p *firmwarePage) Help(m *Model) []Binding {
	if p.missing {
		return []Binding{{"i", "install fwupd"}}
	}
	return []Binding{{"f", "refresh"}, {"u", "update device"}, {"/", "filter"}}
}
