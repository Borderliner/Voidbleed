package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"voidbleed/installer/internal/engine"
	"voidbleed/installer/internal/hw"
)

// ── Welcome ─────────────────────────────────────────────────────────────────

type welcomeScreen struct{}

func (*welcomeScreen) label() string { return "Welcome" }
func (*welcomeScreen) heading() (string, string) {
	return "Welcome", "This installer sets up Voidbleed on a whole disk. Nothing changes until you confirm on the review screen."
}
func (*welcomeScreen) enter(*Model) tea.Cmd { return nil }

func (*welcomeScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	if k, ok := msg.(tea.KeyPressMsg); ok && (k.String() == "enter" || k.String() == "right") {
		if m.st.facts.UEFI64 {
			return nil, next
		}
	}
	return nil, stay
}

func (*welcomeScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	f := m.st.facts
	check := func(ok bool, text string) string {
		if ok {
			return s.secondary.Render(m.glyphs.Done) + "  " + s.text.Render(text)
		}
		return s.fail.Render(m.glyphs.Fail) + "  " + s.warn.Render(text)
	}

	var gpus []string
	for _, g := range f.GPUs {
		name := map[hw.Vendor]string{hw.Intel: "Intel", hw.AMD: "AMD", hw.NVIDIA: "NVIDIA", hw.Virtual: "Virtual GPU"}[g.Vendor]
		if name == "" {
			name = "Other"
		}
		if g.NvidiaGen != "" {
			name += " (" + g.NvidiaGen + ")"
		}
		gpus = append(gpus, name)
	}
	usable := 0
	for _, d := range f.Disks {
		if !d.LiveMedium && d.SizeBytes >= engine.MinDiskBytes {
			usable++
		}
	}

	network := m.spinner() + "  " + s.muted.Render("Checking the network"+m.glyphs.Ellipsis)
	if o := m.st.online; o != nil {
		if *o {
			network = check(true, "Online")
		} else {
			network = s.dim.Render(m.glyphs.Todo) + "  " + s.muted.Render("Offline: the live image will be copied")
		}
	}

	facts := []string{
		check(f.UEFI64, map[bool]string{true: "UEFI firmware, 64-bit", false: "Needs 64-bit UEFI: reboot the USB in UEFI mode"}[f.UEFI64]),
		check(true, fmt.Sprintf("%s CPU %s %.1f GiB memory", strings.ToUpper(string(f.CPU)[:1])+string(f.CPU)[1:], m.glyphs.Bullet, float64(f.MemoryMiB)/1024)),
		check(len(gpus) > 0, "Graphics: "+strings.Join(gpus, " + ")),
		check(usable > 0, fmt.Sprintf("%d disk%s to install on", usable, plural(usable, "s"))),
		network,
	}

	right := m.wordmark() + "\n" + s.muted.Italic(true).Render("Bleed into the void.") + "\n\n" +
		strings.Join(facts, "\n") + "\n\n"
	if f.UEFI64 {
		right += s.text.Render("Press ") + s.key.Render("enter") + s.text.Render(" to begin.")
	}
	if width < 70 {
		return right
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, m.logo(), "     ", right)
}

func (*welcomeScreen) help(m *Model) []binding { return []binding{{"enter", "begin"}} }

// ── Keyboard ────────────────────────────────────────────────────────────────

type keyboardScreen struct{ p *picker }

func newKeyboardScreen(st *state) *keyboardScreen {
	p := newPicker(keyboardLayouts(), true)
	p.maxMulti = 4
	p.chosen = append([]string{}, st.cfg.System.KeyboardLayouts...)
	p.focus(p.chosen[0])
	return &keyboardScreen{p: p}
}

func (*keyboardScreen) label() string { return "Keyboard" }
func (*keyboardScreen) heading() (string, string) {
	return "Keyboard layout", "Pick up to four layouts. The first is the default; switch between them with Super+Space."
}
func (*keyboardScreen) enter(*Model) tea.Cmd { return nil }

func (sc *keyboardScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, stay
	}
	if k.String() == "esc" {
		return nil, back
	}
	if sc.p.handle(k) && len(sc.p.chosen) > 0 {
		m.st.cfg.System.KeyboardLayouts = append([]string{}, sc.p.chosen...)
		m.st.cfg.System.Keymap = consoleKeymap(sc.p.chosen[0])
		return nil, next
	}
	return nil, stay
}

func (sc *keyboardScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	names := map[string]string{}
	for _, o := range sc.p.all {
		names[o.Value] = o.Label
	}
	var chosen []string
	for i, v := range sc.p.chosen {
		name := names[v]
		if i == 0 {
			name = s.accent.Render(name) + s.dim.Render(" (default)")
		} else {
			name = s.text.Render(name)
		}
		chosen = append(chosen, name)
	}
	summary := s.muted.Render("Selected  ") + strings.Join(chosen, s.dim.Render("  ·  "))
	if len(chosen) == 0 {
		summary = s.warn.Render("Select at least one layout with space.")
	}
	return summary + "\n\n" + sc.p.view(m, width, height-2)
}

func (*keyboardScreen) help(m *Model) []binding {
	return []binding{{"space", "select"}, {"enter", "continue"}, {"esc", "back"}}
}

// ── Language ────────────────────────────────────────────────────────────────

type listScreen struct {
	p          *picker
	labelText  string
	title, sub string
	apply      func(m *Model, value string)
	current    func(m *Model) string
}

