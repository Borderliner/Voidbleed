package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/catalog"
	"voidbleed/internal/config"
	"voidbleed/internal/theme"
)

func testState(t *testing.T) *state {
	t.Helper()
	// Use the built-in lists so tests don't depend on the host's data files.
	xkbRulesFile, libcLocalesFile, zoneinfoDir = "/nonexistent", "/nonexistent", "/nonexistent"
	cat, err := catalog.Load("../../catalog")
	if err != nil {
		t.Fatal(err)
	}
	st := &state{
		opts: Options{Catalog: cat, Demo: true}, styles: newStyles(theme.Unicode), cat: cat,
		cfg: config.Default(), selected: map[string]bool{}, detected: map[string]bool{},
	}
	st.facts = demoFacts()
	st.livePackages = demoLivePackages(st)
	for _, id := range cat.DefaultSelection(st.facts) {
		st.selected[id] = true
	}
	return st
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func typeText(m *Model, text string) {
	for _, r := range text {
		m.Update(key(string(r)))
	}
}

func TestLoginFrom(t *testing.T) {
	for in, want := range map[string]string{
		"Reza Hajianpour": "reza",
		"  Ana-María ":    "anamara",
		"42 Bob":          "",
		"j0hn smith":      "j0hn",
	} {
		if got := loginFrom(in); got != want {
			t.Errorf("loginFrom(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStrength(t *testing.T) {
	cases := map[string]int{"": 0, "abc": 1, "abcdefgh": 2, "abcdefghijkl": 3, "Abcdefgh1!": 4, "correcthorsebatterystaple": 4}
	for pw, want := range cases {
		if got, _ := strength(pw); got != want {
			t.Errorf("strength(%q) = %d, want %d", pw, got, want)
		}
	}
}

func TestPickerFilterAndSelect(t *testing.T) {
	p := newPicker(fallbackLayouts, true)
	for _, r := range "pers" {
		p.handle(key(string(r)))
	}
	if len(p.visible) != 1 || p.all[p.visible[0]].Value != "ir" {
		t.Fatalf("filter 'pers' → %v", p.visible)
	}
	p.handle(key("space"))
	p.handle(key("backspace"))
	if p.filter != "per" || !slices.Equal(p.chosen, []string{"ir"}) {
		t.Fatalf("filter=%q chosen=%v", p.filter, p.chosen)
	}
	if !p.handle(key("enter")) {
		t.Fatal("enter not reported")
	}
}

func TestSoftwareExclusiveAndRequires(t *testing.T) {
	st := testState(t)
	m := newModel(st)
	sc := &softwareScreen{}
	sc.toggle(m, "power-tlp")
	if st.selected["power-ppd"] || !st.selected["power-tlp"] {
		t.Fatal("selecting TLP should replace power-profiles-daemon")
	}
	delete(st.selected, "flatpak")
	sc.toggle(m, "steam")
	if !st.selected["flatpak"] {
		t.Fatal("Steam should pull in Flatpak")
	}
	sc.toggle(m, "flatpak")
	if st.selected["steam"] {
		t.Fatal("removing Flatpak should remove Steam")
	}
}

// Walks every screen with the keyboard and checks the resulting config.
func TestFullWalkthrough(t *testing.T) {
	st := testState(t)
	m := newModel(st)
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 34})
	screen := func() string { return m.screens[m.cur].label() }
	expect := func(label string) {
		t.Helper()
		if screen() != label {
			t.Fatalf("on %q, want %q", screen(), label)
		}
		if out := m.render(); !strings.Contains(out, "VOIDBLEED") {
			t.Fatalf("%s did not render", label)
		}
	}

	expect("Welcome")
	m.Update(key("enter"))
	expect("Keyboard")
	typeText(m, "pers")
	m.Update(key("space"))
	m.Update(key("enter"))
	expect("Language")
	m.Update(key("enter"))
	expect("Time zone")
	typeText(m, "tehr")
	m.Update(key("enter"))
	expect("Disk")
	m.Update(key("down"))
	m.Update(key("down")) // the live USB stick: not selectable
	m.Update(key("enter"))
	expect("Disk")
	m.Update(key("up"))
	m.Update(key("up"))
	m.Update(key("enter"))
	expect("Storage")
	m.Update(key("right")) // btrfs
	m.Update(key("down"))
	m.Update(key("right")) // encrypt
	m.Update(key("down"))
	typeText(m, "hunter2hunter2")
	m.Update(key("down"))
	typeText(m, "hunter2hunter")
	m.Update(key("enter"))
	expect("Storage") // passphrases differ
	typeText(m, "2")
	m.Update(key("enter"))
	expect("Account")
	typeText(m, "Reza H")
	m.Update(key("down"))
	m.Update(key("down"))
	typeText(m, "Sup3r-secret!")
	m.Update(key("down"))
	typeText(m, "Sup3r-secret!")
	m.Update(key("down"))
	m.Update(key("enter"))
	expect("Software")
	m.Update(key("enter"))
	expect("Source")
	m.Update(key("enter"))
	expect("Review")

	c := st.cfg
	if c.Disk.Device != "/dev/nvme0n1" || c.Disk.Filesystem != config.Btrfs || !c.Disk.Encrypt || c.Disk.Passphrase != "hunter2hunter2" {
		t.Fatalf("disk config: %+v", c.Disk)
	}
	if !slices.Equal(c.System.KeyboardLayouts, []string{"us", "ir"}) || c.System.Timezone != "Asia/Tehran" {
		t.Fatalf("system config: %+v", c.System)
	}
	if c.User.Name != "reza" || c.User.FullName != "Reza H" || c.User.RootPassword != "" {
		t.Fatalf("user config: %+v", c.User)
	}
	if !slices.Contains(c.Install.Groups, "gpu-nvidia580") {
		t.Fatalf("groups: %v", c.Install.Groups)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("collected config invalid: %v", err)
	}

	m.Update(key("enter"))
	expect("Review") // not confirmed yet
	typeText(m, "nvme1n1")
	m.Update(key("enter"))
	expect("Review") // wrong disk name
	for range "nvme1n1" {
		m.Update(key("backspace"))
	}
	typeText(m, "nvme0n1")
	if m.screens[m.cur].(*reviewScreen).confirm.Value() != "nvme0n1" {
		t.Fatal("confirmation text not captured")
	}
}

func TestQuitNeedsConfirmation(t *testing.T) {
	m := newModel(testState(t))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil || !m.quitAsk {
		t.Fatal("ctrl+c should ask first")
	}
	m.Update(key("n"))
	if m.quitAsk {
		t.Fatal("any other key should cancel")
	}
	m.locked = true
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.quitAsk {
		t.Fatal("quitting must be impossible while installing")
	}
}
