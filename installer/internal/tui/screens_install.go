package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"voidbleed/installer/internal/config"
	"voidbleed/installer/internal/engine"
	"voidbleed/installer/internal/sys"
)

func ansi_truncate(s string, width int, tail string) string { return ansi.Truncate(s, width, tail) }

// ── Review ──────────────────────────────────────────────────────────────────

type reviewScreen struct {
	confirm  textinput.Model
	plan     *engine.Plan
	err      string
	showPlan bool
	scroll   int
	preview  []string
}

func newReviewScreen() *reviewScreen {
	in := newInput("", false, 32)
	in.SetWidth(16)
	return &reviewScreen{confirm: in}
}

func (*reviewScreen) label() string { return "Review" }
func (*reviewScreen) heading() (string, string) {
	return "Review", "Check everything below. Installing erases the selected disk."
}

func (sc *reviewScreen) enter(m *Model) tea.Cmd {
	sc.confirm.SetValue("")
	sc.showPlan = false
	sc.scroll = 0
	plan, err := engine.NewPlan(m.st.cfg, m.st.facts, m.st.cat, engine.DefaultPaths())
	sc.plan, sc.err = plan, ""
	if err != nil {
		sc.err = err.Error()
		return nil
	}
	// A dry run of the exact steps, shown on request.
	d := sys.NewDryRun()
	d.DefaultOutput = func(cmd sys.Cmd) string {
		if cmd.Name == "blkid" {
			return "<uuid>"
		}
		return ""
	}
	previewPlan := *plan
	previewPlan.Facts.UEFI, previewPlan.Facts.UEFI64 = true, true
	engine.Run(context.Background(), &engine.Exec{Plan: &previewPlan, R: d}, engine.Steps(&previewPlan), nil)
	sc.preview = nil
	checks := 0
	for _, line := range strings.Split(strings.TrimRight(d.Transcript(), "\n"), "\n") {
		if strings.HasPrefix(line, "query xbps-query -p pkgver ") {
			checks++
			continue
		}
		if checks > 0 {
			sc.preview = append(sc.preview, fmt.Sprintf("check %d packages are on the live image", checks))
			checks = 0
		}
		sc.preview = append(sc.preview, line)
	}
	return sc.confirm.Focus()
}

func (sc *reviewScreen) diskName(m *Model) string {
	return strings.TrimPrefix(m.st.cfg.Disk.Device, "/dev/")
}

func (sc *reviewScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	k, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		var cmd tea.Cmd
		sc.confirm, cmd = sc.confirm.Update(msg)
		return cmd, stay
	}
	switch k.String() {
	case "esc":
		if sc.showPlan {
			sc.showPlan = false
			return nil, stay
		}
		return nil, back
	case "ctrl+d":
		sc.showPlan = !sc.showPlan
		sc.scroll = 0
		return nil, stay
	case "up":
		sc.scroll = max(0, sc.scroll-1)
		return nil, stay
	case "down":
		sc.scroll++
		return nil, stay
	case "pgdown":
		sc.scroll += 15
		return nil, stay
	case "pgup":
		sc.scroll = max(0, sc.scroll-15)
		return nil, stay
	case "enter":
		if sc.plan != nil && sc.confirm.Value() == sc.diskName(m) {
			return nil, next
		}
		return nil, stay
	}
	var cmd tea.Cmd
	sc.confirm, cmd = sc.confirm.Update(msg)
	return cmd, stay
}

