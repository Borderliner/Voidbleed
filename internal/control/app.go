// Package control is the Voidbleed control centre: one terminal interface for
// the things people otherwise keep a wiki page of commands for.
package control

import (
	"context"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	uv "github.com/charmbracelet/ultraviolet"
	"voidbleed/internal/sys"

	"voidbleed/internal/system"
	"voidbleed/internal/theme"
)

type Options struct {
	// Demo answers every command from canned output: nothing on the machine
	// is read or changed, which is how the interface is developed.
	Demo bool
	// NoGraphics draws the logo as block art even where the terminal could
	// show the picture itself.
	NoGraphics bool
}

// Binding is one key hint in the footer.
type Binding struct{ Key, Desc string }

// Page is one section of the control centre.
type Page interface {
	// Label is the sidebar entry; Title and Subtitle head the page.
	Label() string
	Title() (string, string)
	// Load fetches what the page shows. It runs on entry and after actions.
	Load(m *Model) tea.Cmd
	Update(m *Model, msg tea.Msg) tea.Cmd
	View(m *Model, width, height int) string
	Help(m *Model) []Binding
}

type overlay int

const (
	overlayNone overlay = iota
	overlayPassword
	overlayConfirm
	overlayOutput
	overlayQuit
)

// action is something to do once the password is known.
type action struct {
	title      string
	confirm    string // shown before running; empty runs at once
	privileged bool
	cmds       []sys.Cmd
}

type Model struct {
	Client *system.Client
	Glyphs theme.GlyphSet
	Styles theme.Styles
	// Graphics is set once the terminal has answered that it can draw
	// pictures. Until then, and everywhere else, the logo is block art.
	Graphics bool
	// Where the picture was last asked to go, and whether it is there.
	imageSent     bool
	imageOnScreen bool
	imageRow      int
	imageCol      int

	pages       []Page
	cur         int
	askGraphics bool

	width, height int
	frame         int
	status        string // one line under the header
	err           error

	over    overlay
	pending *action
	pass    textinput.Model
	passErr bool

	run struct {
		title   string
		lines   []string
		events  chan runEvent
		running bool
		err     error
		cancel  context.CancelFunc
		scroll  int
	}
}

type tickMsg struct{}
type runEvent struct {
	line string
	done bool
	err  error
}

func tick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func New(opts Options) *Model {
	g := theme.Detect()
	m := &Model{
		Glyphs: g,
		Styles: theme.NewStyles(g),
		width:  100,
		height: 32,
	}
	m.askGraphics = !opts.NoGraphics && !g.ASCII && graphicsAllowed()
	if opts.Demo {
		m.Client = system.NewDemo(demoRunner{})
	} else {
		m.Client = system.New(nil)
	}
	m.pass = textinput.New()
	m.pass.Placeholder = "password"
	m.pass.EchoMode = textinput.EchoPassword
	m.pass.CharLimit = 128
	m.pages = []Page{
		newOverviewPage(),
		newPackagesPage(),
		newFlatpakPage(),
		newServicesPage(),
		newKernelsPage(),
	}
	// Snapshots are only offered on a machine that can take them: a btrfs
	// root with the tools for it. Demo mode shows the page regardless, since
	// it is pretending to be such a machine.
	if opts.Demo || system.SnapshotsAvailable() {
		m.pages = append(m.pages, newSnapshotsPage())
	}
	m.pages = append(m.pages, newFirmwarePage(), newAppearancePage(), newFirewallPage())
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick(), m.pages[m.cur].Load(m)}
	if m.askGraphics {
		// The answer, if there is one, arrives as an APC event.
		cmds = append(cmds, tea.Raw(graphicsQuery))
	}
	return tea.Batch(cmds...)
}

func (m *Model) Page() Page { return m.pages[m.cur] }

// Status sets the line under the header.
func (m *Model) Status(text string) { m.status, m.err = text, nil }

// Fail shows an error where the status line is.
func (m *Model) Fail(err error) { m.err = err }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		m.frame++
		return m, tea.Batch(tick(), m.drawImage())
	case uv.KittyGraphicsEvent:
		// The terminal answered the question asked at startup. Only its own
		// "OK", for the picture this program asked about, turns them on.
		if msg.Options.ID == logoImageID && string(msg.Payload) == "OK" {
			m.Graphics = true
		}
		return m, nil
	case runEvent:
		return m, m.onRunEvent(msg)
	case tea.KeyPressMsg:
		return m, m.onKey(msg)
	}
	return m, m.pages[m.cur].Update(m, msg)
}

