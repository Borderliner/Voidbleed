package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"voidbleed/installer/internal/config"
)

// ── Storage ─────────────────────────────────────────────────────────────────

var (
	fsChoices   = []config.Filesystem{config.Ext4, config.Btrfs}
	swapChoices = []config.SwapMode{config.SwapZram, config.SwapFile, config.SwapPartition, config.SwapNone}
)

type storageField int

const (
	fieldFS storageField = iota
	fieldEncrypt
	fieldPass
	fieldPassConfirm
	fieldSwap
	fieldSwapSize
)

type storageScreen struct {
	focus                  storageField
	pass, confirm, swapMiB textinput.Model
	err                    string
}

func newStorageScreen() *storageScreen {
	return &storageScreen{
		pass:    newInput("at least 8 characters", true, 256),
		confirm: newInput("repeat the passphrase", true, 256),
		swapMiB: func() textinput.Model { in := newInput("MiB", false, 7); in.SetWidth(8); return in }(),
	}
}

func (*storageScreen) label() string { return "Storage" }
func (*storageScreen) heading() (string, string) {
	return "Storage", "Choose the filesystem, whether to encrypt it, and how to swap."
}

func (sc *storageScreen) fields(m *Model) []storageField {
	f := []storageField{fieldFS, fieldEncrypt}
	if m.st.cfg.Disk.Encrypt {
		f = append(f, fieldPass, fieldPassConfirm)
	}
	f = append(f, fieldSwap)
	if mode := m.st.cfg.Swap.Mode; mode == config.SwapFile || mode == config.SwapPartition {
		f = append(f, fieldSwapSize)
	}
	return f
}

func (sc *storageScreen) enter(m *Model) tea.Cmd {
	if sc.swapMiB.Value() == "" {
		size := min(max(m.st.facts.MemoryMiB, 2048), 8192)
		sc.swapMiB.SetValue(strconv.Itoa(size))
	}
	return sc.refocus(m)
}

func (sc *storageScreen) refocus(m *Model) tea.Cmd {
	sc.pass.Blur()
	sc.confirm.Blur()
	sc.swapMiB.Blur()
	switch sc.focus {
	case fieldPass:
		return sc.pass.Focus()
	case fieldPassConfirm:
		return sc.confirm.Focus()
	case fieldSwapSize:
		return sc.swapMiB.Focus()
	}
	return nil
}

func (sc *storageScreen) moveFocus(m *Model, delta int) tea.Cmd {
	fields := sc.fields(m)
	i := slices.Index(fields, sc.focus)
	sc.focus = fields[(i+delta+len(fields))%len(fields)]
	return sc.refocus(m)
}

func (sc *storageScreen) validate(m *Model) string {
	c := &m.st.cfg
	if c.Disk.Encrypt {
		switch {
		case len(sc.pass.Value()) < 8:
			return "The passphrase needs at least 8 characters."
		case sc.pass.Value() != sc.confirm.Value():
			return "The passphrases don't match."
		}
	}
	if c.Swap.Mode == config.SwapPartition && c.Disk.Encrypt {
		return "A swap partition can't be encrypted here; use a swap file or zram."
	}
	if c.Swap.Mode == config.SwapFile || c.Swap.Mode == config.SwapPartition {
		n, err := strconv.Atoi(sc.swapMiB.Value())
		if err != nil || n < 256 {
			return "Swap size must be at least 256 MiB."
		}
	}
	return ""
}

func (sc *storageScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	c := &m.st.cfg
	k, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return sc.forward(msg), stay
	}
	switch k.String() {
	case "esc":
		return nil, back
	case "tab", "down":
		return sc.moveFocus(m, 1), stay
	case "shift+tab", "up":
		return sc.moveFocus(m, -1), stay
	case "enter":
		if sc.err = sc.validate(m); sc.err != "" {
			return nil, stay
		}
		if c.Disk.Encrypt {
			c.Disk.Passphrase = sc.pass.Value()
		} else {
			c.Disk.Passphrase = ""
		}
		if n, err := strconv.Atoi(sc.swapMiB.Value()); err == nil {
			c.Swap.SizeMiB = n
		}
		return nil, next
	case "left", "right", "space":
		delta := 1
		if k.String() == "left" {
			delta = -1
		}
		switch sc.focus {
		case fieldFS:
			i := slices.Index(fsChoices, c.Disk.Filesystem)
			c.Disk.Filesystem = fsChoices[(i+delta+len(fsChoices))%len(fsChoices)]
			return nil, stay
		case fieldEncrypt:
			c.Disk.Encrypt = !c.Disk.Encrypt
			if c.Disk.Encrypt && c.Swap.Mode == config.SwapPartition {
				c.Swap.Mode = config.SwapFile
			}
			return nil, stay
		case fieldSwap:
			choices := sc.swapChoices(m)
			i := slices.Index(choices, c.Swap.Mode)
			c.Swap.Mode = choices[(i+delta+len(choices))%len(choices)]
			return nil, stay
		}
	}
	sc.err = ""
	return sc.forward(msg), stay
}

