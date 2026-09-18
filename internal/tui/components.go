package tui

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// picker is a type-to-filter list, single or multi-select.
type picker struct {
	all      []option
	filter   string
	visible  []int // indexes into all
	cursor   int   // index into visible
	offset   int
	multi    bool
	chosen   []string // multi: in the order picked
	value    string   // single
	maxMulti int
}

func newPicker(opts []option, multi bool) *picker {
	p := &picker{all: opts, multi: multi}
	p.refilter()
	return p
}

func (p *picker) refilter() {
	p.visible = p.visible[:0]
	needle := strings.ToLower(p.filter)
	for i, o := range p.all {
		if needle == "" || strings.Contains(strings.ToLower(o.Label+" "+o.Value), needle) {
			p.visible = append(p.visible, i)
		}
	}
	p.cursor = min(p.cursor, max(0, len(p.visible)-1))
}

// focus moves the cursor to value, clearing the filter.
func (p *picker) focus(value string) {
	p.filter = ""
	p.refilter()
	for vi, i := range p.visible {
		if p.all[i].Value == value {
			p.cursor = vi
			return
		}
	}
}

func (p *picker) current() (option, bool) {
	if len(p.visible) == 0 {
		return option{}, false
	}
	return p.all[p.visible[p.cursor]], true
}

func (p *picker) toggle(value string) {
	if i := slices.Index(p.chosen, value); i >= 0 {
		p.chosen = slices.Delete(p.chosen, i, i+1)
		return
	}
	if p.maxMulti > 0 && len(p.chosen) >= p.maxMulti {
		return
	}
	p.chosen = append(p.chosen, value)
}

