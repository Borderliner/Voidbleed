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
	loading bool
	loaded  bool
}

type overviewFactsMsg struct{ info system.Overview }
type overviewCountsMsg struct{ counts system.Overview }

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
		facts.CacheBytes, facts.Counted = p.info.CacheBytes, p.info.Counted
		p.info, p.loaded = facts, true
		m.Status(facts.Hostname + " · " + facts.Kernel + " · up " + facts.Uptime)
	case overviewCountsMsg:
		counts := msg.counts
		counts.Hostname, counts.Kernel, counts.Uptime = p.info.Hostname, p.info.Kernel, p.info.Uptime
		counts.CPU, counts.Memory, counts.Disk, counts.Root = p.info.CPU, p.info.Memory, p.info.Disk, p.info.Root
		p.info, p.loading, p.loaded = counts, false, true
	}
	return nil
}

// attention is the short list of things worth doing something about, each
// with the section that does it.
func (p *overviewPage) attention(m *Model) []string {
	o := p.info
	var items []string
	if o.Updates > 0 {
		items = append(items, plural(o.Updates, "package update", "package updates")+" waiting — press 2")
	}
	if o.FlatpakUpdates > 0 {
		items = append(items, plural(o.FlatpakUpdates, "Flatpak update", "Flatpak updates")+" waiting — press 3")
	}
	if o.Orphans > 0 {
		items = append(items, plural(o.Orphans, "orphaned package", "orphaned packages")+
			" nothing needs — press 2, then c")
	}
	// A gigabyte of downloaded packages nobody will install again is worth a
	// mention; a few megabytes is not.
	if o.CacheBytes > 1<<30 {
		items = append(items, o.CacheSize+" of package cache — press 2, then c")
	}
	return items
}

func (p *overviewPage) View(m *Model, width, height int) string {
	s := m.Styles
	if p.loading && !p.loaded {
		return m.spinner() + s.Dim.Render(" reading the machine…")
	}
	o := p.info

	logo := s.Accent.Render(theme.Logo(m.Glyphs))
	facts := []struct{ label, value string }{
		{"host", o.Hostname},
		{"kernel", o.Kernel},
		{"uptime", o.Uptime},
		{"cpu", o.CPU},
		{"memory", o.Memory},
		{"disk", o.Disk + "  " + s.Dim.Render(o.Root)},
	}
	var body strings.Builder
	for _, f := range facts {
		if strings.TrimSpace(f.value) == "" {
			continue
		}
		body.WriteString(s.Label.Render(f.label) + s.Text.Render(f.value) + "\n")
	}

	body.WriteString("\n" + s.Section.Render("software") + "\n")
	if !o.Counted {
		body.WriteString(s.Label.Render("packages") + m.spinner() + s.Dim.Render(" counting…") + "\n")
	} else {
		body.WriteString(s.Label.Render("packages") + s.Text.Render(
			plural(o.Packages, "package", "packages")+s.Dim.Render(", "+itoa(o.Manual)+" asked for by hand")) + "\n")
		if system.Have("flatpak") {
			body.WriteString(s.Label.Render("flatpak") + s.Text.Render(plural(o.Flatpaks, "application", "applications")) + "\n")
		}
		body.WriteString(s.Label.Render("services") + s.Text.Render(plural(o.Services, "service", "services")+" enabled") + "\n")
	}

	if items := p.attention(m); len(items) > 0 {
		body.WriteString("\n" + s.Section.Render("needs attention") + "\n")
		for _, item := range items {
			body.WriteString(s.Warn.Render(" "+m.Glyphs.Bullet+" ") + s.Text.Render(item) + "\n")
		}
	} else if o.Counted {
		body.WriteString("\n" + s.OK.Render(m.Glyphs.Done+" nothing needs attention") + "\n")
	}

	// The logo only earns its place when there is room beside it, and the
	// wordmark only when it fits on one line.
	if width < 72 {
		return lipgloss.NewStyle().Width(width).Height(height).Render(body.String())
	}
	artW := 30
	art := logo
	if wordmark := theme.Wordmark(m.Glyphs); width >= 104 {
		artW = lipgloss.Width(wordmark) + 2
		art += "\n\n" + s.Accent.Render(wordmark)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(artW).Height(height).Render(art),
		lipgloss.NewStyle().Width(width-artW).Height(height).Render(body.String()))
}

func (p *overviewPage) Help(m *Model) []Binding { return nil }
