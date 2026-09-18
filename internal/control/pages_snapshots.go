package control

import (
	"context"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

// The snapshots page only exists on a machine with a btrfs root; see
// system.SnapshotsAvailable and where the pages are put together.
type snapshotsPage struct {
	table   Table
	snaps   []system.Snapshot
	input   textinput.Model
	naming  bool
	loading bool
}

type snapshotsLoadedMsg struct {
	snaps []system.Snapshot
	err   error
}

func newSnapshotsPage() *snapshotsPage {
	in := textinput.New()
	in.Placeholder = "before the kernel update"
	in.CharLimit = 40
	return &snapshotsPage{
		input: in,
		table: Table{Headers: []string{"snapshot", "taken", ""}, Widths: []int{0, 18, 9}},
	}
}

// Typing reports whether a filter or a field has the keyboard.
func (p *snapshotsPage) Typing() bool { return p.naming || p.table.Typing() }

func (p *snapshotsPage) Label() string { return "Snapshots" }
func (p *snapshotsPage) Title() (string, string) {
	return "Snapshots", "read-only copies of the root subvolume"
}

func (p *snapshotsPage) Load(m *Model) tea.Cmd {
	p.loading = true
	client := m.Client
	return func() tea.Msg {
		snaps, err := client.Snapshots(context.Background())
		return snapshotsLoadedMsg{snaps: snaps, err: err}
	}
}

func (p *snapshotsPage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case snapshotsLoadedMsg:
		p.loading = false
		if msg.err != nil {
			m.Fail(msg.err)
		}
		p.snaps = msg.snaps
		p.fill()
		m.Status(plural(len(p.snaps), "snapshot", "snapshots") + " in " + system.SnapshotDir)
		return nil
	case tea.KeyPressMsg:
		return p.key(m, msg)
	}
	return nil
}

func (p *snapshotsPage) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if p.naming {
		switch key {
		case "esc":
			p.naming = false
			p.input.SetValue("")
			return nil
		case "enter":
			name := system.SnapshotName(p.input.Value())
			p.naming = false
			p.input.SetValue("")
			return m.Do("take snapshot "+name, "", true, system.SnapshotCreateCmd(name))
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return cmd
	}
	if p.table.Typing() {
		p.table.Key(key, 10)
		return nil
	}
	switch key {
	case "n":
		p.naming = true
		p.input.Focus()
		return textinput.Blink
	case "x":
		row, ok := p.table.Current()
		if !ok {
			return nil
		}
		return m.Do("delete snapshot "+row.ID,
			"Delete "+row.ID+"? What it holds cannot be got back afterwards.",
			true, system.SnapshotDeleteCmd(row.ID))
	}
	p.table.Key(key, 10)
	return nil
}

func (p *snapshotsPage) fill() {
	rows := make([]Row, 0, len(p.snaps))
	for _, s := range p.snaps {
		rows = append(rows, Row{
			ID:    s.Name,
			Cols:  []string{s.Name, s.Made.Format("2006-01-02 15:04"), ""},
			Badge: age(s.Made),
		})
	}
	p.table.SetRows(rows)
}

// age is how long ago something happened, in the roughest useful terms.
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return "just now"
	case d < 48*time.Hour:
		return itoa(int(d.Hours())) + "h ago"
	default:
		return itoa(int(d.Hours())/24) + "d ago"
	}
}

func (p *snapshotsPage) View(m *Model, width, height int) string {
	s := m.Styles
	if p.loading {
		return m.spinner() + s.Dim.Render(" reading…")
	}
	body := p.table.View(s, m.Glyphs, m.listWidth(width), height)
	switch {
	case p.naming:
		p.input.SetWidth(min(m.listWidth(width)-8, 40))
		body = s.Label.Render("label") + p.input.View() + "\n\n" +
			s.Dim.Render("optional; the time is always part of the name\nenter to take it, esc to change your mind")
	case len(p.snaps) == 0:
		body = s.Muted.Render("No snapshots yet.") + "\n\n" +
			s.Dim.Render("press n to take one — it costs almost nothing until the system changes")
	}
	return m.split(width, height, body, system.SnapshotRestoreHint)
}

func (p *snapshotsPage) Help(m *Model) []Binding {
	if p.naming {
		return []Binding{{"enter", "take it"}, {"esc", "cancel"}}
	}
	return []Binding{{"n", "new snapshot"}, {"x", "delete"}, {"/", "filter"}}
}