func (sc *reviewScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	if sc.err != "" {
		return s.warn.Render(m.glyphs.Warn+" "+sc.err) + "\n\n" + s.muted.Render("Go back and fix this.")
	}
	if sc.plan == nil {
		return ""
	}
	if sc.showPlan {
		rows := max(1, height-2)
		sc.scroll = min(sc.scroll, max(0, len(sc.preview)-rows))
		visible := sc.preview[sc.scroll:min(len(sc.preview), sc.scroll+rows)]
		for i, l := range visible {
			visible[i] = s.dim.Render(ansi.Truncate(l, width, m.glyphs.Ellipsis))
		}
		return s.muted.Render(fmt.Sprintf("Every action, in order (%d–%d of %d)", sc.scroll+1, sc.scroll+len(visible), len(sc.preview))) +
			"\n" + strings.Join(visible, "\n")
	}

	c := m.st.cfg
	p := sc.plan
	disk, _ := m.selectedDisk()
	colW := max(30, (width-4)/2)
	const keyW = 12
	row := func(k, v string) string {
		return s.muted.Width(keyW).Render(k) + hangingAt(s.text, v, colW-keyW, keyW)
	}

	storage := string(c.Disk.Filesystem)
	if c.Disk.Encrypt {
		storage += ", encrypted (LUKS2)"
	}
	swap := string(c.Swap.Mode)
	if c.Swap.Mode == config.SwapFile || c.Swap.Mode == config.SwapPartition {
		swap += fmt.Sprintf(", %s", humanBytes(uint64(c.Swap.SizeMiB)<<20))
	}
	var groupNames []string
	for _, id := range p.Groups {
		g, _ := m.st.cat.Group(id)
		groupNames = append(groupNames, g.Name)
	}
	if len(groupNames) == 0 {
		groupNames = []string{"none"}
	}
	root := "locked (sudo)"
	if c.User.RootPassword != "" {
		root = "password set"
	}

	left := strings.Join([]string{
		s.section.Render("SYSTEM"),
		row("Keyboard", strings.Join(c.System.KeyboardLayouts, ", ")),
		row("Language", c.System.Locale),
		row("Time zone", c.System.Timezone),
		row("Computer", c.System.Hostname),
		"",
		s.section.Render("ACCOUNT"),
		row("User", c.User.Name+ifNonEmpty(" ("+c.User.FullName+")", c.User.FullName)),
		row("Shell", "fish"),
		row("Root", root),
	}, "\n")
	right := strings.Join([]string{
		s.section.Render("STORAGE"),
		row("Disk", strings.TrimPrefix(disk.Path, "/dev/")+"  "+humanBytes(disk.SizeBytes)),
		row("Filesystem", storage),
		row("Swap", swap),
		row("Boot", "UEFI, GRUB, "+p.Kernel),
		"",
		s.section.Render("SOFTWARE"),
		row("Source", map[config.Source]string{config.SourceOffline: "offline copy", config.SourceNetwork: "network"}[c.Install.Source]),
		row("Extras", strings.Join(groupNames, ", ")),
	}, "\n")
	summary := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(colW).Render(left), "  ",
		lipgloss.NewStyle().Width(colW).Render(right))

	model := disk.Model
	if model == "" {
		model = "disk"
	}
	warning := s.fail.Render(m.glyphs.Warn+"  Everything on "+sc.diskName(m)+" will be erased") + "\n" +
		s.muted.Render(model+", "+humanBytes(disk.SizeBytes)) + "\n\n" +
		s.text.Render("Type ") + s.accent.Render(sc.diskName(m)) + s.text.Render(" to confirm  ") + sc.confirm.View()
	if sc.confirm.Value() == sc.diskName(m) {
		warning += "\n" + s.key.Render("enter") + s.accent.Render(" start installing")
	} else {
		warning += "\n" + s.dim.Render(" ")
	}
	return summary + "\n\n" + s.danger.Width(min(width, 72)).Render(warning)
}

// hangingAt wraps text to width, indenting continuation lines by indent so
// they line up under the first line's value.
func hangingAt(style lipgloss.Style, text string, width, indent int) string {
	lines := strings.Split(lipgloss.Wrap(text, max(10, width), " "), "\n")
	for i, l := range lines {
		if i > 0 {
			l = strings.Repeat(" ", indent) + l
		}
		lines[i] = style.Render(l)
	}
	return strings.Join(lines, "\n")
}

func ifNonEmpty(s, cond string) string {
	if cond == "" {
		return ""
	}
	return s
}

