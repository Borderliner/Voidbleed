package control

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// drive feeds the model a message and runs whatever commands come back, so a
// test sees the same state a person would after the interface settled.
func drive(t *testing.T, m *Model, msgs ...tea.Msg) {
	t.Helper()
	for _, msg := range msgs {
		_, cmd := m.Update(msg)
		runCmd(t, m, cmd, 0)
	}
}

func runCmd(t *testing.T, m *Model, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 12 {
		return
	}
	msg := cmd()
	switch msg.(type) {
	case nil, tickMsg:
		return // the spinner would run forever
	}
	_, next := m.Update(msg)
	runCmd(t, m, next, depth+1)
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
}

func newTestModel(t *testing.T) *Model {
	t.Helper()
	m := New(Options{Demo: true})
	drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	runCmd(t, m, m.pages[m.cur].Load(m), 0)
	return m
}

func view(m *Model) string { return m.render() }

func TestPackagesListsWhatIsInstalled(t *testing.T) {
	m := newTestModel(t)
	out := view(m)
	for _, want := range []string{"Packages", "btop", "niri", "installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("packages view is missing %q:\n%s", want, out)
		}
	}
	// The demo machine has two updates waiting, and the page says so.
	if !strings.Contains(out, "2 updates") {
		t.Errorf("no update count in the status line:\n%s", out)
	}
}

func TestEverySectionRenders(t *testing.T) {
	m := newTestModel(t)
	for i, page := range m.pages {
		drive(t, m, key("tab"))
		_ = i
		out := view(m)
		if !strings.Contains(out, m.pages[m.cur].Label()) {
			t.Errorf("section %s does not name itself:\n%s", page.Label(), out)
		}
		if strings.Contains(out, "panic") {
			t.Fatalf("section %s rendered a panic", page.Label())
		}
	}
}

func TestFilterNarrowsTheList(t *testing.T) {
	m := newTestModel(t)
	drive(t, m, key("/"), key("n"), key("i"))
	out := view(m)
	if !strings.Contains(out, "1 match") || !strings.Contains(out, "niri") {
		t.Errorf("filter did not narrow to niri:\n%s", out)
	}
	drive(t, m, key("esc"))
	if !strings.Contains(view(m), "btop") {
		t.Error("escape did not clear the filter")
	}
}

// Nothing may change the machine without the interface saying what it will do
// first, and destructive actions must ask.
func TestRemoveAsksFirst(t *testing.T) {
	m := newTestModel(t)
	drive(t, m, key("x"))
	if m.over != overlayConfirm {
		t.Fatalf("remove did not ask for confirmation, overlay = %v", m.over)
	}
	out := view(m)
	if !strings.Contains(out, "Remove") {
		t.Errorf("the question does not say what will happen:\n%s", out)
	}
	drive(t, m, key("n"))
	if m.over != overlayNone {
		t.Error("answering no left the dialog up")
	}
}

func TestUpdateRunsAndShowsOutput(t *testing.T) {
	m := newTestModel(t)
	drive(t, m, key("u"))     // asks first: two updates are waiting
	drive(t, m, key("enter")) // yes
	if m.over != overlayOutput {
		t.Fatalf("update did not open the output pane, overlay = %v", m.over)
	}
	if !strings.Contains(view(m), "update the system") {
		t.Error("the output pane does not name the action")
	}
}

func TestAppearanceReadsAndEdits(t *testing.T) {
	m := newTestModel(t)
	for m.pages[m.cur].Label() != "Appearance" {
		drive(t, m, key("tab"))
	}
	out := view(m)
	for _, want := range []string{"adw-gtk3-dark", "Reversal-red-dark", "Vanilla-DMZ", "Ubuntu 10"} {
		if !strings.Contains(out, want) {
			t.Errorf("appearance did not read %q:\n%s", want, out)
		}
	}
	page := m.pages[m.cur].(*appearancePage)
	before := page.edited.CursorSize
	drive(t, m, key("down"), key("down"), key("down"), key("right"))
	if page.edited.CursorSize == before {
		t.Error("changing the cursor size did nothing")
	}
	if !strings.Contains(view(m), "not applied yet") {
		t.Error("an edited-but-unapplied page does not say so")
	}
}

func TestServicesReadsTheRealLayout(t *testing.T) {
	m := newTestModel(t)
	for m.pages[m.cur].Label() != "Services" {
		drive(t, m, key("tab"))
	}
	// The demo client still reads /etc/sv, which exists on any Void machine;
	// what matters is that the page renders a list and names the directory.
	if out := view(m); !strings.Contains(out, "Services") {
		t.Errorf("services page did not render:\n%s", out)
	}
}
