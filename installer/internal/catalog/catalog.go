// Package catalog reads the Voidbleed package catalog (catalog/ in the repo,
// /usr/share/voidbleed/catalog on the live system) and resolves which optional
// groups apply to a machine.
package catalog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"voidbleed/installer/internal/hw"
)

const DefaultDir = "/usr/share/voidbleed/catalog"

type Group struct {
	ID          string   `toml:"id"`
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Category    string   `toml:"category"`
	Default     string   `toml:"default"` // on | off | auto
	Detect      []string `toml:"detect"`
	Exclusive   string   `toml:"exclusive"`
	Requires    []string `toml:"requires"`
	Repos       []string `toml:"repos"`
	Packages    []string `toml:"packages"`
	Flatpaks    []string `toml:"flatpaks"`
	Services    []string `toml:"services"`
	Overlay     string   `toml:"overlay"`
	Cmdline     []string `toml:"cmdline"`
}

type Catalog struct {
	Base       []string // hard dependencies of voidbleed-base
	Desktop    []string // hard dependencies of voidbleed-desktop
	Defaults   []string // removable defaults, installed explicitly
	Services   []string // enabled on every install
	Categories []string
	Groups     []Group
}

func Load(dir string) (*Catalog, error) {
	c := &Catalog{}
	lists := map[string]*[]string{
		"base.txt":     &c.Base,
		"desktop.txt":  &c.Desktop,
		"defaults.txt": &c.Defaults,
		"services.txt": &c.Services,
	}
	for name, dst := range lists {
		items, err := readList(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		*dst = items
	}
	var opt struct {
		Categories []string `toml:"categories"`
		Groups     []Group  `toml:"group"`
	}
	if _, err := toml.DecodeFile(filepath.Join(dir, "optional.toml"), &opt); err != nil {
		return nil, fmt.Errorf("optional.toml: %w", err)
	}
	c.Categories, c.Groups = opt.Categories, opt.Groups
	return c, nil
}

// readList returns names from a one-per-line file; '#' comments and
// tab-separated notes are ignored (same rules as scripts/catalog.py).
func readList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var names []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line, _, _ := strings.Cut(s.Text(), "#")
		line, _, _ = strings.Cut(line, "\t")
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names, s.Err()
}

func (c *Catalog) Group(id string) (Group, bool) {
	for _, g := range c.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

// Matches reports whether every detect condition of g holds on facts.
func (g Group) Matches(f hw.Facts) bool {
	if len(g.Detect) == 0 {
		return false
	}
	for _, cond := range g.Detect {
		if !condition(cond, f) {
			return false
		}
	}
	return true
}

func condition(cond string, f hw.Facts) bool {
	switch {
	case cond == "bluetooth":
		return f.Bluetooth
	case cond == "chassis:laptop":
		return f.Laptop
	case cond == "cpu:intel":
		return f.CPU == hw.Intel
	case cond == "cpu:amd":
		return f.CPU == hw.AMD
	case strings.HasPrefix(cond, "gpu:nvidia-gen:"):
		wanted := strings.Split(strings.TrimPrefix(cond, "gpu:nvidia-gen:"), ",")
		for _, gen := range f.NvidiaGens() {
			for _, w := range wanted {
				if w == gen || (w == "turing+" && gen == hw.GenTuring) {
					return true
				}
			}
		}
		return false
	case strings.HasPrefix(cond, "gpu:"):
		return f.HasGPU(hw.Vendor(strings.TrimPrefix(cond, "gpu:")))
	}
	return false
}

// DefaultSelection is what the installer pre-selects for this machine.
func (c *Catalog) DefaultSelection(f hw.Facts) []string {
	var ids []string
	for _, g := range c.Groups {
		if g.Default == "on" || (g.Default == "auto" && g.Matches(f)) {
			ids = append(ids, g.ID)
		}
	}
	sel, _ := c.Resolve(ids)
	return sel
}

// Resolve adds required groups and checks exclusivity. The result keeps
// catalog order so installs are reproducible.
func (c *Catalog) Resolve(ids []string) ([]string, error) {
	want := map[string]bool{}
	var visit func(id string) error
	visit = func(id string) error {
		if want[id] {
			return nil
		}
		g, ok := c.Group(id)
		if !ok {
			return fmt.Errorf("unknown optional group %q", id)
		}
		want[id] = true
		for _, r := range g.Requires {
			if err := visit(r); err != nil {
				return err
			}
		}
		return nil
	}
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	exclusive := map[string]string{}
	var out []string
	for _, g := range c.Groups {
		if !want[g.ID] {
			continue
		}
		if g.Exclusive != "" {
			if other, taken := exclusive[g.Exclusive]; taken {
				return nil, fmt.Errorf("%s and %s can't both be selected", other, g.ID)
			}
			exclusive[g.Exclusive] = g.ID
		}
		out = append(out, g.ID)
	}
	return out, nil
}

// Selection is the union of everything a set of groups contributes.
type Selection struct {
	Groups   []string
	Repos    []string
	Packages []string
	Flatpaks []string
	Services []string
	Overlays []string
	Cmdline  []string
}

func (c *Catalog) Expand(ids []string) Selection {
	var s Selection
	add := func(dst *[]string, items ...string) {
		for _, it := range items {
			if it != "" && !slices.Contains(*dst, it) {
				*dst = append(*dst, it)
			}
		}
	}
	for _, id := range ids {
		g, ok := c.Group(id)
		if !ok {
			continue
		}
		s.Groups = append(s.Groups, id)
		add(&s.Repos, g.Repos...)
		add(&s.Packages, g.Packages...)
		add(&s.Flatpaks, g.Flatpaks...)
		add(&s.Services, g.Services...)
		add(&s.Overlays, g.Overlay)
		add(&s.Cmdline, g.Cmdline...)
	}
	return s
}

// KernelSeries is the pinned linuxX.Y package from the base list.
func (c *Catalog) KernelSeries() string {
	for _, p := range c.Base {
		if strings.HasPrefix(p, "linux") && strings.Count(p, ".") == 1 && !strings.Contains(p, "-") {
			return p
		}
	}
	return ""
}
