package control

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"voidbleed/internal/system"
	"voidbleed/internal/theme"
)

type overviewPage struct {
	info    system.Overview
	cursor  int // which thing in the attention list is picked
	loading bool
	loaded  bool
}

type overviewFactsMsg struct{ info system.Overview }
type overviewCountsMsg struct{ counts system.Overview }

// logoCols is how wide the picture is drawn; the rows follow from the
// terminal's cell size, so that it comes out square.
const logoCols = 26

func newOverviewPage() *overviewPage { return &overviewPage{} }

func (p *overviewPage) Label() string { return "Overview" }
func (p *overviewPage) Title() (string, string) {
	return "Voidbleed", "bleed into the void"
}

func (p *overviewPage) Load(m *Model) tea.Cmd {
	p.loading = true
	client := m.Client
	return tea.Batch(
		func() tea.Msg { return overviewFactsMsg{info: system.MachineFacts()} },
		func() tea.Msg { return overviewCountsMsg{counts: client.Counts(context.Background())} },
	)
}

func (p *overviewPage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case overviewFactsMsg:
		facts := msg.info
		facts.Packages, facts.Manual = p.info.Packages, p.info.Manual
		facts.Orphans, facts.Updates = p.info.Orphans, p.info.Updates
		facts.Flatpaks, facts.FlatpakUpdates = p.info.Flatpaks, p.info.FlatpakUpdates
		facts.Services, facts.CacheSize = p.info.Services, p.info.CacheSize
		facts.CacheBytes, facts.Stale, facts.Counted = p.info.CacheBytes, p.info.Stale, p.info.Counted
		p.info, p.loaded = facts, true
		m.Status(facts.Hostname + " · " + facts.Kernel + " · up " + facts.Uptime)
	case tea.KeyPressMsg:
		return p.key(m, msg.String())
	case overviewCountsMsg:
		counts := msg.counts
		counts.Hostname, counts.Kernel, counts.Uptime = p.info.Hostname, p.info.Kernel, p.info.Uptime
		counts.CPU, counts.Memory, counts.Disk, counts.Root = p.info.CPU, p.info.Memory, p.info.Disk, p.info.Root
		p.info, p.loading, p.loaded = counts, false, true
	}
	return nil
}

// attend is one thing worth doing something about, and the thing that does
// it: usually opening the section that deals with it, sometimes the action
// itself.
type attend struct {
	text    string
	section string
	view    string
	do      func(m *Model) tea.Cmd
}

// attention is what the machine is asking for, in the order it is worth
// dealing with.
func (p *overviewPage) attention(m *Model) []attend {
	o := p.info
	var items []attend
	if o.Updates > 0 {
		items = append(items, attend{
			text:    plural(o.Updates, "package update", "package updates") + " waiting",
			section: "Packages", view: "updates",
		})
	}
	if o.FlatpakUpdates > 0 {
		items = append(items, attend{
			text:    plural(o.FlatpakUpdates, "Flatpak update", "Flatpak updates") + " waiting",
			section: "Flatpak", view: "updates",
		})
	}
	if len(o.Stale) > 0 {
		items = append(items, attend{
			text:    plural(len(o.Stale), "old kernel", "old kernels") + " still in /boot",
			section: "Kernels", view: "in /boot",
		})
	}
	if o.Orphans > 0 {
		items = append(items, attend{
			text:    plural(o.Orphans, "orphan", "orphans") + " nothing needs",
			section: "Packages", view: "orphans",
		})
	}
	// A gigabyte of downloaded packages nobody will install again is worth a
	// mention; a few megabytes is not.
	if o.CacheBytes > 1<<30 {
		size, orphans := o.CacheSize, o.Orphans
		items = append(items, attend{
			text: size + " of downloaded packages",
			do: func(m *Model) tea.Cmd {
				question := "Empty the download cache (" + size + ")?"
				if orphans > 0 {
					question = "Remove " + plural(orphans, "orphaned package", "orphaned packages") +
						" and empty the download cache (" + size + ")?"
				}
				return m.Do("clean up", question+"\nCached packages are only a saved download; "+
					"xbps fetches them again if it needs them.", true, system.CleanUpCmds()...)
			},
		})
	}
	return items
}

