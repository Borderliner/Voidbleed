// Package tui is the Voidbleed installer's terminal interface.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"voidbleed/internal/catalog"
	"voidbleed/internal/config"
	"voidbleed/internal/hw"
)

type Options struct {
	Catalog *catalog.Catalog
	// Demo uses fake hardware and a dry-run engine: nothing on the machine changes.
	Demo    bool
	LogPath string
}

// move tells the app what to do after a screen handled a message.
type move int

const (
	stay move = iota
	next
	back
)

type binding struct{ key, desc string }

type screen interface {
	label() string             // sidebar entry
	heading() (string, string) // title, subtitle
	enter(m *Model) tea.Cmd    // called each time the screen is shown
	update(m *Model, msg tea.Msg) (tea.Cmd, move)
	view(m *Model, width, height int) string
	help(m *Model) []binding
}

// state is what the screens collect and share.
type state struct {
	opts   Options
	styles styles
	cat    *catalog.Catalog
	facts  hw.Facts
	cfg    config.Config

	selected     map[string]bool // optional groups
	detected     map[string]bool // groups whose detect rules match
	livePackages map[string]bool // packages on the live image (offline availability)
	online       *bool           // nil while checking
}

type Model struct {
	st      *state
	glyphs  glyphSet
	screens []screen
	cur     int
	width   int
	height  int
	frame   int // spinner frame
	quitAsk bool
	locked  bool // no navigation or quitting while installing
	result  error
}

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func newModel(st *state) *Model {
	m := &Model{st: st, glyphs: detectGlyphs(), width: 100, height: 32}
	m.screens = []screen{
		&welcomeScreen{},
		newKeyboardScreen(st),
		newLocaleScreen(st),
		newTimezoneScreen(st),
		&diskScreen{},
		newStorageScreen(),
		newAccountScreen(),
		&softwareScreen{},
		&sourceScreen{},
		newReviewScreen(),
		&installScreen{},
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tick(), checkNetwork(m.st), m.screens[0].enter(m))
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.frame++
		return m, tick()
	case networkMsg:
		online := bool(msg)
		m.st.online = &online
	case tea.KeyPressMsg:
		if m.quitAsk {
			switch msg.String() {
			case "y", "enter":
				return m, tea.Quit
			default:
				m.quitAsk = false
			}
			return m, nil
		}
		if msg.String() == "ctrl+c" && !m.locked {
			m.quitAsk = true
			return m, nil
		}
	}

	cmd, mv := m.screens[m.cur].update(m, msg)
	switch {
	case mv == next && m.cur < len(m.screens)-1:
		m.cur++
		return m, tea.Batch(cmd, m.screens[m.cur].enter(m))
	case mv == back && m.cur > 0 && !m.locked:
		m.cur--
		return m, tea.Batch(cmd, m.screens[m.cur].enter(m))
	}
	return m, cmd
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.BackgroundColor = colSurface
	v.ForegroundColor = colText
	v.WindowTitle = "Voidbleed installer"
	if is, ok := m.screens[m.cur].(*installScreen); ok {
		v.ProgressBar = is.progressBar()
	}
	return v
}

// Card geometry: generous on big terminals, still usable at 80×24.
func (m *Model) cardSize() (int, int) {
	w := min(m.width-2, 112)
	h := min(m.height-1, 38)
	return max(w, 40), max(h, 16)
}

func (m *Model) compact() bool { return m.width < 90 || m.height < 26 }

func (m *Model) render() string {
	s := m.st.styles
	cardW, cardH := m.cardSize()
	innerW := cardW - 6 // border + padding
	innerH := cardH - 2

	title, subtitle := m.screens[m.cur].heading()
	header := m.header(innerW)
	helpLine := m.helpLine(innerW)

	bodyH := innerH - lipgloss.Height(header) - lipgloss.Height(helpLine) - 1 // - rule
	sidebarW := 0
	if !m.compact() {
		sidebarW = 24
	}
	contentW := innerW - sidebarW

	heading := s.title.Render(title)
	if subtitle != "" {
		heading += "\n" + s.subtitle.Render(lipgloss.Wrap(subtitle, contentW-2, " "))
	}
	contentH := bodyH - lipgloss.Height(heading) - 1
	content := m.screens[m.cur].view(m, contentW-2, contentH)
	main := lipgloss.NewStyle().Width(contentW).Height(bodyH).PaddingLeft(2).
		Render(heading + "\n\n" + clip(content, contentH))

	body := main
	if sidebarW > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(sidebarW, bodyH), main)
	}

	if m.quitAsk {
		body = lipgloss.Place(innerW, bodyH, lipgloss.Center, lipgloss.Center, m.quitDialog())
	}

	rule := s.dim.Render(strings.Repeat(m.glyphs.Rule, innerW))
	card := s.card.Width(cardW).Height(cardH).Render(header + "\n" + rule + "\n" + body + "\n" + helpLine)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

func (m *Model) header(width int) string {
	s := m.st.styles
	left := s.accent.Render("VOIDBLEED") + s.dim.Render("  installer")
	if m.st.opts.Demo {
		left += "  " + s.badgeWarn.Render("DEMO")
	}
	right := s.dim.Render(fmt.Sprintf("%d / %d", m.cur+1, len(m.screens)))
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) sidebar(width, height int) string {
	s := m.st.styles
	var b strings.Builder
	for i, sc := range m.screens {
		var mark, label string
		switch {
		case i == m.cur:
			mark, label = s.accent.Render(m.glyphs.Current), s.text.Bold(true).Render(sc.label())
		case i < m.cur:
			mark, label = s.secondary.Render(m.glyphs.Done), s.muted.Render(sc.label())
		default:
			mark, label = s.dim.Render(m.glyphs.Todo), s.dim.Render(sc.label())
		}
		fmt.Fprintf(&b, " %s  %s\n", mark, label)
	}
	b.WriteString("\n")
	b.WriteString(s.dim.Italic(true).Render(" Bleed into the void."))
	return lipgloss.NewStyle().Width(width).Height(height).
		Border(lipgloss.NormalBorder(), false, true, false, false).BorderForeground(colOutline).
		Render(b.String())
}

func (m *Model) helpLine(width int) string {
	s := m.st.styles
	var parts []string
	for _, b := range m.screens[m.cur].help(m) {
		parts = append(parts, s.key.Render(b.key)+" "+s.keyDesc.Render(b.desc))
	}
	if !m.locked {
		parts = append(parts, s.key.Render("ctrl+c")+" "+s.keyDesc.Render("quit"))
	}
	line := strings.Join(parts, s.dim.Render("  "+m.glyphs.Sep+"  "))
	// Truncate rather than wrap: a second help line would push the body up.
	return ansi.Truncate(line, width, m.glyphs.Ellipsis)
}

func (m *Model) quitDialog() string {
	s := m.st.styles
	body := s.title.Render("Quit the installer?") + "\n\n" +
		s.muted.Render("Nothing has been changed on your disks.") + "\n\n" +
		s.key.Render("y") + s.keyDesc.Render(" quit   ") + s.key.Render("any other key") + s.keyDesc.Render(" stay")
	return s.danger.Render(body)
}

// clip limits s to height lines so a screen can't push the help line away.
func clip(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height && height > 0 {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) spinner() string {
	return m.st.styles.accent.Render(m.glyphs.Spinner[m.frame%len(m.glyphs.Spinner)])
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