func (sc *storageScreen) swapChoices(m *Model) []config.SwapMode {
	if m.st.cfg.Disk.Encrypt {
		return slices.DeleteFunc(slices.Clone(swapChoices), func(s config.SwapMode) bool { return s == config.SwapPartition })
	}
	return swapChoices
}

func (sc *storageScreen) forward(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch sc.focus {
	case fieldPass:
		sc.pass, cmd = sc.pass.Update(msg)
	case fieldPassConfirm:
		sc.confirm, cmd = sc.confirm.Update(msg)
	case fieldSwapSize:
		sc.swapMiB, cmd = sc.swapMiB.Update(msg)
	}
	return cmd
}

func (sc *storageScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	c := m.st.cfg
	var rows []string

	fsNames := []string{"ext4", "btrfs"}
	rows = append(rows, m.choiceRow("Filesystem", fsNames, slices.Index(fsChoices, c.Disk.Filesystem), sc.focus == fieldFS))
	rows = append(rows, m.hint(map[config.Filesystem]string{
		config.Ext4:  "Simple and proven. The right choice for most people.",
		config.Btrfs: "Subvolumes for /, /home, snapshots and logs, with zstd compression.",
	}[c.Disk.Filesystem], width))
	rows = append(rows, "")

	encIdx := 0
	if c.Disk.Encrypt {
		encIdx = 1
	}
	rows = append(rows, m.choiceRow("Encryption", []string{"off", "LUKS2"}, encIdx, sc.focus == fieldEncrypt))
	if c.Disk.Encrypt {
		score, word := strength(sc.pass.Value())
		rows = append(rows, m.inputRow("Passphrase", &sc.pass, sc.focus == fieldPass, m.meter(score, word)))
		match := ""
		if sc.confirm.Value() != "" {
			if sc.confirm.Value() == sc.pass.Value() {
				match = s.ok.Render(m.glyphs.Done + " matches")
			} else {
				match = s.warn.Render("doesn't match")
			}
		}
		rows = append(rows, m.inputRow("Confirm", &sc.confirm, sc.focus == fieldPassConfirm, match))
		rows = append(rows, m.hint("You'll type this at every boot. It can't be recovered if lost.", width))
	}
	rows = append(rows, "")

	var swapNames []string
	for _, mode := range sc.swapChoices(m) {
		swapNames = append(swapNames, string(mode))
	}
	rows = append(rows, m.choiceRow("Swap", swapNames, slices.Index(sc.swapChoices(m), c.Swap.Mode), sc.focus == fieldSwap))
	rows = append(rows, m.hint(map[config.SwapMode]string{
		config.SwapZram:      "Compressed swap in RAM. Fast, no disk space used.",
		config.SwapFile:      "A swap file on the root filesystem.",
		config.SwapPartition: "A dedicated partition before the root partition.",
		config.SwapNone:      "No swap at all.",
	}[c.Swap.Mode], width))
	if c.Swap.Mode == config.SwapFile || c.Swap.Mode == config.SwapPartition {
		rows = append(rows, m.inputRow("Swap size", &sc.swapMiB, sc.focus == fieldSwapSize, s.dim.Render("MiB")))
	}

	rows = append(rows, "", sc.layoutBar(m, width))
	if sc.err != "" {
		rows = append(rows, "", s.warn.Render(m.glyphs.Warn+" "+sc.err))
	}
	return strings.Join(rows, "\n")
}

