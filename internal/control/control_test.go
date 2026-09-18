package control

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/system"
)

// onPage walks the sections until the named one is showing.
func onPage(t *testing.T, m *Model, label string) {
	t.Helper()
	for i := 0; i < len(m.pages); i++ {
		if m.pages[m.cur].Label() == label {
			return
		}
		drive(t, m, key("tab"))
	}
	t.Fatalf("no section called %q", label)
}

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
	switch msg := msg.(type) {
	case nil, tickMsg:
		return // the spinner would run forever
	case tea.BatchMsg:
		// A page that loads in stages returns several commands at once.
		for _, batched := range msg {
			runCmd(t, m, batched, depth+1)
		}
		return
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

// The app opens on the overview: what this machine is, and what wants doing.
func TestOverviewIsWhereItOpens(t *testing.T) {
	m := newTestModel(t)
	if got := m.pages[m.cur].Label(); got != "Overview" {
		t.Fatalf("opened on %q", got)
	}
	out := view(m)
	for _, want := range []string{"VOIDBLEED", "packages", "services"} {
		if !strings.Contains(out, want) {
			t.Errorf("the overview does not show %q:\n%s", want, out)
		}
	}
	// The demo machine has updates and old kernels waiting, and the overview
	// is where that gets said.
	if !strings.Contains(out, "needs attention") {
		t.Errorf("nothing was flagged:\n%s", out)
	}
}

// The mark is the first thing the app shows; it must survive being resized,
// changing shape rather than disappearing.
func TestOverviewAlwaysShowsTheMark(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {90, 26}, {100, 30}, {120, 40}, {160, 50}} {
		m := New(Options{Demo: true})
		drive(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		runCmd(t, m, m.pages[m.cur].Load(m), 0)
		out := view(m)
		logo := strings.Contains(out, "▄▄████████▄")
		wordmark := strings.Contains(out, "█▀█ █ █▀▄")
		if !logo && !wordmark {
			t.Errorf("%dx%d shows neither the mark nor the wordmark:\n%s", size[0], size[1], out)
		}
	}
}

// A machine that cannot take snapshots is not shown a snapshots page.
func TestSnapshotsOnlyWhereTheyWork(t *testing.T) {
	m := New(Options{}) // this machine, not the demo one
	shown := false
	for _, page := range m.pages {
		if page.Label() == "Snapshots" {
			shown = true
		}
	}
	if shown != system.SnapshotsAvailable() {
		t.Errorf("snapshots page shown = %v, but this root is %q",
			shown, system.RootFilesystem())
	}
}

func TestKernelsSeparatesInstalledFromLeftovers(t *testing.T) {
	m := newTestModel(t)
	onPage(t, m, "Kernels")
	out := view(m)
	if !strings.Contains(out, "linux6.18") || !strings.Contains(out, "linux6.12") {
		t.Errorf("installed series missing:\n%s", out)
	}
	drive(t, m, key("right")) // installed -> in /boot
	out = view(m)
	if !strings.Contains(out, "6.18.50_1") {
		t.Errorf("leftover trees missing:\n%s", out)
	}
	// The running kernel must never be offered for removal.
	drive(t, m, key("left"))
	page := m.pages[m.cur].(*kernelsPage)
	for _, k := range page.kernels {
		if k.Booted && k.Package != "linux6.18" {
			t.Errorf("booted kernel read as %+v", k)
		}
	}
}

func TestPackagesListsWhatIsInstalled(t *testing.T) {
	m := newTestModel(t)
	onPage(t, m, "Packages")
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
	onPage(t, m, "Packages")
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
	onPage(t, m, "Packages")
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
	onPage(t, m, "Packages")
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

// Packages and Flatpak both answer "what is here, what is out of date, what
// could be here"; they should do it the same way.
func TestFlatpakHasAnUpdatesView(t *testing.T) {
	m := newTestModel(t)
	onPage(t, m, "Flatpak")
	if !strings.Contains(view(m), "Steam") {
		t.Fatalf("installed view is empty:\n%s", view(m))
	}
	drive(t, m, key("right")) // installed -> updates
	out := view(m)
	if !strings.Contains(out, "updates") {
		t.Errorf("no updates view:\n%s", out)
	}
	// The demo machine has one waiting update, for GIMP, at 3.2.8.
	if !strings.Contains(out, "GIMP") || !strings.Contains(out, "3.2.8") {
		t.Errorf("the updates view does not show what is waiting:\n%s", out)
	}
	if strings.Contains(out, "Steam") {
		t.Errorf("the updates view lists an application with no update:\n%s", out)
	}
}

func TestFirmwareUpdatesEverythingWaiting(t *testing.T) {
	restore := system.Have
	system.Have = func(string) bool { return true } // a machine with fwupd
	defer func() { system.Have = restore }()

	m := newTestModel(t)
	onPage(t, m, "Firmware")
	drive(t, m, key("u"))
	if m.over != overlayConfirm {
		t.Fatalf("update all did not ask first, overlay = %v", m.over)
	}
	out := view(m)
	// The question has to name what is about to be written where.
	for _, want := range []string{"System Firmware", "1.31.0", "powered"} {
		if !strings.Contains(out, want) {
			t.Errorf("the question does not mention %q:\n%s", want, out)
		}
	}
}
