package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"voidbleed/internal/theme"
)

// The look lives in internal/theme, shared with the control centre. The
// installer's screens were written against these short names, and they read
// better in dense layout code, so they stay.
var (
	colPrimary   = theme.Primary
	colSecondary = theme.Secondary
	colTertiary  = theme.Tertiary
	colWarn      = theme.Warn
	colSurface   = theme.Surface
	colSurface2  = theme.Surface2
	colSelect    = theme.Select
	colText      = theme.Text
	colMuted     = theme.Muted
	colDim       = theme.Dim
	colOutline   = theme.Outline
	colOK        = theme.OK
)

type glyphSet = theme.GlyphSet

func detectGlyphs() glyphSet { return theme.Detect() }

const labelWidth = theme.LabelWidth

type styles struct {
	app, card, title, subtitle, text, muted, dim, accent, secondary, warn, ok, fail lipgloss.Style
	key, keyDesc, selected, badge, badgeWarn, input, inputFocus, label, labelFocus  lipgloss.Style
	danger, section                                                                 lipgloss.Style
}

func newStyles(g glyphSet) styles {
	s := theme.NewStyles(g)
	return styles{
		app: s.App, card: s.Card, title: s.Title, subtitle: s.Subtitle,
		text: s.Text, muted: s.Muted, dim: s.Dim, accent: s.Accent,
		secondary: s.Secondary, warn: s.Warn, ok: s.OK, fail: s.Fail,
		key: s.Key, keyDesc: s.KeyDesc, selected: s.Selected,
		badge: s.Badge, badgeWarn: s.BadgeWarn,
		input: s.Input, inputFocus: s.InputFocus,
		label: s.Label, labelFocus: s.LabelFocus,
		danger: s.Danger, section: s.Section,
	}
}

func (m *Model) logo() string {
	return m.st.styles.accent.Render(strings.TrimRight(theme.Logo(m.glyphs), "\n"))
}

func (m *Model) wordmark() string {
	return m.st.styles.accent.Render(theme.Wordmark(m.glyphs))
}
