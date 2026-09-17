# Phase 5 — installer interface

`installer/internal/tui`, built with Bubble Tea v2 and Lip Gloss v2.

Screens: Welcome, Keyboard, Language, Time zone, Disk, Storage, Account,
Software, Source, Review, Install. A sidebar shows progress through them;
`enter` moves on, `esc` goes back, `ctrl+c` asks before quitting and is
disabled while installing.

## Design

- Voidbleed palette from `docs/BRANDING.md`, with the logo rendered as
  half-block art and a block wordmark.
- The Linux console can't show rounded borders, block art or arrows, so
  `TERM=linux` switches to ASCII glyphs, square borders and word key hints.
  The layout works from 80×24 up, dropping the sidebar when space is tight.
- Storage draws the partition table as it will be created, and updates as the
  filesystem, encryption and swap choices change.
- Passwords and passphrases show a strength meter and a match indicator.
- Software groups come from the catalog with "detected" badges from the
  hardware rules, and "needs internet" when the live image lacks a package.
  Exclusive groups behave like radio buttons; requirements are pulled in.

## Safety

- Review shows the full summary and, on `ctrl+d`, every command the install
  will run — produced by the dry-run engine, not a separate description.
- Installing starts only after the disk's name is typed by hand.

## Demo mode

`voidbleed-installer --demo` runs the whole flow against fake hardware with the
dry-run engine: no root, nothing changed. It's how the interface is developed
and reviewed, including on a 80×24 console.

## Tests

`go test ./internal/tui` drives the screens with synthesised key presses: the
full walk-through asserts the collected config is valid, that the live USB
stick can't be chosen, that mismatched passphrases block progress, and that
installing needs the typed disk name.
