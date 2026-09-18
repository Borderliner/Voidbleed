package control

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The picture is drawn with escape sequences sitting inside ordinary lines of
// spaces. If anything measured them as having width, every layout on the page
// would shift, so this is the property that matters most.
func TestLogoBoxMeasuresAsPlainCells(t *testing.T) {
	for _, size := range [][2]int{{26, 13}, {20, 10}, {40, 20}} {
		box := logoBox(size[0], size[1], true)
		if w := lipgloss.Width(box); w != size[0] {
			t.Errorf("%dx%d box measured %d columns wide", size[0], size[1], w)
		}
		if h := lipgloss.Height(box); h != size[1] {
			t.Errorf("%dx%d box measured %d rows tall", size[0], size[1], h)
		}
	}
}

// The picture goes to the terminal once; placing it afterwards must stay
// small, because it happens on every frame.
func TestPictureIsSentOnceAndPlacedCheaply(t *testing.T) {
	first := logoBox(26, 13, true)
	later := logoBox(26, 13, false)
	if len(first) < len(logoPNG) {
		t.Error("the first frame does not carry the picture")
	}
	if len(later) > 512 {
		t.Errorf("placing the picture costs %d bytes a frame", len(later))
	}
	if !strings.Contains(later, "a=p,i=") {
		t.Errorf("no placement escape:\n%q", later)
	}
	// Saved and restored: the cursor has to come back to where the frame
	// expects it, or the rest of the screen lands in the wrong place.
	if !strings.HasPrefix(strings.TrimLeft(later, " \n"), "\x1b7") || !strings.HasSuffix(later, "\x1b8") {
		t.Errorf("the cursor is not saved and restored:\n%q", later)
	}
}

func TestGraphicsDetection(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"ghostty", map[string]string{"TERM": "xterm-ghostty"}, true},
		{"kitty", map[string]string{"TERM": "xterm-kitty"}, true},
		{"wezterm", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "WezTerm"}, true},
		{"foot", map[string]string{"TERM": "foot"}, false},
		{"linux console", map[string]string{"TERM": "linux"}, false},
		{"inside tmux", map[string]string{"TERM": "xterm-ghostty", "TMUX": "/tmp/tmux-1000/default"}, false},
		{"turned off", map[string]string{"TERM": "xterm-ghostty", "VOIDBLEED_GRAPHICS": "0"}, false},
		{"turned on", map[string]string{"TERM": "foot", "VOIDBLEED_GRAPHICS": "1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"TERM", "TERM_PROGRAM", "TMUX", "STY", "KITTY_WINDOW_ID", "VOIDBLEED_GRAPHICS"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := graphicsEnv(); got != tc.want {
				t.Errorf("graphics = %v, want %v", got, tc.want)
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
	if !strings.Contains(view(m), "a=p,i=") {
		t.Fatal("the overview did not place the picture")
	}
	drive(t, m, key("tab")) // on to the packages page
	out := view(m)
	if !strings.Contains(out, "a=d,d=i") {
		t.Errorf("the picture was left on screen:\n%q", out[:min(len(out), 200)])
	}
	if strings.Contains(out, "a=p,i=") {
		t.Error("another page placed the picture")
	}
}
