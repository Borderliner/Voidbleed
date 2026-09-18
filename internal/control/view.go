package control

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// listWidth is how much room a page's list gets: pages must build their
// tables at this width, or the detail pane squeezes them and every row wraps.
func (m *Model) listWidth(width int) int {
	if width < 76 {
		return width
	}
	return width * 55 / 100
}

// split draws a list on the left and a detail pane on the right, the way a
// file manager does: the list is where you move, the pane is what you are
// looking at. Narrow terminals drop the pane rather than squeezing both.
func (m *Model) split(width, height int, list, detail string) string {
	if width < 76 || detail == "" {
		return lipgloss.NewStyle().Width(width).Height(height).Render(list)
	}
	listW := m.listWidth(width)
	detailW := width - listW - 3
	left := lipgloss.NewStyle().Width(listW).Height(height).Render(list)
	right := lipgloss.NewStyle().Width(detailW).Height(height).
		Render(m.Styles.Muted.Render(wrap(detail, detailW)))
	divider := lipgloss.NewStyle().Height(height).Foreground(lipgloss.Color("#5e3a3f")).
		Render(strings.Repeat("│\n", max(height, 1)))
	if m.Glyphs.ASCII {
		divider = lipgloss.NewStyle().Height(height).Render(strings.Repeat("|\n", max(height, 1)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " "+divider+" ", right)
}

// field is one label-and-value row on a settings page.
func (m *Model) field(label, value string, focused bool, hint string) string {
	s := m.Styles
	style := s.Label
	if focused {
		style = s.LabelFocus
	}
	line := style.Render(label) + s.Text.Render(value)
	if focused {
		line += " " + s.Dim.Render(m.Glyphs.LeftRight)
	}
	if hint != "" {
		line += "\n" + strings.Repeat(" ", 16) + s.Dim.Render(hint)
	}
	return line
}

func wrap(text string, width int) string {
	if width < 8 {
		return text
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		out = append(out, lipgloss.Wrap(para, width, " "))
	}
	return strings.Join(out, "\n")
}

// cycle moves through a list of choices, wrapping at both ends.
func cycle(options []string, current string, step int) string {
	if len(options) == 0 {
		return current
	}
	index := 0
	for i, o := range options {
		if o == current {
			index = i
			break
		}
	}
	return options[(index+step+len(options))%len(options)]
}