func (m *Model) onKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	switch m.over {
	case overlayQuit:
		if key == "y" || key == "enter" {
			return tea.Quit
		}
		m.over = overlayNone
		return nil
	case overlayPassword:
		return m.passwordKey(msg)
	case overlayConfirm:
		switch key {
		case "y", "enter":
			act := m.pending
			m.over, m.pending = overlayNone, nil
			return m.start(act)
		case "n", "esc", "q":
			m.over, m.pending = overlayNone, nil
		}
		return nil
	case overlayOutput:
		switch key {
		case "esc", "q", "enter":
			if m.run.running {
				return nil // a package manager mid-transaction is not interruptible
			}
			m.over = overlayNone
			m.run.scroll = 0
			return m.pages[m.cur].Load(m)
		case "ctrl+c":
			if m.run.running && m.run.cancel != nil {
				m.run.cancel()
			}
			return nil
		case "up", "k":
			m.run.scroll = max(0, m.run.scroll-1)
		case "down", "j":
			m.run.scroll++
		}
		return nil
	}

	switch key {
	case "ctrl+c", "q":
		m.over = overlayQuit
		return nil
	case "tab", "shift+tab", "ctrl+n", "ctrl+p":
		// Pages own plain letters for their own actions, so section switching
		// lives on tab and the number keys.
		step := 1
		if key == "shift+tab" || key == "ctrl+p" {
			step = -1
		}
		return m.goTo((m.cur + step + len(m.pages)) % len(m.pages))
	case "r", "f5":
		m.Status("reading…")
		return m.pages[m.cur].Load(m)
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		if i := int(key[0] - '1'); i < len(m.pages) {
			return m.goTo(i)
		}
	}
	return m.pages[m.cur].Update(m, msg)
}

func (m *Model) goTo(i int) tea.Cmd {
	if i == m.cur {
		return nil
	}
	m.cur = i
	m.status, m.err = "", nil
	return m.pages[m.cur].Load(m)
}

func (m *Model) passwordKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.over, m.pending, m.passErr = overlayNone, nil, false
		m.pass.SetValue("")
		return nil
	case "enter":
		pw := m.pass.Value()
		if !m.Client.CheckPassword(context.Background(), pw) {
			m.passErr = true
			m.pass.SetValue("")
			return nil
		}
		m.Client.Password = pw
		m.pass.SetValue("")
		m.passErr = false
		act := m.pending
		m.over, m.pending = overlayNone, nil
		return m.Do(act.title, act.confirm, act.privileged, act.cmds...)
	}
	var cmd tea.Cmd
	m.pass, cmd = m.pass.Update(msg)
	return cmd
}

// Do runs commands, asking for the sudo password and for confirmation first
// when those are needed. Pages call this for everything that changes the
// machine.
func (m *Model) Do(title, confirm string, privileged bool, cmds ...sys.Cmd) tea.Cmd {
	act := &action{title: title, confirm: confirm, privileged: privileged, cmds: cmds}
	if privileged && m.Client.NeedsPassword(context.Background()) {
		m.pending = act
		m.over = overlayPassword
		m.pass.Focus()
		return textinput.Blink
	}
	if confirm != "" {
		m.pending = act
		m.over = overlayConfirm
		return nil
	}
	return m.start(act)
}

func (m *Model) start(act *action) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan runEvent, 256)
	m.run.title, m.run.lines, m.run.events = act.title, nil, events
	m.run.running, m.run.err, m.run.cancel, m.run.scroll = true, nil, cancel, 0
	m.over = overlayOutput

	client, cmds, privileged := m.Client, act.cmds, act.privileged
	go func() {
		defer close(events)
		err := client.Stream(ctx, func(line string) {
			events <- runEvent{line: line}
		}, privileged, cmds...)
		events <- runEvent{done: true, err: err}
	}()
	return waitFor(events)
}

func waitFor(events chan runEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return runEvent{done: true}
		}
		return ev
	}
}

