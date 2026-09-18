package control

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"
)

// The box the picture goes into is plain cells, so the layout around it is
// the same whether the picture appears or not.
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

// The picture is sent once and placed cheaply, because placing happens again
// and again while the overview is on screen.
func TestPictureIsSentOnceAndPlacedCheaply(t *testing.T) {
	if len(transmitLogo()) < len(logoPNG) {
		t.Error("the transmission does not carry the picture")
	}
	place := placeLogoAt(10, 20, 26, 13)
	if len(place) > 128 {
		t.Errorf("placing the picture costs %d bytes", len(place))
	}
	for _, want := range []string{"\x1b7", "\x1b[10;20H", "a=p,i=7311", "c=26,r=13", "C=1", "\x1b8"} {
		if !strings.Contains(place, want) {
			t.Errorf("placement is missing %q:\n%q", want, place)
		}
	}
}

// The box is found by the marker it leaves, not by re-deriving the layout.
func TestMarkerLocatesTheBox(t *testing.T) {
	frame := "aaa\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#e8313f")).Render("xx") +
		logoBox(6, 2) + "\nzzz"
	row, col, cleaned, found := findMarker(frame)
	if !found {
		t.Fatal("marker not found")
	}
	if row != 2 || col != 3 {
		t.Errorf("marker at row %d col %d, want 2, 3", row, col)
	}
	if strings.Contains(cleaned, imageMarker) {
		t.Error("the marker was left in the frame")
	}
}

// Only the terminal's own answer turns pictures on: everything else leaves a
// hole in the page where a picture was reserved.
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

// Leaving the overview has to take the picture with it.
func TestPictureIsRemovedWhenSomethingElseIsDrawn(t *testing.T) {
	m := New(Options{Demo: true})
	m.Graphics = true
	drive(t, m, tea.WindowSizeMsg{Width: 140, Height: 45})
	runCmd(t, m, m.pages[m.cur].Load(m), 0)
	_ = view(m) // the overview reserves the box and records where it is
	if m.imageRow == 0 {
		t.Fatal("the overview did not reserve a box for the picture")
	}
	if cmd := m.drawImage(); cmd == nil {
		t.Fatal("nothing was sent to draw the picture")
	}
	drive(t, m, key("tab")) // on to the packages page
	_ = view(m)
	if m.imageRow != 0 {
		t.Fatal("another page reserved a box")
	}
	raw, ok := m.drawImage()().(tea.RawMsg)
	if !ok {
		t.Fatal("the picture was left on screen")
	}
	if !strings.Contains(raw.Msg.(string), "a=d,d=i") {
		t.Errorf("expected a delete, got %q", raw.Msg)
	}
}

// A cell is taller than it is wide, so a square picture needs fewer rows than
// columns -- and how many depends on the font the terminal is using.
func TestPictureComesOutSquare(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cellW, cellH int
		want         int
	}{
		{"no answer yet", 0, 0, 13}, // the usual one-to-two
		{"ubuntu mono at 14", 8, 19, 11},
		{"a wide font", 10, 20, 13},
		{"a tall font", 7, 20, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Options{Demo: true})
			m.cellW, m.cellH = tc.cellW, tc.cellH
			if got := m.logoRows(); got != tc.want {
				t.Errorf("%d×%d cells: %d columns wants %d rows, got %d",
					tc.cellW, tc.cellH, logoCols, tc.want, got)
			}
		})
	}
}

// The terminal's answer about its cell size has to reach the model.
func TestCellSizeIsRemembered(t *testing.T) {
	m := New(Options{Demo: true})
	drive(t, m, uv.CellSizeEvent{Width: 8, Height: 19})
	if m.cellW != 8 || m.cellH != 19 {
		t.Errorf("cell size read as %d×%d", m.cellW, m.cellH)
	}
}