func (p *overviewPage) View(m *Model, width, height int) string {
	s := m.Styles
	if p.loading && !p.loaded {
		return m.spinner() + s.Dim.Render(" reading the machine…")
	}
	body := p.body(m, width, height)

	// The mark sits above its name, the way it does on the wallpaper and the
	// boot splash. On a short terminal the logo gives way to the wordmark,
	// and on a very short one the facts have it all.
	logo, wordmark := theme.Logo(m.Glyphs), theme.Wordmark(m.Glyphs)
	logoW, logoH := lipgloss.Width(logo), lipgloss.Height(logo)
	if m.Graphics {
		logoW, logoH = logoCols, m.logoRows()
	}
	bodyH := lipgloss.Height(body)

	head := ""
	switch {
	case width >= logoW && height >= logoH+bodyH+3:
		mark := s.Brand.Render(logo)
		if m.Graphics {
			mark = logoBox(logoCols, m.logoRows())
		}
		head = center(width, mark) + "\n" + center(width, s.Brand.Render(wordmark)) + "\n\n"
	case width >= lipgloss.Width(wordmark) && height >= bodyH+2:
		head = center(width, s.Brand.Render(wordmark)) + "\n\n"
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(head + body)
}

func (p *overviewPage) key(m *Model, key string) tea.Cmd {
	items := p.attention(m)
	if len(items) == 0 {
		return nil
	}
	switch key {
	case "up", "k":
		p.cursor = (p.cursor + len(items) - 1) % len(items)
	case "down", "j":
		p.cursor = (p.cursor + 1) % len(items)
	case "enter":
		item := items[min(p.cursor, len(items)-1)]
		if item.do != nil {
			return item.do(m)
		}
		return m.goToView(item.section, item.view)
	}
	return nil
}

// body is the machine on the left and its software on the right, or one under
// the other when there is not width for two columns. It trims the attention
// list to what the height allows.
func (p *overviewPage) body(m *Model, width, height int) string {
	for limit := 5; ; limit-- {
		out := p.bodyWith(m, width, limit)
		if limit == 1 || lipgloss.Height(out) <= height-2 {
			return out
		}
	}
}

func (p *overviewPage) bodyWith(m *Model, width, limit int) string {
	s := m.Styles
	o := p.info
	label := s.Muted.Width(9)

	var machine strings.Builder
	for _, f := range []struct{ label, value string }{
		{"host", o.Hostname},
		{"kernel", o.Kernel},
		{"uptime", o.Uptime},
		{"cpu", o.CPU},
		{"memory", o.Memory},
		{"disk", o.Disk},
		{"root", o.Root},
	} {
		if strings.TrimSpace(f.value) == "" {
			continue
		}
		machine.WriteString(label.Render(f.label) + s.Text.Render(f.value) + "\n")
	}

	var software strings.Builder
	if !o.Counted {
		software.WriteString(label.Render("packages") + m.spinner() + s.Dim.Render(" counting…") + "\n")
	} else {
		software.WriteString(label.Render("packages") + s.Text.Render(itoa(o.Packages)) +
			s.Dim.Render(", "+itoa(o.Manual)+" asked for") + "\n")
		if system.Have("flatpak") {
			software.WriteString(label.Render("flatpak") + s.Text.Render(itoa(o.Flatpaks)+" apps") + "\n")
		}
		software.WriteString(label.Render("services") + s.Text.Render(itoa(o.Services)+" enabled") + "\n")
	}
	if items := p.attention(m); len(items) > 0 {
		software.WriteString("\n" + s.Section.Render("needs attention") + "\n")
		picked := min(p.cursor, len(items)-1)
		for i, item := range items {
			if i == limit && len(items) > limit {
				software.WriteString(s.Dim.Render("  and "+itoa(len(items)-i)+" more") + "\n")
				break
			}
			line := s.Text.Render(item.text)
			if i == picked {
				// The picked one says what enter will do with it.
				what := "enter to go there"
				if item.do != nil {
					what = "enter to do it"
				}
				line = s.Accent.Render(item.text) + s.Dim.Render("  "+what)
			}
			software.WriteString(s.Warn.Render(m.Glyphs.Bullet+" ") + line + "\n")
		}
	} else if o.Counted {
		software.WriteString("\n" + s.OK.Render(m.Glyphs.Done+" nothing needs attention") + "\n")
	}

	if width < 72 {
		return machine.String() + "\n" + software.String()
	}
	half := width / 2
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(half).Render(machine.String()),
		lipgloss.NewStyle().Width(width-half).Render(software.String()))
}

func center(width int, text string) string {
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, text)
}

func (p *overviewPage) Help(m *Model) []Binding {
	if len(p.attention(m)) == 0 {
		return nil
	}
	return []Binding{{m.Glyphs.UpDown, "pick"}, {"enter", "deal with it"}}
}

// sectionNumber is what to press to reach a section, since the snapshots page
// is only there on a machine that can take snapshots.
func (m *Model) sectionNumber(label string) int {
	for i, page := range m.pages {
		if page.Label() == label {
			return i + 1
		}
	}
	return 0
}
