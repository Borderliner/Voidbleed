package control

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"voidbleed/internal/theme"
)

// Row is one line of a table. ID is what an action works on; Badge is a short
// state word drawn on the right.
type Row struct {
	ID    string
	Cols  []string
	Badge string
	// Mark is drawn before the row: a tick for selected packages, a dot for
	// running services.
	Mark  bool
	Muted bool
}

// Table is the list every page is built from: a header, rows, a cursor, and a
// filter that types straight into the list.
type Table struct {
	Headers []string
	Widths  []int // 0 means "take what is left"
	Rows    []Row

	cursor  int
	offset  int
	filter  string
	typing  bool
	visible []int // indexes into Rows after filtering
}

func (t *Table) SetRows(rows []Row) {
	t.Rows = rows
	if t.cursor >= len(rows) {
		t.cursor = max(0, len(rows)-1)
	}
	t.refilter()
}

func (t *Table) Filtering() string {
	if !t.typing && t.filter == "" {
		return ""
	}
	return t.filter
}

func (t *Table) Typing() bool { return t.typing }

// Current returns the row under the cursor.
func (t *Table) Current() (Row, bool) {
	if len(t.visible) == 0 {
		return Row{}, false
	}
	return t.Rows[t.visible[t.cursor]], true
}

func (t *Table) refilter() {
	t.visible = t.visible[:0]
	needle := strings.ToLower(t.filter)
	for i, r := range t.Rows {
		if needle == "" || strings.Contains(strings.ToLower(strings.Join(r.Cols, " ")), needle) {
			t.visible = append(t.visible, i)
		}
	}
	if t.cursor >= len(t.visible) {
		t.cursor = max(0, len(t.visible)-1)
	}
}

// Key handles navigation and filtering, and reports whether it used the key.
func (t *Table) Key(key string, page int) bool {
	if t.typing {
		switch key {
		case "esc":
			t.typing, t.filter = false, ""
			t.refilter()
			return true
		case "enter":
			t.typing = false
			return true
		case "backspace":
			if t.filter != "" {
				t.filter = t.filter[:len(t.filter)-1]
				t.refilter()
			}
			return true
		}
		if len([]rune(key)) == 1 {
			t.filter += key
			t.refilter()
			return true
		}
	}
	switch key {
	case "/":
		t.typing = true
		return true
	case "up", "k":
		t.move(-1)
	case "down", "j":
		t.move(1)
	case "pgup":
		t.move(-page)
	case "pgdown":
		t.move(page)
	case "home", "g":
		t.cursor = 0
	case "end", "G":
		t.cursor = max(0, len(t.visible)-1)
	case "esc":
		if t.filter != "" {
			t.filter = ""
			t.refilter()
			return true
		}
		return false
	default:
		return false
	}
	return true
}

func (t *Table) move(by int) {
	t.cursor = min(max(t.cursor+by, 0), max(0, len(t.visible)-1))
}

// View renders the table into the given box.
func (t *Table) View(s theme.Styles, g theme.GlyphSet, width, height int) string {
	if height < 3 {
		height = 3
	}
	body := height
	if len(t.Headers) > 0 {
		body--
	}
	if t.typing || t.filter != "" {
		body--
	}

	// Keep the cursor in view.
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+body {
		t.offset = t.cursor - body + 1
	}
	if t.offset > max(0, len(t.visible)-body) {
		t.offset = max(0, len(t.visible)-body)
	}

	widths := t.columnWidths(width)
	var b strings.Builder
	if len(t.Headers) > 0 {
		cells := make([]string, len(t.Headers))
		for i, h := range t.Headers {
			cells[i] = pad(h, widths[i])
		}
		b.WriteString(s.TableHead.Render(strings.Join(cells, " ")) + "\n")
	}

	if len(t.visible) == 0 {
		b.WriteString(s.Dim.Render("nothing here"))
	}
	for i := t.offset; i < len(t.visible) && i < t.offset+body; i++ {
		row := t.Rows[t.visible[i]]
		cells := make([]string, len(widths))
		for c := range widths {
			text := ""
			if c < len(row.Cols) {
				text = row.Cols[c]
			}
			if c == 0 {
				mark := "  "
				if row.Mark {
					mark = g.Done + " "
				}
				text = mark + text
			}
			cells[c] = pad(text, widths[c])
		}
		line := strings.Join(cells, " ")
		if row.Badge != "" {
			line = fitBadge(line, row.Badge, width)
		}
		switch {
		case i == t.cursor:
			b.WriteString(s.Selected.Width(width).Render(clipLine(line, width)))
		case row.Muted:
			b.WriteString(s.Dim.Render(clipLine(line, width)))
		default:
			b.WriteString(s.Text.Render(clipLine(line, width)))
		}
		if i < len(t.visible)-1 && i < t.offset+body-1 {
			b.WriteByte('\n')
		}
	}

	out := b.String()
	if t.typing || t.filter != "" {
		prompt := s.Muted.Render("filter: ") + s.Text.Render(t.filter)
		if t.typing {
			prompt += s.Accent.Render(g.Cursor)
		}
		count := s.Dim.Render(plural(len(t.visible), "match", "matches"))
		out += "\n" + prompt + "  " + count
	}
	return out
}

// columnWidths gives every fixed column its width and splits the rest.
func (t *Table) columnWidths(width int) []int {
	n := len(t.Widths)
	if n == 0 {
		return []int{width}
	}
	widths := make([]int, n)
	used, flex := 0, 0
	for i, w := range t.Widths {
		widths[i] = w
		if w == 0 {
			flex++
		} else {
			used += w
		}
	}
	gaps := n - 1
	if flex > 0 {
		each := (width - used - gaps) / flex
		for i := range widths {
			if widths[i] == 0 {
				widths[i] = max(each, 8)
			}
		}
	}
	return widths
}

func pad(s string, width int) string {
	s = clipLine(s, width)
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func clipLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// fitBadge right-aligns a badge on the row when there is room for it.
func fitBadge(line, badge string, width int) string {
	space := width - ansi.StringWidth(line) - ansi.StringWidth(badge) - 1
	if space < 1 {
		return line
	}
	return line + strings.Repeat(" ", space) + badge
}

func plural(n int, one, many string) string {
	word := many
	if n == 1 {
		word = one
	}
	return lipgloss.NewStyle().Render(itoa(n) + " " + word)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