// layoutBar draws the partition table the install will create.
func (sc *storageScreen) layoutBar(m *Model, width int) string {
	s := m.st.styles
	c := m.st.cfg
	disk, ok := m.selectedDisk()
	if !ok {
		return ""
	}
	type seg struct {
		name  string
		bytes uint64
		style lipgloss.Style
	}
	segs := []seg{{"EFI", 1 << 30, lipgloss.NewStyle().Background(colOutline).Foreground(colText)}}
	if c.Swap.Mode == config.SwapPartition {
		n, _ := strconv.Atoi(sc.swapMiB.Value())
		segs = append(segs, seg{"swap", uint64(n) << 20, lipgloss.NewStyle().Background(colTertiary).Foreground(colSurface)})
	}
	used := uint64(0)
	for _, sg := range segs {
		used += sg.bytes
	}
	rootName := string(c.Disk.Filesystem)
	if c.Disk.Encrypt {
		rootName += ", " + m.glyphs.Lock
	}
	if disk.SizeBytes > used {
		segs = append(segs, seg{rootName, disk.SizeBytes - used, lipgloss.NewStyle().Background(colPrimary).Foreground(colSurface).Bold(true)})
	}

	barW := max(20, width-4)
	var bar, legend []string
	remaining := barW
	for i, sg := range segs {
		w := int(float64(barW) * float64(sg.bytes) / float64(disk.SizeBytes))
		w = max(w, len(sg.name)+2)
		if i == len(segs)-1 {
			w = max(remaining, 1)
		}
		remaining -= w
		label := sg.name
		if lipgloss.Width(label) > w {
			label = ""
		}
		bar = append(bar, sg.style.Render(padRight(" "+label, w)))
		legend = append(legend, s.dim.Render(sg.name+" "+humanBytes(sg.bytes)))
	}
	return s.muted.Render(strings.TrimPrefix(disk.Path, "/dev/")+" after install") + "\n" +
		strings.Join(bar, "") + "\n" + strings.Join(legend, s.dim.Render("  ·  "))
}

func (*storageScreen) help(m *Model) []binding {
	return []binding{{m.glyphs.UpDown, "field"}, {m.glyphs.LeftRight, "change"}, {"enter", "continue"}, {"esc", "back"}}
}

// ── Account ─────────────────────────────────────────────────────────────────

type accountScreen struct {
	focus                                   int
	fullName, user, pass, confirm, hostname textinput.Model
	root                                    textinput.Model
	lockRoot                                bool
	userEdited                              bool
	err                                     string
}

const (
	accFull = iota
	accUser
	accPass
	accConfirm
	accHost
	accRootMode
	accRootPass
)

func newAccountScreen() *accountScreen {
	sc := &accountScreen{
		fullName: newInput("Your name", false, 64),
		user:     newInput("lowercase login name", false, 32),
		pass:     newInput("password", true, 256),
		confirm:  newInput("repeat password", true, 256),
		hostname: newInput("voidbleed", false, 63),
		root:     newInput("root password", true, 256),
		lockRoot: true,
	}
	return sc
}

func (*accountScreen) label() string { return "Account" }
func (*accountScreen) heading() (string, string) {
	return "Your account", "You'll use sudo for administration."
}

func (sc *accountScreen) fields() []int {
	f := []int{accFull, accUser, accPass, accConfirm, accHost, accRootMode}
	if !sc.lockRoot {
		f = append(f, accRootPass)
	}
	return f
}

func (sc *accountScreen) inputs() map[int]*textinput.Model {
	return map[int]*textinput.Model{accFull: &sc.fullName, accUser: &sc.user, accPass: &sc.pass,
		accConfirm: &sc.confirm, accHost: &sc.hostname, accRootPass: &sc.root}
}

func (sc *accountScreen) enter(m *Model) tea.Cmd {
	if sc.hostname.Value() == "" {
		sc.hostname.SetValue(m.st.cfg.System.Hostname)
	}
	return sc.refocus()
}

func (sc *accountScreen) refocus() tea.Cmd {
	var cmd tea.Cmd
	for id, in := range sc.inputs() {
		if id == sc.focus {
			cmd = in.Focus()
		} else {
			in.Blur()
		}
	}
	return cmd
}