func (sc *reviewScreen) help(m *Model) []binding {
	if sc.showPlan {
		return []binding{{m.glyphs.UpDown, "scroll"}, {"ctrl+d", "summary"}, {"esc", "back"}}
	}
	return []binding{{"ctrl+d", "show every command"}, {"enter", "install"}, {"esc", "back"}}
}

// ── Install ─────────────────────────────────────────────────────────────────

type (
	eventMsg engine.Event
	logMsg   string
	doneMsg  struct{ err error }
)

type installScreen struct {
	steps    []engine.Step
	status   []engine.EventKind // per step; -1 = pending
	elapsed  []time.Duration
	current  int
	progress float64
	started  time.Time
	logs     []string
	msgs     chan tea.Msg
	finished bool
	err      error
}

func (*installScreen) label() string { return "Install" }

func (sc *installScreen) heading() (string, string) {
	switch {
	case sc.finished && sc.err == nil:
		return "Voidbleed is installed", "Remove the installation media, then reboot into your new system."
	case sc.finished:
		return "Installation failed", "The disk was unmounted and nothing else will be changed."
	default:
		return "Installing", "This takes a few minutes. Don't turn the computer off."
	}
}

func (sc *installScreen) enter(m *Model) tea.Cmd {
	if sc.msgs != nil {
		return nil
	}
	m.locked = true
	paths := engine.DefaultPaths()
	if m.st.opts.LogPath != "" {
		paths.Log = m.st.opts.LogPath
	}
	plan, err := engine.NewPlan(m.st.cfg, m.st.facts, m.st.cat, paths)
	if err != nil {
		sc.finished, sc.err = true, err
		m.locked = false
		return nil
	}
	sc.steps = engine.Steps(plan)
	sc.status = make([]engine.EventKind, len(sc.steps))
	for i := range sc.status {
		sc.status[i] = -1
	}
	sc.elapsed = make([]time.Duration, len(sc.steps))
	sc.started = time.Now()
	sc.msgs = make(chan tea.Msg, 4096)

	runner, closeLog := m.runner(sc.msgs)
	go func() {
		events := make(chan engine.Event, 16)
		go func() {
			for e := range events {
				sc.msgs <- eventMsg(e)
			}
		}()
		err := engine.Run(context.Background(), &engine.Exec{Plan: plan, R: runner}, sc.steps, events)
		close(events)
		closeLog()
		sc.msgs <- doneMsg{err}
	}()
	return sc.wait()
}

func (sc *installScreen) wait() tea.Cmd {
	return func() tea.Msg { return <-sc.msgs }
}

func (sc *installScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	switch msg := msg.(type) {
	case eventMsg:
		e := engine.Event(msg)
		sc.progress = e.Progress
		if e.Kind != engine.Finished {
			sc.current = e.Index
			sc.status[e.Index] = e.Kind
			if e.Kind != engine.StepStarted {
				sc.elapsed[e.Index] = e.Elapsed
			}
		}
		return sc.wait(), stay
	case logMsg:
		sc.logs = append(sc.logs, string(msg))
		if len(sc.logs) > 400 {
			sc.logs = sc.logs[len(sc.logs)-400:]
		}
		return sc.wait(), stay
	case doneMsg:
		sc.finished, sc.err = true, msg.err
		m.locked = false
		return nil, stay
	case tea.KeyPressMsg:
		if !sc.finished {
			return nil, stay
		}
		switch msg.String() {
		case "r":
			if sc.err == nil && !m.st.opts.Demo {
				return tea.Sequence(tea.Quit, func() tea.Msg { _ = exec.Command("reboot").Run(); return nil }), stay
			}
			return tea.Quit, stay
		case "q", "enter", "esc":
			return tea.Quit, stay
		}
	}
	return nil, stay
}

func (sc *installScreen) progressBar() *tea.ProgressBar {
	if sc.msgs == nil {
		return nil
	}
	switch {
	case sc.finished && sc.err != nil:
		return tea.NewProgressBar(tea.ProgressBarError, int(sc.progress*100))
	case sc.finished:
		return nil
	}
	return tea.NewProgressBar(tea.ProgressBarDefault, int(sc.progress*100))
}

