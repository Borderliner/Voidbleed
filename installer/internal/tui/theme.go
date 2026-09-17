package tui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Voidbleed palette (docs/BRANDING.md). Lip Gloss downsamples for the Linux
// console's 16 colours.
var (
	colPrimary   = lipgloss.Color("#e8313f")
	colSecondary = lipgloss.Color("#ff8a85")
	colTertiary  = lipgloss.Color("#f0a35e")
	colWarn      = lipgloss.Color("#ffc247")
	colSurface   = lipgloss.Color("#0e090a")
	colSurface2  = lipgloss.Color("#211416")
	colSelect    = lipgloss.Color("#3a1519")
	colText      = lipgloss.Color("#f3e7e8")
	colMuted     = lipgloss.Color("#c9b1b3")
	colDim       = lipgloss.Color("#8a6f73")
	colOutline   = lipgloss.Color("#5e3a3f")
	colOK        = lipgloss.Color("#8fc07f")
)

type glyphSet struct {
	Current, Done, Todo, Bullet     string
	Check, Uncheck, Radio, Unradio  string
	Cursor, Arrow, Lock, Warn, Fail string
	Bar, BarEmpty, Rule, Ellipsis   string
	UpDown, LeftRight, Sep          string
	Spinner                         []string
	ASCII                           bool
}

var unicodeGlyphs = glyphSet{
	Current: "●", Done: "✓", Todo: "○", Bullet: "•",
	Check: "■", Uncheck: "□", Radio: "◉", Unradio: "○",
	Cursor: "▌", Arrow: "›", Lock: "encrypted", Warn: "▲", Fail: "✗",
	Bar: "━", BarEmpty: "━", Rule: "─", Ellipsis: "…",
	UpDown: "↑↓", LeftRight: "←→", Sep: "•",
	Spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
}

// The Linux framebuffer console font lacks most symbols.
var asciiGlyphs = glyphSet{
	Current: ">", Done: "*", Todo: "-", Bullet: "-",
	Check: "[x]", Uncheck: "[ ]", Radio: "(*)", Unradio: "( )",
	Cursor: ">", Arrow: ">", Lock: "encrypted", Warn: "!", Fail: "x",
	Bar: "#", BarEmpty: ".", Rule: "-", Ellipsis: "...",
	UpDown: "up/down", LeftRight: "left/right", Sep: "|",
	Spinner: []string{"|", "/", "-", "\\"},
	ASCII:   true,
}

func detectGlyphs() glyphSet {
	if os.Getenv("TERM") == "linux" {
		return asciiGlyphs
	}
	return unicodeGlyphs
}

// labelWidth is the form label column; hints align under the values.
const labelWidth = 16

type styles struct {
	app, card, title, subtitle, text, muted, dim, accent, secondary, warn, ok, fail lipgloss.Style
	key, keyDesc, selected, badge, badgeWarn, input, inputFocus, label, labelFocus  lipgloss.Style
	danger, section                                                                 lipgloss.Style
}

func newStyles(g glyphSet) styles {
	base := lipgloss.NewStyle().Foreground(colText)
	// The console font has square box corners but no rounded or thick ones.
	cardBorder, dangerBorder := lipgloss.RoundedBorder(), lipgloss.ThickBorder()
	if g.ASCII {
		cardBorder, dangerBorder = lipgloss.NormalBorder(), lipgloss.NormalBorder()
	}
	return styles{
		app:        lipgloss.NewStyle().Background(colSurface).Foreground(colText),
		card:       lipgloss.NewStyle().Border(cardBorder).BorderForeground(colOutline).Padding(0, 2),
		title:      base.Bold(true).Foreground(colPrimary),
		subtitle:   base.Foreground(colMuted),
		text:       base,
		muted:      base.Foreground(colMuted),
		dim:        base.Foreground(colDim),
		accent:     base.Foreground(colPrimary).Bold(true),
		secondary:  base.Foreground(colSecondary),
		warn:       base.Foreground(colWarn),
		ok:         base.Foreground(colOK),
		fail:       base.Foreground(colPrimary).Bold(true),
		key:        base.Foreground(colSecondary).Bold(true),
		keyDesc:    base.Foreground(colDim),
		selected:   base.Background(colSelect).Foreground(colText).Bold(true),
		badge:      base.Foreground(colSurface).Background(colSecondary).Padding(0, 1),
		badgeWarn:  base.Foreground(colSurface).Background(colWarn).Padding(0, 1),
		input:      base.Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(colOutline),
		inputFocus: base.Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(colPrimary),
		label:      base.Foreground(colMuted).Width(labelWidth),
		labelFocus: base.Foreground(colPrimary).Bold(true).Width(labelWidth),
		danger:     base.Border(dangerBorder).BorderForeground(colPrimary).Padding(0, 2),
		section:    base.Foreground(colTertiary).Bold(true),
	}
}

// logo is voidbleed-logo.png rendered as half blocks (26×13).
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

func (m *Model) logo() string {
	if m.glyphs.ASCII {
		return m.st.styles.accent.Render(strings.TrimPrefix(asciiLogo, "\n"))
	}
	return m.st.styles.accent.Render(strings.TrimPrefix(logo, "\n"))
}

func (m *Model) wordmark() string {
	if m.glyphs.ASCII {
		return m.st.styles.accent.Render("V O I D B L E E D")
	}
	return m.st.styles.accent.Render(wordmark)
}
