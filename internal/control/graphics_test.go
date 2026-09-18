package control

import (
	"encoding/base64"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"
)

// The picture is drawn by putting particular characters in the frame, so the
// block of them has to measure exactly as many cells as it occupies -- every
// layout on the page is placed around it.
func TestLogoBoxMeasuresAsPlainCells(t *testing.T) {
	for _, size := range [][2]int{{26, 13}, {20, 10}, {40, 20}} {
		box := logoBox(size[0], size[1])
		if w := lipgloss.Width(box); w != size[0] {
			t.Errorf("%dx%d box measured %d columns wide", size[0], size[1], w)
		}
		if h := lipgloss.Height(box); h != size[1] {
			t.Errorf("%dx%d box measured %d rows tall", size[0], size[1], h)
		}
	}
}

// Every cell names its own row and column in the picture, and carries the
// image id in its colour. Get any of that wrong and the terminal draws
// nothing, or draws the wrong part of the picture.
func TestLogoCellsNameTheirPlace(t *testing.T) {
	box := logoBox(3, 2)
	for r, line := range strings.Split(box, "\n") {
		runes := []rune(stripSGR(line))
		if len(runes) != 9 { // three cells of placeholder + two marks
			t.Fatalf("row %d has %d runes, want 9: %q", r, len(runes), string(runes))
		}
		for c := 0; c < 3; c++ {
			cell := runes[c*3 : c*3+3]
			if cell[0] != kitty.Placeholder {
				t.Errorf("row %d cell %d does not start with the placeholder", r, c)
			}
			if cell[1] != kitty.Diacritic(r) {
				t.Errorf("row %d cell %d names row %q", r, c, cell[1])
			}
			if cell[2] != kitty.Diacritic(c) {
				t.Errorf("row %d cell %d names column %q", r, c, cell[2])
			}
		}
	}
	// 7311 is 0x001C8F, so the colour is #001c8f.
	if !strings.Contains(box, "0;28;143") && !strings.Contains(box, "001c8f") {
		t.Errorf("the image id is not carried in the colour:\n%q", box[:min(len(box), 60)])
	}
}

func stripSGR(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// The picture goes over once, as a virtual placement: the terminal holds it
// and draws it only where the cells appear. Nothing about it moves the cursor.
func TestTransmissionIsVirtualAndMovesNothing(t *testing.T) {
	out := transmitLogo(26, 11)
	if !strings.Contains(out, "U=1") {
		t.Error("not a virtual placement: the terminal would draw it at the cursor")
	}
	if !strings.Contains(out, "c=26,r=11") {
		t.Error("the transmission does not say how big to draw it")
	}
	for _, forbidden := range []string{"\x1b7", "\x1b8", "\x1b[", "a=p"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the transmission contains %q, which moves or places something", forbidden)
		}
	}

	// The chunks have to reassemble into the picture exactly.
	parts := strings.Split(out, "\x1b\\")
	var payload strings.Builder
	for i, part := range parts[:len(parts)-1] {
		if !strings.HasPrefix(part, "\x1b_G") {
			t.Fatalf("chunk %d is not an APC sequence", i)
		}
		_, data, _ := strings.Cut(strings.TrimPrefix(part, "\x1b_G"), ";")
		payload.WriteString(data)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatalf("the chunks do not reassemble: %v", err)
	}
	if len(decoded) != len(logoPNG) {
		t.Errorf("reassembled %d bytes, the picture is %d", len(decoded), len(logoPNG))
	}
}

// It is sent once, and again only when the size it should be drawn at changes.
func TestPictureIsSentOnlyWhenItHasTo(t *testing.T) {
	m := New(Options{Demo: true})
	m.Graphics = true
	if m.sendImage() == nil {
		t.Fatal("the picture was never sent")
	}
	for i := 0; i < 20; i++ {
		if cmd := m.sendImage(); cmd != nil {
			t.Fatal("the picture was sent again while nothing changed")
		}
	}
	m.cellW, m.cellH = 8, 19 // the terminal answers, and the shape changes
	if m.sendImage() == nil {
		t.Error("the picture was not sent again at its new size")
	}
}

func TestOnlyTheTerminalsAnswerTurnsPicturesOn(t *testing.T) {
	answer := func(id int, payload string) bool {
		m := New(Options{Demo: true})
		drive(t, m, uv.KittyGraphicsEvent{
			Options: kitty.Options{ID: id},
			Payload: []byte(payload),
		})
		return m.Graphics
	}
	if !answer(logoImageID, "OK") {
		t.Error("a terminal saying OK was not believed")
	}
	if answer(logoImageID, "ENOTSUPPORTED:no graphics") {
		t.Error("a refusal was read as support")
	}
	if answer(1, "OK") {
		t.Error("an answer about another image was taken as ours")
	}
}

func TestGraphicsDetection(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"ghostty", map[string]string{"TERM": "xterm-ghostty"}, true},
		{"foot", map[string]string{"TERM": "foot"}, true}, // ask; the answer decides
		{"linux console", map[string]string{"TERM": "linux"}, false},
		{"inside tmux", map[string]string{"TERM": "xterm-ghostty", "TMUX": "/tmp/tmux-1000/default"}, false},
		{"turned off", map[string]string{"TERM": "xterm-ghostty", "VOIDBLEED_GRAPHICS": "0"}, false},
		{"turned on inside tmux", map[string]string{"TMUX": "x", "VOIDBLEED_GRAPHICS": "1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"TERM", "TERM_PROGRAM", "TMUX", "STY", "KITTY_WINDOW_ID", "VOIDBLEED_GRAPHICS"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := graphicsAllowed(); got != tc.want {
				t.Errorf("asking the terminal = %v, want %v", got, tc.want)
			}
		})
	}
}

// The overview is the only page that draws the cells, so leaving it takes the
// picture with it -- no deletion to remember, and nothing to leave behind.
func TestOnlyTheOverviewDrawsThePicture(t *testing.T) {
	m := New(Options{Demo: true})
	m.Graphics = true
	drive(t, m, tea.WindowSizeMsg{Width: 140, Height: 45})
	runCmd(t, m, m.pages[m.cur].Load(m), 0)
	if !strings.ContainsRune(view(m), kitty.Placeholder) {
		t.Fatal("the overview did not draw the picture")
	}
	drive(t, m, key("tab"))
	if strings.ContainsRune(view(m), kitty.Placeholder) {
		t.Error("another page drew the picture")
	}
}