func (sc *installScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	if sc.finished && sc.err == nil {
		return sc.successView(m, width)
	}

	// Overall progress bar.
	barW := max(10, width-18)
	filled := int(sc.progress * float64(barW))
	bar := s.accent.Render(strings.Repeat(m.glyphs.Bar, filled)) + s.dim.Render(strings.Repeat(m.glyphs.BarEmpty, barW-filled))
	elapsed := time.Since(sc.started).Round(time.Second)
	head := bar + "  " + s.text.Bold(true).Render(fmt.Sprintf("%3d%%", int(sc.progress*100))) + "  " + s.dim.Render(elapsed.String())

	var steps []string
	for i, st := range sc.steps {
		var mark, title string
		switch sc.status[i] {
		case engine.StepStarted:
			mark, title = m.spinner(), s.text.Bold(true).Render(st.Title)
		case engine.StepFinished:
			mark, title = s.secondary.Render(m.glyphs.Done), s.muted.Render(st.Title)
		case engine.StepFailed:
			mark, title = s.fail.Render(m.glyphs.Fail), s.fail.Render(st.Title)
		default:
			mark, title = s.dim.Render(m.glyphs.Todo), s.dim.Render(st.Title)
		}
		dur := ""
		if sc.elapsed[i] > 0 {
			dur = s.dim.Render("  " + sc.elapsed[i].Round(time.Second).String())
		}
		steps = append(steps, mark+"  "+title+dur)
	}
	stepList := strings.Join(steps, "\n")

	logH := max(3, height-lipgloss.Height(stepList)-5)
	if sc.err != nil {
		logH = max(3, logH-4)
	}
	var tail []string
	for _, l := range sc.logs[max(0, len(sc.logs)-logH):] {
		tail = append(tail, s.dim.Render(ansi.Truncate(l, width-2, m.glyphs.Ellipsis)))
	}
	logBox := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false, false, false).BorderForeground(colOutline).
		Width(width).Render(strings.Join(tail, "\n"))

	out := head + "\n\n" + stepList + "\n\n" + logBox
	if sc.err != nil {
		msg := strings.TrimSpace(sc.err.Error())
		if lines := strings.Split(msg, "\n"); len(lines) > 6 {
			msg = strings.Join(lines[len(lines)-6:], "\n")
		}
		out = s.danger.Width(min(width, 96)).Render(s.fail.Render("Error")+"\n"+s.text.Render(lipgloss.Wrap(msg, min(width, 96)-6, " "))) +
			"\n" + s.dim.Render("Full log: "+m.st.opts.LogPath) + "\n\n" + stepList + "\n\n" + logBox
	}
	return out
}

func (sc *installScreen) successView(m *Model, width int) string {
	s := m.st.styles
	c := m.st.cfg
	lines := []string{
		m.wordmark(),
		s.muted.Italic(true).Render("Bleed into the void."),
		"",
		s.text.Render(fmt.Sprintf("Installed in %s.", time.Since(sc.started).Round(time.Second))),
		s.text.Render("Log in as ") + s.accent.Render(c.User.Name) + s.text.Render(" after rebooting."),
	}
	if c.Disk.Encrypt {
		lines = append(lines, s.text.Render("You'll be asked for the disk passphrase first."))
	}
	lines = append(lines, "", s.key.Render("r")+s.keyDesc.Render(" reboot now   ")+s.key.Render("q")+s.keyDesc.Render(" back to the live system"))
	right := strings.Join(lines, "\n")
	if width < 70 {
		return right
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, m.logo(), "     ", right)
}

func (sc *installScreen) help(m *Model) []binding {
	if !sc.finished {
		return []binding{{"", "installing" + m.glyphs.Ellipsis}}
	}
	if sc.err != nil {
		return []binding{{"q", "exit"}}
	}
	return []binding{{"r", "reboot"}, {"q", "exit"}}
}
