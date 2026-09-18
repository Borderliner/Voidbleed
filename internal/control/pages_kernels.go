package control

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

type kernelView int

const (
	kernelInstalled kernelView = iota
	kernelLeftovers
)

type kernelsPage struct {
	view    kernelView
	table   Table
	kernels []system.Kernel
	stale   []string
	loading bool
}

type kernelsLoadedMsg struct {
	kernels []system.Kernel
	stale   []string
	err     error
}

func newKernelsPage() *kernelsPage {
	return &kernelsPage{table: Table{
		Headers: []string{"kernel", "version", ""},
		Widths:  []int{0, 16, 10},
	}}
}

func (p *kernelsPage) Label() string { return "Kernels" }

func (p *kernelsPage) Title() (string, string) {
	if p.view == kernelLeftovers {
		return "Kernels", "trees left behind in /boot"
	}
	return "Kernels", "installed series"
}

func (p *kernelsPage) Load(m *Model) tea.Cmd {
	p.loading = true
	client := m.Client
	return func() tea.Msg {
		ctx := context.Background()
		kernels, err := client.Kernels(ctx)
		stale, _ := client.StaleKernels(ctx)
		return kernelsLoadedMsg{kernels: kernels, stale: stale, err: err}
	}
}

func (p *kernelsPage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case kernelsLoadedMsg:
		p.loading = false
		if msg.err != nil {
			m.Fail(msg.err)
		}
		p.kernels, p.stale = msg.kernels, msg.stale
		p.fill()
		status := plural(len(p.kernels), "series", "series") + " installed"
		if len(p.stale) > 0 {
			status += ", " + plural(len(p.stale), "old tree", "old trees") + " still in /boot"
		}
		m.Status(status)
		return nil
	case tea.KeyPressMsg:
		return p.key(m, msg.String())
	}
	return nil
}

func (p *kernelsPage) key(m *Model, key string) tea.Cmd {
	if p.table.Typing() {
		p.table.Key(key, 8)
		return nil
	}
	switch key {
	case "left", "h", "right", "l":
		p.view = kernelView(1 - int(p.view))
		p.fill()
		return nil
	case "x", "enter":
		row, ok := p.table.Current()
		if !ok {
			return nil
		}
		if p.view == kernelLeftovers {
			return m.Do("remove kernel "+row.ID+" from /boot",
				"Delete the files of "+row.ID+" in /boot? No installed package owns them.",
				true, system.VkpurgeCmd(row.ID))
		}
		k := p.kernel(row.ID)
		// The running kernel and the pinned series are the two a machine
		// cannot do without; neither is removable from here.
		switch {
		case k.Booted:
			m.Status("that is the kernel this machine booted from")
			return nil
		case k.Pinned:
			m.Status(k.Package + " is the series Voidbleed installs; removing it would undo the pin")
			return nil
		}
		return m.Do("remove "+k.Package,
			"Remove "+k.Package+" "+k.Version+"? The running kernel is not affected.",
			true, system.KernelRemoveCmd(k))
	case "a":
		if p.view != kernelLeftovers || len(p.stale) == 0 {
			return nil
		}
		return m.Do("clean /boot",
			"Delete the files of "+plural(len(p.stale), "old kernel", "old kernels")+" in /boot?",
			true, system.VkpurgeAllCmd())
	}
	p.table.Key(key, 8)
	return nil
}

func (p *kernelsPage) kernel(name string) system.Kernel {
	for _, k := range p.kernels {
		if k.Package == name {
			return k
		}
	}
	return system.Kernel{}
}

func (p *kernelsPage) fill() {
	var rows []Row
	if p.view == kernelLeftovers {
		for _, version := range p.stale {
			rows = append(rows, Row{ID: version, Cols: []string{version, "", ""}, Badge: "in /boot"})
		}
		p.table.SetRows(rows)
		return
	}
	for _, k := range p.kernels {
		badge := ""
		switch {
		case k.Booted:
			badge = "booted"
		case k.Pinned:
			badge = "pinned"
		}
		rows = append(rows, Row{
			ID:    k.Package,
			Cols:  []string{k.Package, k.Version, ""},
			Badge: badge,
			Mark:  k.Booted,
			Muted: !k.Booted && !k.Pinned,
		})
	}
	p.table.SetRows(rows)
}

func (p *kernelsPage) View(m *Model, width, height int) string {
	s := m.Styles
	if p.loading {
		return m.spinner() + s.Dim.Render(" reading…")
	}
	head := m.tabs([]string{"installed", "in /boot"}, int(p.view))
	if p.view == kernelLeftovers && len(p.stale) == 0 {
		head += "\n\n" + s.Dim.Render("nothing left behind")
	}
	if p.view == kernelLeftovers && !system.Have("vkpurge") {
		head += "\n\n" + s.Dim.Render("vkpurge is not installed; it is what clears these out")
	}
	list := head + "\n\n" + p.table.View(s, m.Glyphs, m.listWidth(width), height-2)

	detail := ""
	if row, ok := p.table.Current(); ok {
		if p.view == kernelLeftovers {
			detail = row.ID + "\n\nModules and an image left in /boot by a kernel that is no longer " +
				"installed. Removing a kernel package does not take these with it, which is what " +
				"vkpurge is for.\n\nThe running kernel is never listed here."
		} else {
			k := p.kernel(row.ID)
			detail = k.Package + " " + k.Version + "\n\n"
			switch {
			case k.Booted:
				detail += "This is what the machine booted.\n"
			case k.Pinned:
				detail += "The series Voidbleed installs.\n"
			default:
				detail += "Installed, not running.\n"
			}
			if k.Headers {
				detail += "Headers installed, which dkms modules build against.\n"
			}
			if system.KernelMetapackageIgnored() {
				detail += "\nVoid's \"linux\" metapackage is ignored on this machine, so an update " +
					"cannot quietly move the system to another series."
			}
		}
	}
	return m.split(width, height, list, detail)
}

func (p *kernelsPage) Help(m *Model) []Binding {
	help := []Binding{{m.Glyphs.LeftRight, "view"}}
	if p.view == kernelLeftovers {
		return append(help, Binding{"enter", "remove"}, Binding{"a", "remove all"})
	}
	return append(help, Binding{"x", "remove series"})
}
