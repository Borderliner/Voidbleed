package system

import (
	"context"
	"sort"
	"strings"

	"voidbleed/internal/sys"
)

// Package is one xbps package, installed or not.
type Package struct {
	Name        string
	Version     string
	NewVersion  string // set when an update is waiting
	Description string
	Installed   bool
	Manual      bool // asked for by a person, rather than pulled in as a dependency
	Orphan      bool // nothing depends on it any more
}

// Installed lists what is on the machine, marking what was asked for by hand
// and what nothing needs any more.
func (c *Client) Installed(ctx context.Context) ([]Package, error) {
	out, err := c.output(ctx, "xbps-query", "-l")
	if err != nil {
		return nil, err
	}
	manual := c.pkgnameSet(ctx, "-m")
	orphans := c.pkgnameSet(ctx, "-O")

	var pkgs []Package
	for _, line := range lines(out) {
		// "ii firefox-142.0_1   Web browser"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, version := splitPkgver(fields[1])
		pkgs = append(pkgs, Package{
			Name:        name,
			Version:     version,
			Description: strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, fields[0]), " "+fields[1])),
			Installed:   true,
			Manual:      manual[name],
			Orphan:      orphans[name],
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs, nil
}

// pkgnameSet reads one of the plain "pkgver per line" queries.
func (c *Client) pkgnameSet(ctx context.Context, flag string) map[string]bool {
	set := map[string]bool{}
	out, err := c.output(ctx, "xbps-query", flag)
	if err != nil {
		return set
	}
	for _, line := range lines(out) {
		name, _ := splitPkgver(strings.Fields(line)[0])
		set[name] = true
	}
	return set
}

// Updates lists the packages a system update would replace. -M keeps the
// freshly fetched repository data in memory instead of writing it under
// /var/db/xbps, which is what lets an ordinary user ask the question at all.
func (c *Client) Updates(ctx context.Context) ([]Package, error) {
	out, err := c.output(ctx, "xbps-install", "-Mun")
	if err != nil {
		// xbps exits non-zero when it has nothing to do in some versions;
		// an empty list is the honest answer either way.
		if strings.TrimSpace(out) == "" {
			return nil, nil
		}
	}
	var pkgs []Package
	for _, line := range lines(out) {
		// "intel-media-driver-26.3.5_1 update x86_64 https://…  20874590 4984173"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, version := splitPkgver(fields[0])
		pkgs = append(pkgs, Package{Name: name, NewVersion: version, Installed: fields[1] == "update"})
	}
	return pkgs, nil
}

// Search asks the repositories; [*] marks what is already installed.
func (c *Client) Search(ctx context.Context, term string) ([]Package, error) {
	out, err := c.output(ctx, "xbps-query", "-Rs", term)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, nil // xbps exits 2 when a search finds nothing
	}
	var pkgs []Package
	for _, line := range lines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, version := splitPkgver(fields[1])
		pkgs = append(pkgs, Package{
			Name:        name,
			Version:     version,
			Description: strings.TrimSpace(strings.TrimPrefix(line, fields[0]+" "+fields[1])),
			Installed:   fields[0] == "[*]",
		})
	}
	return pkgs, nil
}

// Show returns xbps-query's own description of a package.
func (c *Client) Show(ctx context.Context, name string) (string, error) {
	out, err := c.output(ctx, "xbps-query", "-R", name)
	if err != nil {
		return "", err
	}
	return out, nil
}

// The commands that change something. They are returned rather than run so
// the interface can show them, confirm them, and stream their output.

func InstallCmd(names ...string) sys.Cmd {
	return sys.Command("xbps-install", append([]string{"-Sy"}, names...)...)
}

func RemoveCmd(name string, withDependencies bool) sys.Cmd {
	args := []string{"-y"}
	if withDependencies {
		args = append(args, "-R") // also remove dependencies nothing else needs
	}
	return sys.Command("xbps-remove", append(args, name)...)
}

// MarkManualCmd says a package was wanted for its own sake. xbps calls a
// package orphaned when it was pulled in as a dependency and nothing needs it
// any more; this clears that flag, which is what "keep this" means.
func MarkManualCmd(name string) sys.Cmd {
	return sys.Command("xbps-pkgdb", "-m", "manual", name)
}

// MarkAutoCmd is the other direction: treat it as a dependency again, so it
// goes when the last thing needing it goes.
func MarkAutoCmd(name string) sys.Cmd {
	return sys.Command("xbps-pkgdb", "-m", "auto", name)
}

func UpdateCmd() sys.Cmd  { return sys.Command("xbps-install", "-Suy") }
func SyncCmd() sys.Cmd    { return sys.Command("xbps-install", "-S") }
func CleanUpCmd() sys.Cmd { return sys.Command("xbps-remove", "-Ooy") }