func (sc *listScreen) label() string             { return sc.labelText }
func (sc *listScreen) heading() (string, string) { return sc.title, sc.sub }
func (sc *listScreen) enter(m *Model) tea.Cmd {
	sc.p.value = sc.current(m)
	sc.p.focus(sc.p.value)
	return nil
}

func (sc *listScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, stay
	}
	if k.String() == "esc" {
		return nil, back
	}
	if sc.p.handle(k) && sc.p.value != "" {
		sc.apply(m, sc.p.value)
		return nil, next
	}
	return nil, stay
}

func (sc *listScreen) view(m *Model, width, height int) string { return sc.p.view(m, width, height) }

func (*listScreen) help(m *Model) []binding {
	return []binding{{m.glyphs.UpDown, "move"}, {"enter", "choose"}, {"esc", "back"}}
}

func newLocaleScreen(st *state) *listScreen {
	return &listScreen{
		p: newPicker(locales(), false), labelText: "Language",
		title: "Language", sub: "The system language, and the formats for dates, numbers and currency.",
		apply:   func(m *Model, v string) { m.st.cfg.System.Locale = v },
		current: func(m *Model) string { return m.st.cfg.System.Locale },
	}
}

func newTimezoneScreen(st *state) *listScreen {
	return &listScreen{
		p: newPicker(timezones(), false), labelText: "Time zone",
		title: "Time zone", sub: "Search by city, for example \"tehran\" or \"new york\".",
		apply:   func(m *Model, v string) { m.st.cfg.System.Timezone = v },
		current: func(m *Model) string { return m.st.cfg.System.Timezone },
	}
}

// ── Disk ────────────────────────────────────────────────────────────────────

type diskScreen struct{ cursor int }

func (*diskScreen) label() string { return "Disk" }
func (*diskScreen) heading() (string, string) {
	return "Installation disk", "Voidbleed uses the whole disk. Everything on it will be erased."
}

// problem explains why a disk can't be used, or returns "".
func diskProblem(d hw.Disk) string {
	switch {
	case d.LiveMedium:
		return "running the live system"
	case d.SizeBytes < engine.MinDiskBytes:
		return fmt.Sprintf("too small (needs %d GiB)", engine.MinDiskBytes>>30)
	}
	for _, p := range d.Partitions {
		if p.Mountpoint != "" {
			return "in use (" + p.Mountpoint + ")"
		}
	}
	return ""
}

func (sc *diskScreen) enter(m *Model) tea.Cmd {
	for i, d := range m.st.facts.Disks {
		if d.Path == m.st.cfg.Disk.Device {
			sc.cursor = i
			return nil
		}
	}
	for i, d := range m.st.facts.Disks {
		if diskProblem(d) == "" {
			sc.cursor = i
			break
		}
	}
	return nil
}

func (sc *diskScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, stay
	}
	disks := m.st.facts.Disks
	switch k.String() {
	case "esc":
		return nil, back
	case "up":
		sc.cursor = max(0, sc.cursor-1)
	case "down":
		sc.cursor = min(len(disks)-1, sc.cursor+1)
	case "enter":
		if len(disks) > 0 && diskProblem(disks[sc.cursor]) == "" {
			m.st.cfg.Disk.Device = disks[sc.cursor].Path
			return nil, next
		}
	}
	return nil, stay
}

func (sc *diskScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	if len(m.st.facts.Disks) == 0 {
		return s.warn.Render("No disks found.")
	}
	var b strings.Builder
	for i, d := range m.st.facts.Disks {
		problem := diskProblem(d)
		name := strings.TrimPrefix(d.Path, "/dev/")
		model := d.Model
		if model == "" {
			model = "Unknown disk"
		}
		transport := strings.ToUpper(d.Transport)
		line1 := fmt.Sprintf("%-10s %s", name, model)
		size := humanBytes(d.SizeBytes)

		var parts []string
		for _, p := range d.Partitions {
			desc := p.FSType
			if desc == "" {
				desc = "unformatted"
			}
			parts = append(parts, desc+" "+humanBytes(p.SizeBytes))
		}
		line2 := "empty"
		if len(parts) > 0 {
			line2 = fmt.Sprintf("%d partition%s: %s", len(parts), plural(len(parts), "s"), strings.Join(parts, ", "))
		}
		if problem != "" {
			line2 = problem
		}

		cursor := "  "
		title := s.text.Bold(true).Render(padRight(line1, width-22)) + s.muted.Render(padRight(size, 12)) + s.dim.Render(transport)
		detail := s.dim.Render(line2)
		if problem != "" {
			title = s.dim.Render(padRight(line1, width-22) + padRight(size, 12) + transport)
			detail = s.warn.Render(line2)
		}
		if i == sc.cursor {
			cursor = s.accent.Render(m.glyphs.Cursor) + " "
			if problem == "" {
				title = s.selected.Render(padRight(line1, width-22)) + s.accent.Render(padRight(size, 12)) + s.muted.Render(transport)
			}
		}
		fmt.Fprintf(&b, "%s%s\n    %s\n\n", cursor, title, detail)
	}
	return b.String()
}

func (*diskScreen) help(m *Model) []binding {
	return []binding{{m.glyphs.UpDown, "move"}, {"enter", "use this disk"}, {"esc", "back"}}
}

// selectedDisk returns the chosen disk's facts.
func (m *Model) selectedDisk() (hw.Disk, bool) {
	i := slices.IndexFunc(m.st.facts.Disks, func(d hw.Disk) bool { return d.Path == m.st.cfg.Disk.Device })
	if i < 0 {
		return hw.Disk{}, false
	}
	return m.st.facts.Disks[i], true
}
