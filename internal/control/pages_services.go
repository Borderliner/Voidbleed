package control

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

type servicesPage struct {
	table    Table
	services []system.Service
	onlyOn   bool // hide services that are not enabled
	loading  bool
	// runit keeps its supervise sockets root-only, so the live state is only
	// known once something has been done with a password.
	haveState bool
}

type servicesLoadedMsg struct {
	services  []system.Service
	haveState bool
	err       error
}

func newServicesPage() *servicesPage {
	// The name alone says nothing: "socklog-unix" and "dbus" mean something
	// only if you already know. What each one is goes next to it, and the
	// state becomes the badge on the right.
	return &servicesPage{table: Table{
		Headers: []string{"service", "what it is", ""},
		Widths:  []int{22, 0, 9},
	}}
}

func (p *servicesPage) Label() string           { return "Services" }
func (p *servicesPage) Title() (string, string) { return "Services", "runit, from /etc/sv" }

func (p *servicesPage) Load(m *Model) tea.Cmd {
	p.loading = true
	client := m.Client
	// Asking runit for state needs root. When the program already has it, or
	// sudo is still unlocked, read it; otherwise show what the symlinks say.
	canAsk := !client.NeedsPassword(context.Background())
	return func() tea.Msg {
		ctx := context.Background()
		services, err := client.Services(ctx)
		state := false
		if canAsk && err == nil {
			services = client.Status(ctx, services)
			state = true
		}
		return servicesLoadedMsg{services: services, haveState: state, err: err}
	}
}

func (p *servicesPage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case servicesLoadedMsg:
		p.loading = false
		if msg.err != nil {
			m.Fail(msg.err)
		}
		p.services, p.haveState = msg.services, msg.haveState
		p.fill()
		enabled := 0
		for _, s := range p.services {
			if s.Enabled {
				enabled++
			}
		}
		m.Status(plural(enabled, "service", "services") + " enabled of " + itoa(len(p.services)))
		return nil
	case tea.KeyPressMsg:
		return p.key(m, msg.String())
	}
	return nil
}

func (p *servicesPage) key(m *Model, key string) tea.Cmd {
	if p.table.Typing() {
		p.table.Key(key, 10)
		return nil
	}
	row, has := p.table.Current()
	switch key {
	case "o":
		p.onlyOn = !p.onlyOn
		p.fill()
		return nil
	case "e", "enter":
		if !has {
			return nil
		}
		if p.enabled(row.ID) {
			return m.Do("disable "+row.ID,
				"Stop "+row.ID+" now and keep it from starting at the next boot?",
				true, system.DisableCmd(row.ID))
		}
		return m.Do("enable "+row.ID, "", true, system.EnableCmd(row.ID))
	case "s":
		if !has {
			return nil
		}
		return m.Do("start "+row.ID, "", true, system.ServiceCmd("up", row.ID))
	case "t":
		if !has {
			return nil
		}
		return m.Do("stop "+row.ID, "Stop "+row.ID+" now?", true, system.ServiceCmd("down", row.ID))
	case "R":
		if !has {
			return nil
		}
		return m.Do("restart "+row.ID, "", true, system.ServiceCmd("restart", row.ID))
	}
	p.table.Key(key, 10)
	return nil
}

func (p *servicesPage) enabled(name string) bool {
	for _, s := range p.services {
		if s.Name == name {
			return s.Enabled
		}
	}
	return false
}

func (p *servicesPage) fill() {
	rows := make([]Row, 0, len(p.services))
	for _, s := range p.services {
		if p.onlyOn && !s.Enabled {
			continue
		}
		state := "disabled"
		switch {
		case s.Enabled && s.Running():
			state = "running"
		case s.Enabled && s.State == "down":
			state = "stopped"
		case s.Enabled:
			state = "enabled"
		}
		rows = append(rows, Row{
			ID:    s.Name,
			Cols:  []string{s.Name, s.About, ""},
			Badge: state,
			Mark:  s.Enabled,
			Muted: !s.Enabled,
		})
	}
	p.table.SetRows(rows)
}

func (p *servicesPage) View(m *Model, width, height int) string {
	if p.loading {
		return m.spinner() + m.Styles.Dim.Render(" reading…")
	}
	note := ""
	if !p.haveState {
		note = m.Styles.Dim.Render("runit only tells root what is running; any action below asks for the password and the live state appears after it") + "\n\n"
	}
	list := note + p.table.View(m.Styles, m.Glyphs, m.listWidth(width), height-lineCount(note))

	var detail string
	if row, ok := p.table.Current(); ok {
		for _, s := range p.services {
			if s.Name != row.ID {
				continue
			}
			detail = s.Name + "\n"
			if s.About != "" {
				detail += s.About + "\n"
			}
			detail += "\n"
			if s.Enabled {
				detail += "enabled: starts at boot\n"
			} else {
				detail += "not enabled\n"
			}
			if s.State != "" {
				detail += "state: " + s.State + "\n"
				if s.PID != "" {
					detail += "pid: " + s.PID + "\n"
				}
				if s.Since != "" {
					detail += "for: " + s.Since + "\n"
				}
			}
			if s.Package != "" {
				detail += "from: " + s.Package + "\n"
			}
			detail += "\ndefinition: " + system.ServiceDir + "/" + s.Name
			break
		}
	}
	return m.split(width, height, list, detail)
}

func (p *servicesPage) Help(m *Model) []Binding {
	return []Binding{
		{"e", "enable/disable"}, {"s", "start"}, {"t", "stop"}, {"R", "restart"},
		{"o", "only enabled"}, {"/", "filter"},
	}
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := 1
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}
