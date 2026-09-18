// Command voidbleed-control is the Voidbleed control centre: packages,
// Flatpaks, services, firmware, appearance and the firewall, in one terminal
// interface that looks like the installer it follows.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"voidbleed/internal/control"
)

func main() {
	demo := flag.Bool("demo", false, "answer everything from canned data; change nothing")
	noGraphics := flag.Bool("no-graphics", false, "draw the logo as block art, never as a picture")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `voidbleed-control — the Voidbleed control centre

  voidbleed-control          manage this machine
  voidbleed-control --demo   try the interface against canned data

Run it as yourself: anything that changes the machine asks for the
administrator password when it needs one.

In Ghostty, Kitty or WezTerm the logo is drawn as a picture; set
VOIDBLEED_GRAPHICS=0 or pass --no-graphics for the block art instead.

`)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *version {
		fmt.Println("voidbleed-control 1")
		return
	}

	program := tea.NewProgram(control.New(control.Options{Demo: *demo, NoGraphics: *noGraphics}))
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "voidbleed-control:", err)
		os.Exit(1)
	}
}
