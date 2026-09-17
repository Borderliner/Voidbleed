package tui

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// option is one entry in a picker.
type option struct {
	Value, Label, Detail string
}

// Data files on a Void system. Tests and demo mode fall back to small lists.
var (
	xkbRulesFile    = "/usr/share/X11/xkb/rules/evdev.lst"
	libcLocalesFile = "/etc/default/libc-locales"
	zoneinfoDir     = "/usr/share/zoneinfo"
	kbdKeymapsDir   = "/usr/share/kbd/keymaps"
)

// keyboardLayouts reads the "! layout" section of evdev.lst.
func keyboardLayouts() []option {
	f, err := os.Open(xkbRulesFile)
	if err != nil {
		return fallbackLayouts
	}
	defer f.Close()
	var opts []option
	inLayouts := false
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "!") {
			inLayouts = strings.TrimSpace(line) == "! layout"
			continue
		}
		if !inLayouts {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		opts = append(opts, option{Value: fields[0], Label: strings.Join(fields[1:], " "), Detail: fields[0]})
	}
	if len(opts) == 0 {
		return fallbackLayouts
	}
	sort.SliceStable(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
	return opts
}

var fallbackLayouts = []option{
	{"us", "English (US)", "us"}, {"gb", "English (UK)", "gb"}, {"de", "German", "de"},
	{"fr", "French", "fr"}, {"es", "Spanish", "es"}, {"it", "Italian", "it"},
	{"ir", "Persian", "ir"}, {"ru", "Russian", "ru"}, {"ara", "Arabic", "ara"},
	{"tr", "Turkish", "tr"}, {"jp", "Japanese", "jp"}, {"br", "Portuguese (Brazil)", "br"},
}

// locales lists UTF-8 locales from libc-locales (commented or not).
func locales() []option {
	f, err := os.Open(libcLocalesFile)
	if err != nil {
		return fallbackLocales
	}
	defer f.Close()
	seen := map[string]bool{}
	var opts []option
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(strings.TrimLeft(s.Text(), "#"))
		if len(fields) != 2 || fields[1] != "UTF-8" || seen[fields[0]] {
			continue
		}
		name := fields[0]
		if !strings.Contains(name, ".UTF-8") {
			continue
		}
		seen[name] = true
		opts = append(opts, option{Value: name, Label: name})
	}
	if len(opts) == 0 {
		return fallbackLocales
	}
	return opts
}

var fallbackLocales = []option{
	{"en_US.UTF-8", "en_US.UTF-8", ""}, {"en_GB.UTF-8", "en_GB.UTF-8", ""}, {"de_DE.UTF-8", "de_DE.UTF-8", ""},
	{"fa_IR.UTF-8", "fa_IR.UTF-8", ""}, {"fr_FR.UTF-8", "fr_FR.UTF-8", ""}, {"ja_JP.UTF-8", "ja_JP.UTF-8", ""},
}

// timezones lists Region/City zones from the zoneinfo tree.
func timezones() []option {
	var opts []option
	regions := map[string]bool{"Africa": true, "America": true, "Antarctica": true, "Arctic": true,
		"Asia": true, "Atlantic": true, "Australia": true, "Europe": true, "Indian": true, "Pacific": true}
	filepath.WalkDir(zoneinfoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(zoneinfoDir, path)
		region, _, _ := strings.Cut(rel, "/")
		if regions[region] && strings.Contains(rel, "/") {
			opts = append(opts, option{Value: rel, Label: strings.ReplaceAll(rel, "_", " ")})
		}
		return nil
	})
	if len(opts) == 0 {
		return fallbackZones
	}
	sort.Slice(opts, func(i, j int) bool { return opts[i].Value < opts[j].Value })
	return append([]option{{Value: "UTC", Label: "UTC"}}, opts...)
}

var fallbackZones = []option{
	{"UTC", "UTC", ""}, {"America/New_York", "America/New York", ""}, {"Asia/Tehran", "Asia/Tehran", ""},
	{"Asia/Tokyo", "Asia/Tokyo", ""}, {"Europe/Berlin", "Europe/Berlin", ""}, {"Europe/London", "Europe/London", ""},
}

// currentTimezone reads the live system's /etc/localtime link.
func currentTimezone() string {
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return "UTC"
	}
	if _, zone, ok := strings.Cut(target, "zoneinfo/"); ok {
		return zone
	}
	return "UTC"
}

// consoleKeymap picks a kbd keymap for an xkb layout, falling back to us.
func consoleKeymap(layout string) string {
	matches, _ := filepath.Glob(filepath.Join(kbdKeymapsDir, "i386", "*", layout+".map.gz"))
	if len(matches) > 0 {
		return layout
	}
	return "us"
}
