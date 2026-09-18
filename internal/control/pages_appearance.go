package control

import (
	"context"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

type appearancePage struct {
	current system.Appearance
	edited  system.Appearance
	themes  system.Themes
	field   int
	applied bool
}

type appearanceLoadedMsg struct {
	current system.Appearance
	themes  system.Themes
}
type appearanceAppliedMsg struct{ err error }

var appearanceFields = []string{"GTK theme", "Icons", "Cursor", "Cursor size", "Font", "Colours", "Qt style"}

var (
	cursorSizes  = []string{"16", "24", "32", "48", "64"}
	colorSchemes = []string{"prefer-dark", "default", "prefer-light"}
	fontSizes    = []string{"9", "10", "11", "12", "13", "14"}
)

func newAppearancePage() *appearancePage { return &appearancePage{} }

func (p *appearancePage) Label() string { return "Appearance" }
func (p *appearancePage) Title() (string, string) {
	return "Appearance", "one look for GTK, Qt and the cursor"
}

func (p *appearancePage) Load(m *Model) tea.Cmd {
	client := m.Client
	return func() tea.Msg {
		return appearanceLoadedMsg{
			current: client.Appearance(context.Background()),
			themes:  system.InstalledThemes(),
		}
	}
}

func (p *appearancePage) Update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case appearanceLoadedMsg:
		p.current, p.edited, p.themes = msg.current, msg.current, msg.themes
		p.applied = false
		m.Status("read from gsettings and the toolkit config files")
		return nil
	case appearanceAppliedMsg:
		if msg.err != nil {
			m.Fail(msg.err)
			return nil
		}
		p.current, p.applied = p.edited, true
		m.Status("applied; GTK and Qt applications pick it up as they start")
		return nil
	case tea.KeyPressMsg:
		return p.key(m, msg.String())
	}
	return nil
}

func (p *appearancePage) key(m *Model, key string) tea.Cmd {
	switch key {
	case "up", "k":
		p.field = (p.field + len(appearanceFields) - 1) % len(appearanceFields)
	case "down", "j", "tab":
		p.field = (p.field + 1) % len(appearanceFields)
	case "left", "h":
		p.cycleField(-1)
	case "right", "l":
		p.cycleField(1)
	case "enter", "a":
		if p.edited == p.current {
			m.Status("nothing to apply")
			return nil
		}
		settings, client := p.edited, m.Client
		m.Status("writing…")
		return func() tea.Msg {
			return appearanceAppliedMsg{err: client.ApplyAppearance(context.Background(), settings)}
		}
	case "z":
		p.edited = p.current
	}
	return nil
}

func (p *appearancePage) cycleField(step int) {
	a := &p.edited
	switch p.field {
	case 0:
		a.GTKTheme = cycle(p.themes.GTK, a.GTKTheme, step)
	case 1:
		a.IconTheme = cycle(p.themes.Icons, a.IconTheme, step)
	case 2:
		a.CursorTheme = cycle(p.themes.Cursors, a.CursorTheme, step)
	case 3:
		a.CursorSize, _ = strconv.Atoi(cycle(cursorSizes, strconv.Itoa(a.CursorSize), step))
	case 4:
		a.FontName = cycleFontSize(a.FontName, step)
	case 5:
		a.ColorScheme = cycle(colorSchemes, a.ColorScheme, step)
	case 6:
		a.QtStyle = cycle(p.themes.QtStyle, a.QtStyle, step)
	}
}

// cycleFontSize changes only the size in "Ubuntu 10": the family is someone's
// own choice, and guessing a list of families to cycle through is worse than
// leaving it alone.
func cycleFontSize(font string, step int) string {
	family, size, ok := strings.Cut(strings.TrimSpace(font), " ")
	if !ok {
		i := strings.LastIndex(font, " ")
		if i < 0 {
			return font
		}
		family, size = font[:i], font[i+1:]
	}
	if i := strings.LastIndex(font, " "); i > 0 {
		family, size = font[:i], font[i+1:]
	}
	if _, err := strconv.Atoi(size); err != nil {
		return font
	}
	return family + " " + cycle(fontSizes, size, step)
}

func (p *appearancePage) values() []string {
	a := p.edited
	return []string{
		a.GTKTheme, a.IconTheme, a.CursorTheme, strconv.Itoa(a.CursorSize),
		a.FontName, a.ColorScheme, a.QtStyle,
	}
}

func (p *appearancePage) View(m *Model, width, height int) string {
	s := m.Styles
	values := p.values()
	var b strings.Builder
	for i, label := range appearanceFields {
		value := values[i]
		if value == "" {
			value = s.Dim.Render("not set")
		}
		b.WriteString(m.field(label, value, i == p.field, "") + "\n")
	}
	if p.edited != p.current {
		b.WriteString("\n" + s.Warn.Render(m.Glyphs.Warn+" not applied yet — press enter"))
	} else if p.applied {
		b.WriteString("\n" + s.OK.Render(m.Glyphs.Done+" applied"))
	}

	detail := "Where this is written\n\n" +
		"~/.config/gtk-3.0/settings.ini\n" +
		"~/.config/gtk-4.0/settings.ini\n" +
		"~/.config/qt5ct, ~/.config/qt6ct\n" +
		"~/.local/share/icons/default\n" +
		"~/.config/niri/local.kdl\n" +
		"gsettings org.gnome.desktop.interface\n\n" +
		"Both gsettings and the .ini files are written: applications that go " +
		"through the desktop portal read the first, the rest read the second. " +
		"Existing keys in those files are kept.\n\n" +
		"Qt fonts are left alone — qt5ct and qt6ct store them in a binary " +
		"format only they should write.\n\n" +
		itoa(len(p.themes.GTK)) + " GTK themes, " + itoa(len(p.themes.Icons)) +
		" icon themes and " + itoa(len(p.themes.Cursors)) + " cursor themes installed."
	return m.split(width, height, b.String(), detail)
}

func (p *appearancePage) Help(m *Model) []Binding {
	return []Binding{
		{m.Glyphs.UpDown, "field"}, {m.Glyphs.LeftRight, "change"},
		{"enter", "apply"}, {"z", "undo"},
	}
}