func (m *Model) onRunEvent(ev runEvent) tea.Cmd {
	if ev.done {
		m.run.running, m.run.err = false, ev.err
		if ev.err == nil {
			m.Status(m.run.title + ": done")
		}
		return nil
	}
	m.run.lines = append(m.run.lines, ev.line)
	// Follow the tail unless the reader scrolled up.
	return waitFor(m.run.events)
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.BackgroundColor = theme.Surface
	v.ForegroundColor = theme.Text
	v.WindowTitle = "Voidbleed control"
	return v
}

func (m *Model) spinner() string {
	return m.Styles.Accent.Render(m.Glyphs.Spinner[m.frame%len(m.Glyphs.Spinner)])
}

func (m *Model) compact() bool { return m.width < 88 }

func (m *Model) cardSize() (int, int) {
	w := min(m.width-2, 120)
	h := min(m.height-1, 44)
	return max(w, 40), max(h, 16)
}

func (m *Model) render() string {
	s := m.Styles
	cardW, cardH := m.cardSize()
	innerW, innerH := cardW-6, cardH-2

	header := m.header(innerW)
	help := m.helpLine(innerW)
	bodyH := innerH - lipgloss.Height(header) - lipgloss.Height(help) - 1

	sidebarW := 0
	if !m.compact() {
		// Wide enough for "Appearance" and its number, and no wider: every
		// column here is one the page does not get.
		sidebarW = 16
	}
	contentW := innerW - sidebarW

	var body string
	switch m.over {
	case overlayOutput:
		body = m.outputView(innerW, bodyH)
	case overlayPassword:
		body = lipgloss.Place(innerW, bodyH, lipgloss.Center, lipgloss.Center, m.passwordDialog())
	case overlayConfirm:
		body = lipgloss.Place(innerW, bodyH, lipgloss.Center, lipgloss.Center, m.confirmDialog())
	case overlayQuit:
		body = lipgloss.Place(innerW, bodyH, lipgloss.Center, lipgloss.Center, m.quitDialog())
	default:
		title, subtitle := m.pages[m.cur].Title()
		heading := s.Title.Render(title)
		if subtitle != "" {
			heading += "  " + s.Subtitle.Render(subtitle)
		}
		note := m.statusLine()
		contentH := bodyH - lipgloss.Height(heading) - 2
		if note != "" {
			contentH--
		}
		page := m.pages[m.cur].View(m, contentW-2, max(contentH, 3))
		main := heading + "\n"
		if note != "" {
			main += note + "\n"
		}
		main += "\n" + page
		body = lipgloss.NewStyle().Width(contentW).Height(bodyH).PaddingLeft(2).Render(main)
		if sidebarW > 0 {
			body = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(sidebarW, bodyH), body)
		}
	}

	rule := s.Dim.Render(strings.Repeat(m.Glyphs.Rule, innerW))
	card := s.Card.Width(cardW).Height(cardH).Render(header + "\n" + rule + "\n" + body + "\n" + help)
	frame := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)

	// The overview leaves a marker in the corner of the box it reserved; that
	// is where the picture goes, and the marker itself becomes a space.
	row, col, frame, found := findMarker(frame)
	m.imageRow, m.imageCol = row, col
	if !found {
		m.imageRow, m.imageCol = 0, 0
	}
	return frame
}

// drawImage keeps the picture where the last frame said it should be. It is
// written outside the frame: Bubble Tea paints cells, and an escape sequence
// inside a cell is not something it passes through.
func (m *Model) drawImage() tea.Cmd {
	if !m.Graphics {
		return nil
	}
	if m.imageRow == 0 {
		if !m.imageOnScreen {
			return nil
		}
		m.imageOnScreen = false
		return tea.Raw(deleteLogo())
	}
	out := ""
	if !m.imageSent {
		out, m.imageSent = transmitLogo(), true
	}
	// Repainting the cells rubs the picture out, so it is placed again every
	// few frames; the escape is small enough that this costs nothing.
	if !m.imageOnScreen || m.frame%8 == 0 {
		out += placeLogoAt(m.imageRow, m.imageCol, logoCols, logoRows)
		m.imageOnScreen = true
	}
	if out == "" {
		return nil
	}
	return tea.Raw(out)
}

