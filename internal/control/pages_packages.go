package control

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

type pkgMode int

const (
	modeInstalled pkgMode = iota
	modeUpdates
	modeOrphans
	modeSearch
)

var pkgViews = []string{"installed", "updates", "orphans", "search"}

func (p pkgMode) String() string { return pkgViews[p] }

type packagesPage struct {
	mode      pkgMode
	table     Table
	installed []system.Package
	updates   []system.Package
	found     []system.Package
	marked    map[string]bool
	detail    string
	detailFor string
	details   map[string]string // what xbps said about a package, kept
	loading   bool
}

type pkgLoadedMsg struct {
	installed []system.Package
	updates   []system.Package
	err       error
}
type pkgFoundMsg struct {
	packages []system.Package
	err      error
}
type pkgDetailMsg struct {
	name, text string
}

// pkgDetailDueMsg fires when the cursor has sat on a row long enough to be
// worth asking xbps about it.
type pkgDetailDueMsg struct{ name string }

func newPackagesPage() *packagesPage {
	return &packagesPage{
		marked:  map[string]bool{},
		details: map[string]string{},
		table: Table{
			Headers: []string{"package", "version", ""},
			Widths:  []int{0, 16, 10},
		},
	}
}

func (p *packagesPage) Label() string { return "Packages" }

func (p *packagesPage) Title() (string, string) {
	switch p.mode {
	case modeUpdates:
		return "Packages", "updates waiting"
	case modeOrphans:
		return "Packages", "installed, but nothing needs them"
	case modeSearch:
		return "Packages", "search the repositories"
	default:
		return "Packages", "installed on this machine"
	}
}

func (p *packagesPage) Load(m *Model) tea.Cmd {
	p.loading = true
	client := m.Client
	return func() tea.Msg {
		ctx := context.Background()
		installed, err := client.Installed(ctx)
		updates, _ := client.Updates(ctx)
		return pkgLoadedMsg{installed: installed, updates: updates, err: err}
	}
}

func (p *packagesPage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pkgLoadedMsg:
		p.loading = false
		if msg.err != nil {
			m.Fail(msg.err)
		}
		p.installed, p.updates = msg.installed, msg.updates
		// Mark the installed rows that have a newer version waiting.
		waiting := map[string]string{}
		for _, u := range msg.updates {
			waiting[u.Name] = u.NewVersion
		}
		for i, pkg := range p.installed {
			p.installed[i].NewVersion = waiting[pkg.Name]
		}
		p.fill()
		m.Status(plural(len(p.installed), "package", "packages") + " installed, " +
			plural(len(p.updates), "update", "updates") + " waiting")
		return p.detailCmd(m)
	case pkgFoundMsg:
		p.loading = false
		if msg.err != nil {
			m.Fail(msg.err)
		}
		p.found = msg.packages
		p.fill()
		return p.detailCmd(m)
	case pkgDetailDueMsg:
		if msg.name != p.detailFor {
			return nil // the cursor moved on while the timer ran
		}
		if text, known := p.details[msg.name]; known {
			p.detail = text
			return nil
		}
		client, name := m.Client, msg.name
		return func() tea.Msg {
			out, err := client.Show(context.Background(), name)
			if err != nil {
				return pkgDetailMsg{name: name, text: "no description available"}
			}
			return pkgDetailMsg{name: name, text: describe(out)}
		}
	case pkgDetailMsg:
		p.details[msg.name] = msg.text
		if msg.name == p.detailFor {
			p.detail = msg.text
		}
		return nil
	case tea.KeyPressMsg:
		return p.key(m, msg.String())
	}
	return nil
}

func (p *packagesPage) key(m *Model, key string) tea.Cmd {
	// While the filter is being typed it owns every printable key, so a
	// package called "x" cannot be removed by searching for it.
	if p.table.Typing() {
		if p.table.Key(key, 10) {
			if p.mode == modeSearch && key == "enter" {
				return p.search(m)
			}
			return p.detailCmd(m)
		}
		return nil
	}

	switch key {
	case "left", "h":
		p.mode = pkgMode((int(p.mode) + len(pkgViews) - 1) % len(pkgViews))
		p.fill()
		return p.detailCmd(m)
	case "right", "l":
		p.mode = pkgMode((int(p.mode) + 1) % len(pkgViews))
		p.fill()
		return p.detailCmd(m)
	case " ":
		if row, ok := p.table.Current(); ok {
			p.marked[row.ID] = !p.marked[row.ID]
			p.fill()
		}
		return nil
	case "i", "enter":
		names := p.targets()
		if len(names) == 0 {
			return nil
		}
		if p.mode == modeInstalled {
			return nil // already here
		}
		return m.Do("install "+strings.Join(names, " "), "", true, system.InstallCmd(names...))
	case "x", "delete":
		names := p.targets()
		if len(names) == 0 || p.mode == modeSearch {
			return nil
		}
		return m.Do("remove "+strings.Join(names, " "),
			"Remove "+strings.Join(names, ", ")+" and anything that depended on it?",
			true, system.RemoveCmd(names[0], true))
	case "u":
		if len(p.updates) == 0 {
			m.Status("nothing to update")
			return nil
		}
		return m.Do("update the system",
			plural(len(p.updates), "package", "packages")+" will be replaced. Continue?",
			true, system.UpdateCmd())
	case "s":
		return m.Do("sync repositories", "", true, system.SyncCmd())
	case "c":
		return m.Do("clean up", "Remove orphaned packages and the download cache?", true, system.CleanUpCmd())
	}
	if p.table.Key(key, 10) {
		return p.detailCmd(m)
	}
	return nil
}