// loginFrom derives a username from a full name: "Reza H." → "reza".
func loginFrom(full string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(strings.ToLower(full)), " ")
	var b strings.Builder
	for _, r := range first {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9' && b.Len() > 0) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (sc *accountScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	k, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return sc.forward(msg), stay
	}
	fields := sc.fields()
	switch k.String() {
	case "esc":
		return nil, back
	case "tab", "down":
		sc.focus = fields[(slices.Index(fields, sc.focus)+1)%len(fields)]
		return sc.refocus(), stay
	case "shift+tab", "up":
		sc.focus = fields[(slices.Index(fields, sc.focus)-1+len(fields))%len(fields)]
		return sc.refocus(), stay
	case "left", "right", "space":
		if sc.focus == accRootMode {
			sc.lockRoot = !sc.lockRoot
			return nil, stay
		}
	case "enter":
		if sc.focus != accRootPass && sc.focus != accRootMode && slices.Index(fields, sc.focus) < slices.Index(fields, accHost) {
			sc.focus = fields[slices.Index(fields, sc.focus)+1]
			return sc.refocus(), stay
		}
		if sc.err = sc.apply(m); sc.err == "" {
			return nil, next
		}
		return nil, stay
	}
	sc.err = ""
	cmd := sc.forward(msg)
	if sc.focus == accUser {
		sc.userEdited = sc.user.Value() != ""
	}
	if sc.focus == accFull && !sc.userEdited {
		sc.user.SetValue(loginFrom(sc.fullName.Value()))
	}
	return cmd, stay
}

func (sc *accountScreen) forward(msg tea.Msg) tea.Cmd {
	if in, ok := sc.inputs()[sc.focus]; ok {
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		return cmd
	}
	return nil
}

func (sc *accountScreen) apply(m *Model) string {
	c := &m.st.cfg
	candidate := *c
	candidate.User = config.User{
		Name:     sc.user.Value(),
		FullName: strings.TrimSpace(sc.fullName.Value()),
		Password: sc.pass.Value(),
	}
	candidate.System.Hostname = sc.hostname.Value()
	switch {
	case sc.user.Value() == "":
		return "Choose a username."
	case sc.pass.Value() == "":
		return "Choose a password."
	case sc.pass.Value() != sc.confirm.Value():
		return "The passwords don't match."
	case !sc.lockRoot && sc.root.Value() == "":
		return "Set a root password, or keep root locked."
	}
	if !sc.lockRoot {
		candidate.User.RootPassword = sc.root.Value()
	}
	candidate.Disk.Device = "/dev/placeholder" // validated on its own screen
	if err := candidate.Validate(); err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			if strings.Contains(line, "hostname") || strings.Contains(line, "username") ||
				strings.Contains(line, "full name") || strings.Contains(line, "password") {
				return strings.ToUpper(line[:1]) + line[1:] + "."
			}
		}
	}
	c.User = candidate.User
	c.System.Hostname = candidate.System.Hostname
	return ""
}

func (sc *accountScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	rows := []string{
		m.inputRow("Full name", &sc.fullName, sc.focus == accFull, ""),
		m.inputRow("Username", &sc.user, sc.focus == accUser, ""),
	}
	score, word := strength(sc.pass.Value())
	rows = append(rows, m.inputRow("Password", &sc.pass, sc.focus == accPass, m.meter(score, word)))
	match := ""
	if sc.confirm.Value() != "" {
		if sc.confirm.Value() == sc.pass.Value() {
			match = s.ok.Render(m.glyphs.Done + " matches")
		} else {
			match = s.warn.Render("doesn't match")
		}
	}
	rows = append(rows, m.inputRow("Confirm", &sc.confirm, sc.focus == accConfirm, match), "")
	rows = append(rows, m.inputRow("Computer name", &sc.hostname, sc.focus == accHost, s.dim.Render("shown on the network")), "")
	rootIdx := 0
	if !sc.lockRoot {
		rootIdx = 1
	}
	rows = append(rows, m.choiceRow("Root account", []string{"locked, use sudo", "set a password"}, rootIdx, sc.focus == accRootMode))
	if !sc.lockRoot {
		rows = append(rows, m.inputRow("Root password", &sc.root, sc.focus == accRootPass, ""))
	}
	rows = append(rows, "", s.dim.Render("Your login shell is fish. Your account can use sudo."))
	if sc.err != "" {
		rows = append(rows, "", s.warn.Render(m.glyphs.Warn+" "+sc.err))
	}
	return strings.Join(rows, "\n")
}

