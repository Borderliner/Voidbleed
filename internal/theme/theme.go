// Package theme is the Voidbleed look, shared by the installer and the
// control centre so the two programs are visibly the same thing.
package theme

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// The palette from docs/BRANDING.md. Lip Gloss downsamples for the Linux
// console's 16 colours.
var (
	Primary   = lipgloss.Color("#e8313f")
	Secondary = lipgloss.Color("#ff8a85")
	Tertiary  = lipgloss.Color("#f0a35e")
	Warn      = lipgloss.Color("#ffc247")
	Surface   = lipgloss.Color("#0e090a")
	Surface2  = lipgloss.Color("#211416")
	Select    = lipgloss.Color("#3a1519")
	Text      = lipgloss.Color("#f3e7e8")
	Muted     = lipgloss.Color("#c9b1b3")
	Dim       = lipgloss.Color("#8a6f73")
	Outline   = lipgloss.Color("#5e3a3f")
	OK        = lipgloss.Color("#8fc07f")
)

// GlyphSet is the drawing alphabet: the Linux console cannot render most of
// the Unicode one, so every symbol has an ASCII twin.
type GlyphSet struct {
	Current, Done, Todo, Bullet     string
	Check, Uncheck, Radio, Unradio  string
	Cursor, Arrow, Lock, Warn, Fail string
	Bar, BarEmpty, Rule, Ellipsis   string
	UpDown, LeftRight, Sep          string
	Spinner                         []string
	ASCII                           bool
}

var Unicode = GlyphSet{
	Current: "●", Done: "✓", Todo: "○", Bullet: "•",
	Check: "■", Uncheck: "□", Radio: "◉", Unradio: "○",
	Cursor: "▌", Arrow: "›", Lock: "encrypted", Warn: "▲", Fail: "✗",
	Bar: "━", BarEmpty: "━", Rule: "─", Ellipsis: "…",
	UpDown: "↑↓", LeftRight: "←→", Sep: "•",
	Spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
}

// ASCIIOnly is for the framebuffer console font, which lacks most symbols.
var ASCIIOnly = GlyphSet{
	Current: ">", Done: "*", Todo: "-", Bullet: "-",
	Check: "[x]", Uncheck: "[ ]", Radio: "(*)", Unradio: "( )",
	Cursor: ">", Arrow: ">", Lock: "encrypted", Warn: "!", Fail: "x",
	Bar: "#", BarEmpty: ".", Rule: "-", Ellipsis: "...",
	UpDown: "up/down", LeftRight: "left/right", Sep: "|",
	Spinner: []string{"|", "/", "-", "\\"},
	ASCII:   true,
}

// Detect picks the alphabet the terminal can actually draw.
func Detect() GlyphSet {
	if os.Getenv("TERM") == "linux" {
		return ASCIIOnly
	}
	return Unicode
}

// LabelWidth is the form label column; hints align under the values.
const LabelWidth = 16

// Styles is every style either program draws with.
type Styles struct {
	App, Card, Title, Subtitle, Text, Muted, Dim                 lipgloss.Style
	Accent, Secondary, Warn, OK, Fail                            lipgloss.Style
	Key, KeyDesc, Selected, Badge, BadgeWarn                     lipgloss.Style
	Input, InputFocus, Label, LabelFocus, Danger, Section        lipgloss.Style
	SidebarItem, SidebarActive, Header, Footer, Table, TableHead lipgloss.Style
}

func NewStyles(g GlyphSet) Styles {
	base := lipgloss.NewStyle().Foreground(Text)
	// The console font has square box corners but no rounded or thick ones.
	cardBorder, dangerBorder := lipgloss.RoundedBorder(), lipgloss.ThickBorder()
	if g.ASCII {
		cardBorder, dangerBorder = lipgloss.NormalBorder(), lipgloss.NormalBorder()
	}
	return Styles{
		App:        lipgloss.NewStyle().Background(Surface).Foreground(Text),
		Card:       lipgloss.NewStyle().Border(cardBorder).BorderForeground(Outline).Padding(0, 2),
		Title:      base.Bold(true).Foreground(Primary),
		Subtitle:   base.Foreground(Muted),
		Text:       base,
		Muted:      base.Foreground(Muted),
		Dim:        base.Foreground(Dim),
		Accent:     base.Foreground(Primary).Bold(true),
		Secondary:  base.Foreground(Secondary),
		Warn:       base.Foreground(Warn),
		OK:         base.Foreground(OK),
		Fail:       base.Foreground(Primary).Bold(true),
		Key:        base.Foreground(Secondary).Bold(true),
		KeyDesc:    base.Foreground(Dim),
		Selected:   base.Background(Select).Foreground(Text).Bold(true),
		Badge:      base.Foreground(Surface).Background(Secondary).Padding(0, 1),
		BadgeWarn:  base.Foreground(Surface).Background(Warn).Padding(0, 1),
		Input:      base.Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(Outline),
		InputFocus: base.Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(Primary),
		Label:      base.Foreground(Muted).Width(LabelWidth),
		LabelFocus: base.Foreground(Primary).Bold(true).Width(LabelWidth),
		Danger:     base.Border(dangerBorder).BorderForeground(Primary).Padding(0, 2),
		Section:    base.Foreground(Tertiary).Bold(true),

		// The control centre's chrome: a sidebar of sections, a header and a
		// footer of key hints around the page.
		SidebarItem:   base.Foreground(Muted).Padding(0, 1),
		SidebarActive: base.Foreground(Text).Background(Select).Bold(true).Padding(0, 1),
		Header:        base.Foreground(Primary).Bold(true),
		Footer:        base.Foreground(Dim),
		Table:         base,
		TableHead:     base.Foreground(Dim).Bold(true),
	}
}

// Logo is voidbleed-logo.png rendered as half blocks (26×13).
const logo = `
        ▄▄████████▄    ▄
      ▄████▀▀▀▀▀███▀▄▄▀
    ▄███▀        ▄▄█▀▄▄
   ▄███       ▄▄██▀ ████
   ███      ▄██▀▀▄   ███
   ███     ██▀▄██▀   ███
   ███▄  ▄█▀  ▀▀    ▄███
   ▀██▀▄█▀         ▄███▀
     ▄▀▀▄▄▄     ▄▄████▀
   ▄▀  █████████████▀
  ▀      ▀▀▀▀██▀▀▀`

// asciiLogo is the slashed ring for fonts without block elements.
const asciiLogo = `
   .-""""-.   /
  /       .' /
 |      .'   |
 |    .'     |
  \ .'      /
  /'-.____.'
 /`

const wordmark = "█ █ █▀█ █ █▀▄ █▄▄ █   █▀▀ █▀▀ █▀▄\n▀▄▀ █▄█ █ █▄▀ █▄█ █▄▄ ██▄ ██▄ █▄▀"

// Logo returns the mark, in blocks or in ASCII.
func Logo(g GlyphSet) string {
	if g.ASCII {
		return strings.TrimPrefix(asciiLogo, "\n")
	}
	return strings.TrimPrefix(logo, "\n")
}

// Wordmark returns VOIDBLEED, in blocks or in letters.
func Wordmark(g GlyphSet) string {
	if g.ASCII {
		return "V O I D B L E E D"
	}
	return wordmark
}
