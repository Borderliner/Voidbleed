package control

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

type firewallPage struct {
	state   system.Firewall
	table   Table
	input   textinput.Model
	adding  bool
	loading bool
}

type firewallLoadedMsg struct{ state system.Firewall }

func newFirewallPage() *firewallPage {
	in := textinput.New()
	in.Placeholder = "22/tcp, 80, ssh, from 192.168.1.0/24 to any port 445"
	in.CharLimit = 120
	return &firewallPage{
		input: in,
		table: Table{Headers: []string{"to", "action", "from"}, Widths: []int{0, 12, 20}},
	}
}

func (p *firewallPage) Label() string           { return "Firewall" }
func (p *firewallPage) Title() (string, string) { return "Firewall", "ufw, the simple one" }

func (p *firewallPage) Load(m *Model) tea.Cmd {
	p.loading = true
	client := m.Client
	return func() tea.Msg {
		return firewallLoadedMsg{state: client.FirewallState(context.Background())}
	}
}

func (p *firewallPage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case firewallLoadedMsg:
		p.loading = false
		p.state = msg.state
		rows := make([]Row, 0, len(p.state.Rules))
		for _, r := range p.state.Rules {
			rows = append(rows, Row{ID: r.Number, Cols: []string{r.To, r.Action, r.From}})
		}
		p.table.SetRows(rows)
		switch {
		case !p.state.Installed:
			m.Status("no firewall installed")
		case p.state.Unknown:
			m.Status("state needs the password: press e, or any action, to unlock")
		case p.state.Active:
			m.Status("active, " + plural(len(p.state.Rules), "rule", "rules"))
		default:
			m.Status("installed but not filtering")
		}
		return nil
	case tea.KeyPressMsg:
		return p.key(m, msg)
	}
	return nil
}

func (p *firewallPage) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if p.adding {
		switch key {
		case "esc":
			p.adding = false
			p.input.SetValue("")
			return nil
		case "enter":
			rule := strings.TrimSpace(p.input.Value())
			p.adding = false
			p.input.SetValue("")
			if rule == "" {
				return nil
			}
			return m.Do("allow "+rule, "", true, system.FirewallRuleCmd("allow", rule))
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return cmd
	}

	if !p.state.Installed {
		if key == "i" {
			return m.Do("install ufw", "", true, system.InstallCmd("ufw"))
		}
		return nil
	}

	switch key {
	case "e":
		if p.state.Active {
			return m.Do("turn the firewall off",
				"Nothing will be filtered, at this boot or the next. Continue?",
				true, system.FirewallDisableCmds()...)
		}
		// Letting the machine be reachable by default is the surprise worth
		// avoiding: ufw's own default denies incoming, and enabling the
		// service keeps the rules after a reboot.
		return m.Do("turn the firewall on", "", true, system.FirewallEnableCmds()...)
	case "a":
		p.adding = true
		p.input.Focus()
		return textinput.Blink
	case "d":
		if !p.state.Active {
			return nil
		}
		policy := "deny"
		if strings.Contains(p.state.Policy, "deny (incoming)") {
			policy = "allow"
		}
		return m.Do(policy+" incoming by default",
			"Traffic that matches no rule will be "+policy+"ed. Continue?",
			true, system.FirewallDefaultCmd(policy, "incoming"))
	case "x":
		row, ok := p.table.Current()
		if !ok || row.ID == "" {
			return nil
		}
		return m.Do("delete rule "+row.ID,
			"Delete rule "+row.ID+" ("+strings.Join(row.Cols, " ")+")?",
			true, system.FirewallDeleteCmd(row.ID))
	}
	p.table.Key(key, 8)
	return nil
}

func (p *firewallPage) View(m *Model, width, height int) string {
	s := m.Styles
	if p.loading {
		return m.spinner() + s.Dim.Render(" reading…")
	}
	if !p.state.Installed {
		return s.Muted.Render("No firewall is installed. ufw is the one this page drives.") + "\n\n" +
			s.Dim.Render("press i to install it, then reload with r")
	}

	status := s.Fail.Render(m.Glyphs.Fail + " off")
	switch {
	case p.state.Unknown:
		status = s.Dim.Render("unknown")
	case p.state.Active:
		status = s.OK.Render(m.Glyphs.Done + " on")
	}
	head := s.Text.Render(p.state.Backend+"  ") + status
	if p.state.Policy != "" {
		head += "\n" + s.Dim.Render(p.state.Policy)
	}
	if !p.state.Enabled && p.state.Active {
		head += "\n" + s.Warn.Render(m.Glyphs.Warn+" filtering now, but the service is not enabled: it will not come back after a reboot")
	}
	body := head + "\n\n" + p.table.View(s, m.Glyphs, m.listWidth(width), height-lineCount(head)-2)
	if p.adding {
		p.input.SetWidth(min(width-4, 60))
		body = head + "\n\n" + s.Label.Render("allow") + p.input.View() + "\n\n" +
			s.Dim.Render("a port, a service name, or a whole ufw rule; enter to add, esc to drop it")
	}

	detail := "ufw is a front end to the kernel's own filtering.\n\n" +
		"Turning it on also enables its runit service, so the rules are there " +
		"after a reboot — a firewall that forgets is worse than none.\n\n" +
		"Rules are what ufw accepts: 22/tcp, 80, ssh, or a full sentence like " +
		"\"from 192.168.1.0/24 to any port 445\"."
	if len(p.state.Others) > 0 {
		detail += "\n\nAlso installed: " + strings.Join(p.state.Others, ", ") +
			". Only one of them should be running; this page does not touch the others."
	}
	return m.split(width, height, body, detail)
}

func (p *firewallPage) Help(m *Model) []Binding {
	if !p.state.Installed {
		return []Binding{{"i", "install ufw"}}
	}
	if p.adding {
		return []Binding{{"enter", "add"}, {"esc", "cancel"}}
	}
	toggle := "turn on"
	if p.state.Active {
		toggle = "turn off"
	}
	return []Binding{{"e", toggle}, {"a", "allow"}, {"x", "delete rule"}, {"d", "default policy"}}
}