func (m *Model) statusLine() string {
	switch {
	case m.err != nil:
		return m.Styles.Fail.Render(m.Glyphs.Fail+" ") + m.Styles.Text.Render(clipLine(m.err.Error(), m.width-12))
	case m.status != "":
		return m.Styles.Dim.Render(m.status)
	}
	return ""
}

func (m *Model) header(width int) string {
	s := m.Styles
	left := s.Header.Render("VOIDBLEED") + s.Dim.Render("  control")
	host, _ := os.Hostname()
	right := s.Dim.Render(host)
	if m.Client.Root() {
		right = s.Warn.Render("root") + s.Dim.Render("  "+host)
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) sidebar(width, height int) string {
	s := m.Styles
	var b strings.Builder
	for i, p := range m.pages {
		label := " " + itoa(i+1) + " " + p.Label()
		style := s.SidebarItem
		if i == m.cur {
			style = s.SidebarActive
		}
		b.WriteString(style.Width(width-1).Render(clipLine(label, width-2)) + "\n")
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(b.String())
}

func (m *Model) helpLine(width int) string {
	s := m.Styles
	var bindings []Binding
	switch m.over {
	case overlayOutput:
		if m.run.running {
			bindings = []Binding{{"ctrl+c", "stop"}}
		} else {
			bindings = []Binding{{"esc", "back"}, {m.Glyphs.UpDown, "scroll"}}
		}
	case overlayPassword:
		bindings = []Binding{{"enter", "unlock"}, {"esc", "cancel"}}
	case overlayConfirm:
		bindings = []Binding{{"y", "yes"}, {"n", "no"}}
	case overlayQuit:
		bindings = []Binding{{"y", "quit"}, {"n", "stay"}}
	default:
		bindings = append(m.pages[m.cur].Help(m),
			Binding{"tab", "section"}, Binding{"r", "reload"}, Binding{"q", "quit"})
	}
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		parts = append(parts, s.Key.Render(b.Key)+" "+s.KeyDesc.Render(b.Desc))
	}
	line := strings.Join(parts, s.Dim.Render("  "+m.Glyphs.Sep+"  "))
	return clipLine(line, width)
}

func (m *Model) outputView(width, height int) string {
	s := m.Styles
	head := s.Title.Render(m.run.title)
	switch {
	case m.run.running:
		head += "  " + m.spinner()
	case m.run.err != nil:
		head += "  " + s.Fail.Render(m.Glyphs.Fail+" failed")
	default:
		head += "  " + s.OK.Render(m.Glyphs.Done+" done")
	}

	bodyH := height - 2
	lines := m.run.lines
	if m.run.err != nil {
		lines = append(lines, "", m.run.err.Error())
	}
	start := max(0, len(lines)-bodyH-m.run.scroll)
	if start+bodyH > len(lines) {
		m.run.scroll = max(0, len(lines)-bodyH-start)
	}
	var b strings.Builder
	for i := start; i < len(lines) && i < start+bodyH; i++ {
		style := s.Text
		if strings.HasPrefix(lines[i], "$ ") {
			style = s.Dim
		}
		b.WriteString(style.Render(clipLine(lines[i], width)) + "\n")
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(head + "\n\n" + b.String())
}

func (m *Model) passwordDialog() string {
	s := m.Styles
	m.pass.SetWidth(32)
	body := s.Text.Render("Administrator password for "+m.Styles.Accent.Render(m.pending.title)) + "\n\n" + m.pass.View()
	if m.passErr {
		body += "\n\n" + s.Fail.Render(m.Glyphs.Fail+" that password was not accepted")
	} else {
		body += "\n\n" + s.Dim.Render("sudo needs it once; it is kept in memory only")
	}
	return s.Card.Render(body)
}

func (m *Model) confirmDialog() string {
	s := m.Styles
	return s.Danger.Render(s.Title.Render(m.pending.title) + "\n\n" +
		s.Text.Render(m.pending.confirm) + "\n\n" +
		s.Muted.Render("y to go ahead, n to change your mind"))
}

func (m *Model) quitDialog() string {
	s := m.Styles
	return s.Card.Render(s.Text.Render("Leave the control centre?") + "\n\n" + s.Muted.Render("y / n"))
}
