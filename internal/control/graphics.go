package control

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The real logo, drawn by the terminal itself where that is possible.
//
// Ghostty, Kitty and WezTerm speak Kitty's graphics protocol: a picture is
// handed over once and then placed into a box of character cells. Everywhere
// else -- a plain console, tmux, ssh to something older -- the block art
// stands in for it, which is why the art is still there.
//
//go:embed logo.png
var logoPNG []byte

// One id for the one picture this program has.
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

// transmitLogo hands the picture over. It is sent once: placing it afterwards
// costs a few dozen bytes, while sending it again would cost seventy kilobytes
// every time.
func transmitLogo() string {
	payload := base64.StdEncoding.EncodeToString(logoPNG)
	var b strings.Builder
	first := true
	for len(payload) > 0 {
		// The protocol takes the picture in chunks of at most 4096 bytes.
		n := min(4096, len(payload))
		chunk := payload[:n]
		payload = payload[n:]
		more := 0
		if len(payload) > 0 {
			more = 1
		}
		if first {
			// f=100: a PNG. t=d: the bytes are in this escape. q=2: say
			// nothing back, or the reply arrives as keyboard input.
			fmt.Fprintf(&b, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\", logoImageID, more, chunk)
			first = false
			continue
		}
		fmt.Fprintf(&b, "\x1b_Gm=%d,q=2;%s\x1b\\", more, chunk)
	}
	return b.String()
}

// placeLogoAt draws the picture into a box of cells at an absolute position on
// the screen.
//
// This is written outside the frame Bubble Tea paints, which means moving the
// terminal's cursor behind the renderer's back: it is saved, walked to the
// box, and put back, and the cursor is hidden again afterwards in case the
// restore brought it back. z=-1 puts the picture under the text layer, so
// repainting the cells it sits in no longer rubs it out -- which is what let
// this be done once per position instead of several times a second.
func placeLogoAt(row, col, cols, rows int) string {
	return fmt.Sprintf("\x1b7\x1b[%d;%dH\x1b_Ga=p,i=%d,p=1,c=%d,r=%d,z=-1,C=1,q=2\x1b\\\x1b8%s",
		row, col, logoImageID, cols, rows, ansi.HideCursor)
}

// deleteLogo takes the picture off the screen. A placement is anchored to the
// screen, not to the text under it, so leaving the overview without this would
// leave the logo hanging over whatever came next. Lower-case "i" drops the
// placements and keeps the picture, so coming back costs nothing.
func deleteLogo() string {
	return fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", logoImageID)
}

// imageMarker is written into the first cell of the reserved box so the frame
// can be searched for it. Deriving the position from the layout instead would
// mean re-deriving it every time the layout changed; this cannot drift.
const imageMarker = "\x00"

// findMarker returns the one-based row and column of the marker in a rendered
// frame, and the frame with the marker turned back into a space.
func findMarker(frame string) (row, col int, cleaned string, found bool) {
	for i, line := range strings.Split(frame, "\n") {
		at := strings.Index(line, imageMarker)
		if at < 0 {
			continue
		}
		return i + 1, ansi.StringWidth(line[:at]) + 1,
			strings.Replace(frame, imageMarker, " ", 1), true
	}
	return 0, 0, frame, false
}

// logoBox is the space the picture is drawn into: plain cells, with the marker
// in the corner. It measures exactly cols by rows, so the layout around it is
// the same whether the picture appears or not.
func logoBox(cols, rows int) string {
	lines := make([]string, rows)
	lines[0] = imageMarker + strings.Repeat(" ", cols-1)
	for i := 1; i < rows; i++ {
		lines[i] = strings.Repeat(" ", cols)
	}
	return strings.Join(lines, "\n")
}