func (*accountScreen) help(m *Model) []binding {
	return []binding{{m.glyphs.UpDown, "field"}, {"enter", "continue"}, {"esc", "back"}}
}

// ── Software ────────────────────────────────────────────────────────────────

type softwareScreen struct {
	cursor, offset int
}

func (*softwareScreen) label() string { return "Software" }
func (*softwareScreen) heading() (string, string) {
	return "Drivers and software", "Detected hardware is pre-selected. Everything here can be changed later with xbps or Flatpak."
}
func (*softwareScreen) enter(*Model) tea.Cmd { return nil }

// needsInternet reports why a group can't be installed from the live image alone.
func (m *Model) needsInternet(id string) string {
	g, ok := m.st.cat.Group(id)
	if !ok {
		return ""
	}
	if len(g.Flatpaks) > 0 {
		return "downloads from Flathub"
	}
	for _, p := range g.Packages {
		if !m.st.livePackages[p] {
			return "needs internet"
		}
	}
	return ""
}

func (sc *softwareScreen) toggle(m *Model, id string) {
	st := m.st
	g, _ := st.cat.Group(id)
	if st.selected[id] {
		delete(st.selected, id)
		// Unselect groups that required this one.
		for _, other := range st.cat.Groups {
			if slices.Contains(other.Requires, id) {
				delete(st.selected, other.ID)
			}
		}
		return
	}
	if g.Exclusive != "" {
		for _, other := range st.cat.Groups {
			if other.Exclusive == g.Exclusive {
				delete(st.selected, other.ID)
			}
		}
	}
	st.selected[id] = true
	for _, r := range g.Requires {
		st.selected[r] = true
	}
}

func (sc *softwareScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, stay
	}
	groups := m.st.cat.Groups
	switch k.String() {
	case "esc":
		return nil, back
	case "up":
		sc.cursor = max(0, sc.cursor-1)
	case "down":
		sc.cursor = min(len(groups)-1, sc.cursor+1)
	case "space":
		sc.toggle(m, groups[sc.cursor].ID)
	case "enter":
		var ids []string
		for _, g := range groups {
			if m.st.selected[g.ID] {
				ids = append(ids, g.ID)
			}
		}
		resolved, err := m.st.cat.Resolve(ids)
		if err != nil {
			return nil, stay
		}
		if resolved == nil {
			resolved = []string{}
		}
		m.st.cfg.Install.Groups = resolved
		return nil, next
	}
	return nil, stay
}

func (sc *softwareScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	var lines []string
	cursorLine := 0
	lastCategory := ""
	for i, g := range m.st.cat.Groups {
		if g.Category != lastCategory {
			if lastCategory != "" {
				lines = append(lines, "")
			}
			lines = append(lines, s.section.Render(strings.ToUpper(g.Category)))
			lastCategory = g.Category
		}
		box := s.dim.Render(m.glyphs.Uncheck)
		if g.Exclusive != "" {
			box = s.dim.Render(m.glyphs.Unradio)
		}
		if m.st.selected[g.ID] {
			box = s.accent.Render(m.glyphs.Check)
			if g.Exclusive != "" {
				box = s.accent.Render(m.glyphs.Radio)
			}
		}
		var badges []string
		if m.st.detected[g.ID] {
			badges = append(badges, s.badge.Render("detected"))
		}
		if why := m.needsInternet(g.ID); why != "" && m.st.selected[g.ID] {
			badges = append(badges, s.badgeWarn.Render(why))
		}
		badgeText := strings.Join(badges, " ")
		nameW := min(34, width*2/5)
		descW := max(8, width-nameW-6-lipgloss.Width(badgeText))
		cursor := "  "
		nameStyle := s.text
		if i == sc.cursor {
			cursor = s.accent.Render(m.glyphs.Cursor) + " "
			nameStyle = s.text.Bold(true).Foreground(colPrimary)
			cursorLine = len(lines)
		}
		name := padRight(ansi_truncate(g.Name, nameW-1, m.glyphs.Ellipsis), nameW)
		desc := padRight(ansi_truncate(g.Description, descW-1, m.glyphs.Ellipsis), descW)
		line := cursor + box + " " + nameStyle.Render(name) + s.dim.Render(desc) + badgeText
		lines = append(lines, line)
	}

	rows := max(3, height-2)
	if cursorLine < sc.offset {
		sc.offset = max(0, cursorLine-1)
	}
	if cursorLine >= sc.offset+rows {
		sc.offset = cursorLine - rows + 1
	}
	end := min(len(lines), sc.offset+rows)
	out := strings.Join(lines[sc.offset:end], "\n")

	selected := 0
	for _, g := range m.st.cat.Groups {
		if m.st.selected[g.ID] {
			selected++
		}
	}
	more := ""
	switch {
	case sc.offset > 0 && end < len(lines):
		more = "  ·  more above and below"
	case sc.offset > 0:
		more = "  ·  more above"
	case end < len(lines):
		more = "  ·  more below"
	}
	return out + "\n\n" + s.muted.Render(fmt.Sprintf("%d selected", selected)) + s.dim.Render(more)
}