// targets is what an action applies to: everything marked, or the row under
// the cursor when nothing is.
func (p *packagesPage) targets() []string {
	var names []string
	for _, row := range p.rows() {
		if p.marked[row.ID] {
			names = append(names, row.ID)
		}
	}
	if len(names) > 0 {
		return names
	}
	if row, ok := p.table.Current(); ok {
		return []string{row.ID}
	}
	return nil
}

func (p *packagesPage) rows() []Row {
	var list []system.Package
	switch p.mode {
	case modeUpdates:
		list = p.updates
	case modeOrphans:
		for _, pkg := range p.installed {
			if pkg.Orphan {
				list = append(list, pkg)
			}
		}
	case modeSearch:
		list = p.found
	default:
		list = p.installed
	}
	rows := make([]Row, 0, len(list))
	for _, pkg := range list {
		version := pkg.Version
		badge := ""
		switch {
		case pkg.NewVersion != "" && version != "":
			badge = "→ " + pkg.NewVersion
		case pkg.NewVersion != "":
			version = pkg.NewVersion
			badge = "update"
		case pkg.Installed && p.mode == modeSearch:
			badge = "installed"
		case pkg.Orphan:
			badge = "orphan"
		case !pkg.Manual && p.mode == modeInstalled:
			badge = "dependency"
		}
		rows = append(rows, Row{
			ID:    pkg.Name,
			Cols:  []string{pkg.Name, version, ""},
			Badge: badge,
			Mark:  p.marked[pkg.Name],
			Muted: p.mode == modeInstalled && !pkg.Manual,
		})
	}
	return rows
}

func (p *packagesPage) fill() { p.table.SetRows(p.rows()) }

func (p *packagesPage) search(m *Model) tea.Cmd {
	term := p.table.Filtering()
	if term == "" {
		return nil
	}
	p.loading = true
	client := m.Client
	return func() tea.Msg {
		found, err := client.Search(context.Background(), term)
		return pkgFoundMsg{packages: found, err: err}
	}
}

// detailCmd shows what xbps knows about the highlighted package, which is
// better than anything this program could summarise itself.
//
// Asking for every row the cursor passes over means a process per keypress,
// and holding an arrow key then feels like wading through mud -- so answers
// are kept, and a new question waits until the cursor settles.
func (p *packagesPage) detailCmd(m *Model) tea.Cmd {
	row, ok := p.table.Current()
	if !ok {
		p.detail, p.detailFor = "", ""
		return nil
	}
	if row.ID == p.detailFor {
		return nil
	}
	p.detailFor = row.ID
	if text, known := p.details[row.ID]; known {
		p.detail = text
		return nil
	}
	// Keep the pane the same width while the answer is on its way, so the
	// layout does not jump under the cursor.
	p.detail = row.ID + "\n\nreading…"
	name := row.ID
	return tea.Tick(180*time.Millisecond, func(time.Time) tea.Msg {
		return pkgDetailDueMsg{name: name}
	})
}

// describe keeps the lines of xbps-query -R worth reading on a narrow pane.
func describe(out string) string {
	keep := map[string]bool{
		"pkgver": true, "short_desc": true, "maintainer": true, "license": true,
		"homepage": true, "installed_size": true, "repository": true, "state": true,
	}
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !keep[strings.TrimSpace(key)] {
			continue
		}
		b.WriteString(strings.TrimSpace(key) + "\n  " + strings.TrimSpace(value) + "\n")
	}
	if b.Len() == 0 {
		return strings.TrimSpace(out)
	}
	return b.String()
}

func (p *packagesPage) View(m *Model, width, height int) string {
	tabs := m.tabs(pkgViews, int(p.mode))
	switch {
	case p.mode == modeSearch && p.table.Filtering() == "":
		tabs += "\n\n" + m.Styles.Dim.Render("press / and type, then enter to search the repositories")
	case p.mode == modeOrphans && len(p.table.Rows) == 0:
		tabs += "\n\n" + m.Styles.Dim.Render("nothing is orphaned")
	}
	list := tabs + "\n\n" + p.table.View(m.Styles, m.Glyphs, m.listWidth(width), height-2)
	if p.loading {
		list = tabs + "\n\n" + m.spinner() + m.Styles.Dim.Render(" reading…")
	}
	return m.split(width, height, list, p.detail)
}

func (p *packagesPage) Help(m *Model) []Binding {
	help := []Binding{{m.Glyphs.LeftRight, "view"}, {"/", "filter"}, {"space", "mark"}}
	switch p.mode {
	case modeSearch:
		help = append(help, Binding{"i", "install"})
	case modeUpdates:
		help = append(help, Binding{"u", "update all"})
	case modeOrphans:
		help = append(help, Binding{"x", "remove"}, Binding{"c", "remove them all"})
	default:
		help = append(help, Binding{"x", "remove"}, Binding{"u", "update all"}, Binding{"c", "clean"})
	}
	return help
}

// tabs draws the little row of view names each list page has.
func (m *Model) tabs(names []string, active int) string {
	parts := make([]string, len(names))
	for i, name := range names {
		if i == active {
			parts[i] = m.Styles.Selected.Render(" " + name + " ")
		} else {
			parts[i] = m.Styles.Dim.Render(" " + name + " ")
		}
	}
	return strings.Join(parts, m.Styles.Dim.Render(m.Glyphs.Sep))
}
