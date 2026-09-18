package control

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi/kitty"
)

// The real logo, drawn by the terminal itself where that is possible.
//
// Ghostty, Kitty and WezTerm speak Kitty's graphics protocol: a picture is
// handed over once, and then drawn wherever certain characters appear. Those
// characters are ordinary text in the frame, which is the whole point of
// doing it this way. Placing a picture at a screen position instead means
// moving the terminal's cursor behind the renderer's back and putting it
// back -- and a save-and-restore does not carry the pending-wrap state at the
// last column with it, so one character of the frame lands a cell out and
// stays there. That was the stray mark beside the frame.
//
// Everywhere else -- a plain console, tmux, ssh to something older -- the
// block art stands in, which is why the art is still there.
//
//go:embed logo.png
var logoPNG []byte

// One id for the one picture this program has. The cells carry it too: it is
// encoded in their colour.
const logoImageID = 7311

// graphicsQuery asks the terminal whether it understands any of this, by
// sending it a single transparent pixel and waiting to be told "OK". Guessing
// from $TERM gets it wrong in both directions -- a terminal that cannot draw
// pictures would leave a hole in the page where one was reserved.
const graphicsQuery = "\x1b_Gi=7311,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"

// graphicsAllowed reports whether to ask at all. tmux and screen swallow these
// sequences unless told otherwise, and getting that wrong prints line noise.
func graphicsAllowed() bool {
	switch os.Getenv("VOIDBLEED_GRAPHICS") {
	case "0", "off", "no":
		return false
	case "1", "on", "yes":
		return true
	}
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return false
	}
	term := os.Getenv("TERM")
	return !strings.HasPrefix(term, "screen") && !strings.HasPrefix(term, "tmux") && term != "linux"
}

// transmitLogo hands the picture over and says how big it should be in cells.
//
// U=1 makes it a virtual placement: the terminal keeps the picture and draws
// nothing until it meets the cells below. Nothing in here moves the cursor.
func transmitLogo(cols, rows int) string {
	payload := base64.StdEncoding.EncodeToString(logoPNG)
	var b strings.Builder
	first := true
	for len(payload) > 0 {
		n := min(kitty.MaxChunkSize, len(payload))
		chunk := payload[:n]
		payload = payload[n:]
		more := 0
		if len(payload) > 0 {
			more = 1
		}
		if first {
			// f=100: a PNG. t=d: the bytes are in this escape. q=2: say
			// nothing back, or the reply arrives as keyboard input.
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,t=d,i=%d,U=1,c=%d,r=%d,q=2,m=%d;%s\x1b\\",
				logoImageID, cols, rows, more, chunk)
			first = false
			continue
		}
		fmt.Fprintf(&b, "\x1b_Gm=%d,q=2;%s\x1b\\", more, chunk)
	}
	return b.String()
}

// logoBox is the picture, written as characters. Each cell holds the
// placeholder rune and two combining marks naming its row and column in the
// picture, and the image id rides along in the foreground colour. To anything
// measuring text this is a block of ordinary characters, cols wide and rows
// tall -- so the layout around it does not care whether a picture appears.
func logoBox(cols, rows int) string {
	// The id is carried as a colour: red, green and blue are its three bytes.
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		(logoImageID>>16)&0xff, (logoImageID>>8)&0xff, logoImageID&0xff)))

	lines := make([]string, rows)
	for r := range lines {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		lines[r] = style.Render(b.String())
	}
	return strings.Join(lines, "\n")
}