func (*softwareScreen) help(m *Model) []binding {
	return []binding{{m.glyphs.UpDown, "move"}, {"space", "toggle"}, {"enter", "continue"}, {"esc", "back"}}
}

// ── Source ──────────────────────────────────────────────────────────────────

type sourceScreen struct{}

func (*sourceScreen) label() string { return "Source" }
func (*sourceScreen) heading() (string, string) {
	return "Install source", "Where the packages for the new system come from."
}
func (*sourceScreen) enter(*Model) tea.Cmd { return nil }

func (sc *sourceScreen) update(m *Model, msg tea.Msg) (tea.Cmd, move) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, stay
	}
	c := &m.st.cfg.Install
	switch k.String() {
	case "esc":
		return nil, back
	case "up", "down", "left", "right", "space", "tab":
		if c.Source == config.SourceOffline {
			c.Source = config.SourceNetwork
		} else {
			c.Source = config.SourceOffline
		}
	case "enter":
		if c.Source == config.SourceNetwork && (m.st.online == nil || !*m.st.online) {
			return nil, stay
		}
		return nil, next
	}
	return nil, stay
}

func (sc *sourceScreen) view(m *Model, width, height int) string {
	s := m.st.styles
	c := m.st.cfg.Install
	card := func(active bool, title, body string) string {
		style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colOutline).
			Padding(0, 2).Width(min(width, 76))
		mark := s.dim.Render(m.glyphs.Unradio)
		if active {
			style = style.BorderForeground(colPrimary)
			mark = s.accent.Render(m.glyphs.Radio)
		}
		return style.Render(mark + " " + s.text.Bold(true).Render(title) + "\n" + s.muted.Render(body))
	}

	var needNet []string
	for _, id := range m.st.cfg.Install.Groups {
		if why := m.needsInternet(id); why != "" {
			g, _ := m.st.cat.Group(id)
			needNet = append(needNet, g.Name)
		}
	}

	offlineBody := "Copies this live system to the disk. Fast, and works without internet."
	if len(needNet) > 0 {
		offlineBody += "\nInternet is still used for: " + strings.Join(needNet, ", ") + "."
	}
	parts := []string{
		card(c.Source == config.SourceOffline, "Offline copy (recommended)", offlineBody),
		card(c.Source == config.SourceNetwork, "Network install", "Downloads the newest packages from "+strings.TrimPrefix(c.Mirror, "https://")+".\nTakes longer; needs a working connection the whole time."),
	}

	status := m.spinner() + " " + s.muted.Render("Checking the connection"+m.glyphs.Ellipsis)
	if o := m.st.online; o != nil {
		if *o {
			status = s.ok.Render(m.glyphs.Done + " Online")
		} else {
			status = s.warn.Render(m.glyphs.Warn + " Offline")
			if c.Source == config.SourceNetwork {
				status += s.warn.Render(": connect to a network first (Noctalia's network menu, or nmtui)")
			}
			if len(needNet) > 0 && c.Source == config.SourceOffline {
				status += s.warn.Render(": deselect the software that needs internet, or connect")
			}
		}
	}
	return strings.Join(parts, "\n") + "\n\n" + status
}

func (*sourceScreen) help(m *Model) []binding {
	return []binding{{m.glyphs.UpDown, "switch"}, {"enter", "continue"}, {"esc", "back"}}
}