// handle processes a key; it returns true when enter was pressed.
func (p *picker) handle(k tea.KeyPressMsg) bool {
	switch k.String() {
	case "up", "ctrl+p":
		p.cursor = max(0, p.cursor-1)
	case "down", "ctrl+n":
		p.cursor = min(len(p.visible)-1, p.cursor+1)
	case "pgup":
		p.cursor = max(0, p.cursor-10)
	case "pgdown":
		p.cursor = min(len(p.visible)-1, p.cursor+10)
	case "home":
		p.cursor = 0
	case "end":
		p.cursor = max(0, len(p.visible)-1)
	case "backspace":
		if p.filter != "" {
			p.filter = p.filter[:len(p.filter)-1]
			p.refilter()
		}
	case "space":
		if o, ok := p.current(); ok && p.multi {
			p.toggle(o.Value)
		}
	case "enter":
		if o, ok := p.current(); ok && !p.multi {
			p.value = o.Value
		}
		return true
	default:
		if k.Text != "" && unicode.IsPrint([]rune(k.Text)[0]) && k.Text != " " {
			p.filter += k.Text
			p.cursor = 0
			p.refilter()
		}
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	return false
}

func (p *picker) view(m *Model, width, height int) string {
	s := m.st.styles
	var b strings.Builder
	search := s.dim.Render("type to search")
	if p.filter != "" {
		search = s.accent.Render(p.filter) + s.dim.Render(fmt.Sprintf("   %d match%s", len(p.visible), plural(len(p.visible), "es")))
	}
	b.WriteString(s.muted.Render("Search ") + search + "\n\n")
	rows := max(1, height-2)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	if len(p.visible) == 0 {
		b.WriteString(s.dim.Render("  nothing matches"))
	}
	for vi := p.offset; vi < min(len(p.visible), p.offset+rows); vi++ {
		o := p.all[p.visible[vi]]
		mark := "  "
		if p.multi {
			mark = s.dim.Render(m.glyphs.Uncheck) + " "
			if n := slices.Index(p.chosen, o.Value); n >= 0 {
				mark = s.accent.Render(m.glyphs.Check) + " "
			}
		} else if o.Value == p.value {
			mark = s.accent.Render(m.glyphs.Radio) + " "
		}
		label := o.Label
		detail := ""
		if o.Detail != "" && o.Detail != o.Label {
			detail = "  " + o.Detail
		}
		line := ansi.Truncate(label+detail, width-6, m.glyphs.Ellipsis)
		if vi == p.cursor {
			b.WriteString(s.accent.Render(m.glyphs.Cursor) + " " + mark + s.selected.Render(padRight(line, width-6)))
		} else {
			b.WriteString("  " + mark + s.text.Render(label) + s.dim.Render(strings.TrimPrefix(line, label)))
		}
		b.WriteString("\n")
	}
	if len(p.visible) > rows {
		b.WriteString(s.dim.Render(fmt.Sprintf("  %d–%d of %d", p.offset+1, min(len(p.visible), p.offset+rows), len(p.visible))))
	}
	return b.String()
}

func padRight(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func plural(n int, suffix string) string {
	if n == 1 {
		return ""
	}
	return suffix
}

// choiceRow renders "Label   ext4  [btrfs]" style options.
func (m *Model) choiceRow(label string, choices []string, idx int, focused bool) string {
	s := m.st.styles
	l := s.label.Render(label)
	if focused {
		l = s.labelFocus.Render(label)
	}
	var parts []string
	for i, c := range choices {
		switch {
		case i == idx && focused:
			parts = append(parts, s.selected.Render(" "+c+" "))
		case i == idx:
			parts = append(parts, s.accent.Render(" "+c+" "))
		default:
			parts = append(parts, s.dim.Render(" "+c+" "))
		}
	}
	arrow := "  "
	if focused {
		arrow = s.accent.Render(m.glyphs.Cursor) + " "
	}
	return arrow + l + strings.Join(parts, " ")
}

// inputRow renders a labelled text input.
func (m *Model) inputRow(label string, in *textinput.Model, focused bool, note string) string {
	s := m.st.styles
	l := s.label.Render(label)
	arrow := "  "
	if focused {
		l = s.labelFocus.Render(label)
		arrow = s.accent.Render(m.glyphs.Cursor) + " "
	}
	row := arrow + l + in.View()
	if note != "" {
		row += "  " + note
	}
	return row
}

// hint renders dim help text aligned under form values, wrapped to width.
func (m *Model) hint(text string, width int) string {
	indent := strings.Repeat(" ", labelWidth+2)
	wrapped := lipgloss.Wrap(text, max(20, width-labelWidth-2), " ")
	lines := strings.Split(wrapped, "\n")
	for i, l := range lines {
		lines[i] = indent + m.st.styles.dim.Render(l)
	}
	return strings.Join(lines, "\n")
}

func newInput(placeholder string, secret bool, limit int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.Prompt = ""
	in.CharLimit = limit
	in.SetWidth(26)
	if secret {
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
		if detectGlyphs().ASCII {
			in.EchoCharacter = '*'
		}
	}
	st := textinput.DefaultStyles(true)
	st.Focused.Text = lipgloss.NewStyle().Foreground(colText)
	st.Blurred.Text = lipgloss.NewStyle().Foreground(colMuted)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(colDim)
	st.Blurred.Placeholder = lipgloss.NewStyle().Foreground(colDim)
	in.SetStyles(st)
	return in
}

// strength scores a password 0–4.
func strength(pw string) (int, string) {
	if pw == "" {
		return 0, ""
	}
	classes := 0
	for _, f := range []func(rune) bool{unicode.IsLower, unicode.IsUpper, unicode.IsDigit, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }} {
		if strings.IndexFunc(pw, f) >= 0 {
			classes++
		}
	}
	n := len([]rune(pw))
	switch {
	case n < 8:
		return 1, "weak"
	case n < 12 && classes < 3:
		return 2, "fair"
	case n < 16 && classes < 3:
		return 3, "good"
	default:
		return 4, "strong"
	}
}

func (m *Model) meter(score int, label string) string {
	if label == "" {
		return ""
	}
	s := m.st.styles
	colors := []lipgloss.Style{s.fail, s.warn, s.secondary, s.ok}
	style := colors[max(0, score-1)]
	filled := strings.Repeat(m.glyphs.Bar, score*2)
	empty := strings.Repeat(m.glyphs.BarEmpty, (4-score)*2)
	return style.Render(filled) + s.dim.Render(empty) + " " + style.Render(label)
}

type networkMsg bool

// checkNetwork asks the Void mirror for its repodata headers.
func checkNetwork(st *state) tea.Cmd {
	return func() tea.Msg {
		if st.opts.Demo {
			time.Sleep(900 * time.Millisecond)
			return networkMsg(true)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		url := strings.TrimRight(st.cfg.Install.Mirror, "/") + "/current/x86_64-repodata"
		return networkMsg(exec.CommandContext(ctx, "curl", "-fsI", "--max-time", "7", url).Run() == nil)
	}
}
