package control

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

type flatpakPage struct {
	table     Table
	apps      []system.Flatpak
	found     []system.Flatpak
	remotes   []system.FlatpakRemote
	searching bool
	showAll   bool // include runtimes and platforms
	loading   bool
	missing   bool
}

type flatpakLoadedMsg struct {
	apps    []system.Flatpak
	remotes []system.FlatpakRemote
	err     error
}
type flatpakFoundMsg struct {
	apps []system.Flatpak
	err  error
}

func newFlatpakPage() *flatpakPage {
	return &flatpakPage{table: Table{
		Headers: []string{"application", "version", ""},
		Widths:  []int{0, 14, 10},
	}}
}

func (p *flatpakPage) Label() string { return "Flatpak" }

func (p *flatpakPage) Title() (string, string) {
	if p.searching {
		return "Flatpak", "search Flathub"
	}
	return "Flatpak", "applications from Flathub"
}

func (p *flatpakPage) Load(m *Model) tea.Cmd {
	if !system.Have("flatpak") {
		p.missing = true
		return nil
	}
	p.loading = true
	client := m.Client
	return func() tea.Msg {
		ctx := context.Background()
		apps, err := client.Flatpaks(ctx)
		remotes, _ := client.FlatpakRemotes(ctx)
		return flatpakLoadedMsg{apps: apps, remotes: remotes, err: err}
	}
}

func (p *flatpakPage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case flatpakLoadedMsg:
		p.loading = false
		if msg.err != nil {
			m.Fail(msg.err)
		}
		p.apps, p.remotes = msg.apps, msg.remotes
		p.fill()
		m.Status(p.summary())
		return nil
	case flatpakFoundMsg:
		p.loading = false
		if msg.err != nil {
			m.Fail(msg.err)
		}
		p.found = msg.apps
		p.fill()
		return nil
	case tea.KeyPressMsg:
		return p.key(m, msg.String())
	}
	return nil
}

func (p *flatpakPage) key(m *Model, key string) tea.Cmd {
	if p.missing {
		if key == "i" {
			return m.Do("install flatpak", "", true, system.InstallCmd("flatpak"))
		}
		return nil
	}
	if p.table.Typing() {
		if p.table.Key(key, 10) {
			if p.searching && key == "enter" {
				return p.search(m)
			}
			return nil
		}
		return nil
	}
	switch key {
	case "left", "h", "right", "l":
		p.searching = !p.searching
		p.fill()
		return nil
	case "a":
		p.showAll = !p.showAll
		p.fill()
		return nil
	case "i", "enter":
		row, ok := p.table.Current()
		if !ok || !p.searching {
			return nil
		}
		remote := "flathub"
		if len(p.remotes) > 0 {
			remote = p.remotes[0].Name
		}
		return m.Do("install "+row.ID, "", true, system.FlatpakInstallCmd(remote, row.ID))
	case "x":
		row, ok := p.table.Current()
		if !ok || p.searching {
			return nil
		}
		return m.Do("remove "+row.ID, "Remove "+row.ID+"?", true, system.FlatpakRemoveCmd(row.ID))
	case "u":
		return m.Do("update flatpaks", "", true, system.FlatpakUpdateCmd())
	case "c":
		return m.Do("remove unused runtimes", "Remove runtimes no application uses?", true, system.FlatpakPruneCmd())
	}
	p.table.Key(key, 10)
	return nil
}

func (p *flatpakPage) fill() {
	list := p.apps
	if p.searching {
		list = p.found
	}
	installed := map[string]bool{}
	for _, a := range p.apps {
		installed[a.ID] = true
	}
	rows := make([]Row, 0, len(list))
	for _, app := range list {
		if app.Runtime && !p.showAll && !p.searching {
			continue
		}
		badge := ""
		switch {
		case app.Update:
			badge = "update"
		case app.Runtime:
			badge = "runtime"
		case p.searching && installed[app.ID]:
			badge = "installed"
		case app.Installation == "user":
			badge = "user"
		}
		name := app.Name
		if name == "" {
			name = app.ID
		}
		rows = append(rows, Row{
			ID:    app.ID,
			Cols:  []string{name, app.Version, ""},
			Badge: badge,
			Muted: app.Runtime,
		})
	}
	p.table.SetRows(rows)
}

func (p *flatpakPage) summary() string {
	apps, updates := 0, 0
	for _, a := range p.apps {
		if !a.Runtime {
			apps++
		}
		if a.Update {
			updates++
		}
	}
	return plural(apps, "application", "applications") + ", " + plural(updates, "update", "updates") + " waiting"
}

func (p *flatpakPage) search(m *Model) tea.Cmd {
	term := p.table.Filtering()
	if term == "" {
		return nil
	}
	p.loading = true
	client := m.Client
	return func() tea.Msg {
		apps, err := client.SearchFlatpak(context.Background(), term)
		return flatpakFoundMsg{apps: apps, err: err}
	}
}

func (p *flatpakPage) View(m *Model, width, height int) string {
	if p.missing {
		return m.Styles.Muted.Render("Flatpak is not installed on this machine.") + "\n\n" +
			m.Styles.Dim.Render("press i to install it, then reload with r")
	}
	view := "installed"
	if p.searching {
		view = "search"
	}
	head := m.tabs([]string{"installed", "search"}, map[string]int{"installed": 0, "search": 1}[view])
	if p.searching && p.table.Filtering() == "" {
		head += "\n\n" + m.Styles.Dim.Render("press / and type, then enter to search Flathub")
	}
	body := p.table.View(m.Styles, m.Glyphs, m.listWidth(width), height-2)
	if p.loading {
		body = m.spinner() + m.Styles.Dim.Render(" reading…")
	}
	var detail string
	if row, ok := p.table.Current(); ok {
		detail = row.ID
		for _, a := range append(p.apps, p.found...) {
			if a.ID == row.ID {
				detail = a.Name + "\n\n" + a.ID + "\n" + a.Branch + " " + a.Origin
				if a.Version != "" {
					detail += "\nversion " + a.Version
				}
				if a.Installation != "" {
					detail += "\ninstalled for the " + a.Installation
				}
				break
			}
		}
	}
	return m.split(width, height, head+"\n\n"+body, detail)
}

func (p *flatpakPage) Help(m *Model) []Binding {
	if p.missing {
		return []Binding{{"i", "install flatpak"}}
	}
	if p.searching {
		return []Binding{{m.Glyphs.LeftRight, "view"}, {"/", "search"}, {"i", "install"}}
	}
	return []Binding{
		{m.Glyphs.LeftRight, "view"}, {"u", "update all"}, {"x", "remove"},
		{"a", strings.TrimSpace(map[bool]string{true: "hide runtimes", false: "show runtimes"}[p.showAll])},
	}
}
