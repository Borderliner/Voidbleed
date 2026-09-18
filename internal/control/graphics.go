package control

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// The real logo, drawn by the terminal itself where that is possible.
//
// Ghostty, Kitty and WezTerm all speak Kitty's graphics protocol: a picture is
// handed over once and then placed into a box of character cells. Everywhere
// else -- a plain console, a terminal over ssh, tmux -- the block art stands in
// for it, which is why the art is still there.
//
//go:embed logo.png
var logoPNG []byte

// One id for the one image this program has. Kitty's protocol namespaces
// images per terminal, so a number nothing else is likely to pick will do.
const logoImageID = 7311

// graphicsEnv reports whether the terminal draws pictures, from what it says
// about itself. Querying it properly means a handshake in the middle of a
// Bubble Tea frame; the environment is good enough, and VOIDBLEED_GRAPHICS
// settles it either way.
func graphicsEnv() bool {
	switch os.Getenv("VOIDBLEED_GRAPHICS") {
	case "0", "off", "no":
		return false
	case "1", "on", "yes":
		return true
	}
	// tmux and screen pass escape sequences through only when asked to, and
	// getting that wrong looks like line noise.
	if term := os.Getenv("TERM"); strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux") {
		return false
	}
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return false
	}
	switch os.Getenv("TERM") {
	case "xterm-ghostty", "ghostty", "xterm-kitty":
		return true
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "ghostty", "wezterm":
		return true
	}
	return os.Getenv("KITTY_WINDOW_ID") != ""
}

// transmitLogo hands the picture to the terminal. It is sent once: placing it
// afterwards costs a few dozen bytes, while sending it again would cost
// seventy kilobytes on every frame.
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
			// f=100: a PNG. t=d: the bytes are here, in this escape.
			fmt.Fprintf(&b, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\", logoImageID, more, chunk)
			first = false
			continue
		}
		fmt.Fprintf(&b, "\x1b_Gm=%d,q=2;%s\x1b\\", more, chunk)
	}
	return b.String()
}

// placeLogo draws the picture into a box of cells whose bottom-right corner is
// where the cursor is now. Repainting the box with spaces rubs the picture
// out, so this is emitted after them, on every frame: the cursor is saved,
// walked back to the top-left of the box, and put back.
func placeLogo(cols, rows int) string {
	return fmt.Sprintf("\x1b7\x1b[%dA\x1b[%dD\x1b_Ga=p,i=%d,p=1,c=%d,r=%d,C=1,q=2\x1b\\\x1b8",
		rows-1, cols, logoImageID, cols, rows)
}

// logoBox is the picture as lines of spaces with the escapes attached, ready
// to be laid out like any other block of text: the escapes have no width, so
// the box measures exactly cols by rows.
func logoBox(cols, rows int, transmit bool) string {
	blank := strings.Repeat(" ", cols)
	lines := make([]string, rows)
	for i := range lines {
		lines[i] = blank
	}
	out := strings.Join(lines, "\n") + placeLogo(cols, rows)
	if transmit {
		return transmitLogo() + out
	}
	return out
}

// deleteLogo takes the picture off the screen. A placement is anchored to the
// screen, not to the text under it, so leaving the overview without this would
// leave the logo floating over whatever came next. Lower-case "i" removes the
// placements and keeps the picture itself, so coming back costs nothing.
func deleteLogo() string {
	return fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", logoImageID)
}
