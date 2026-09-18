package system

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"voidbleed/internal/sys"
)

// Appearance is the look every toolkit should agree on.
type Appearance struct {
	GTKTheme    string
	IconTheme   string
	CursorTheme string
	CursorSize  int
	FontName    string // "Ubuntu 10"
	ColorScheme string // default, prefer-dark, prefer-light
	QtStyle     string // qt5ct/qt6ct widget style
}

// Themes is what this machine has to choose from.
type Themes struct {
	GTK     []string
	Icons   []string
	Cursors []string
	QtStyle []string
}

// gsettings is authoritative for anything that reads the desktop portal,
// which is most GTK4 and libadwaita applications; settings.ini covers the
// rest. Both are written, and they are kept in step.
const gsettingsSchema = "org.gnome.desktop.interface"

// Appearance reads the current look, preferring gsettings and falling back to
// the GTK3 file on a machine without dconf.
func (c *Client) Appearance(ctx context.Context) Appearance {
	a := Appearance{CursorSize: 24}
	get := func(key string) string {
		out, err := c.output(ctx, "gsettings", "get", gsettingsSchema, key)
		if err != nil {
			return ""
		}
		return strings.Trim(strings.TrimSpace(out), "'")
	}
	a.GTKTheme = get("gtk-theme")
	a.IconTheme = get("icon-theme")
	a.CursorTheme = get("cursor-theme")
	a.FontName = get("font-name")
	a.ColorScheme = get("color-scheme")
	if n, err := strconv.Atoi(get("cursor-size")); err == nil && n > 0 {
		a.CursorSize = n
	}

	ini := readINI(filepath.Join(configHome(), "gtk-3.0/settings.ini"))
	fallback := func(have *string, key string) {
		if *have == "" {
			*have = ini["Settings"][key]
		}
	}
	fallback(&a.GTKTheme, "gtk-theme-name")
	fallback(&a.IconTheme, "gtk-icon-theme-name")
	fallback(&a.CursorTheme, "gtk-cursor-theme-name")
	fallback(&a.FontName, "gtk-font-name")

	qt := readINI(filepath.Join(configHome(), "qt6ct/qt6ct.conf"))
	a.QtStyle = qt["Appearance"]["style"]
	return a
}

// ApplyAppearance writes the look everywhere a toolkit looks for it. Files
// that other tools also write -- the GTK settings, the Qt configs -- are
// read, changed and written back, so nothing else in them is lost.
func (c *Client) ApplyAppearance(ctx context.Context, a Appearance) error {
	if c.Demo {
		return nil
	}
	home := configHome()
	dark := a.ColorScheme == "prefer-dark"
	size := strconv.Itoa(a.CursorSize)

	gtk := map[string]string{
		"gtk-theme-name":                    a.GTKTheme,
		"gtk-icon-theme-name":               a.IconTheme,
		"gtk-cursor-theme-name":             a.CursorTheme,
		"gtk-cursor-theme-size":             size,
		"gtk-font-name":                     a.FontName,
		"gtk-application-prefer-dark-theme": boolStr(dark),
	}
	for _, dir := range []string{"gtk-3.0", "gtk-4.0"} {
		if err := writeINI(filepath.Join(home, dir, "settings.ini"), "Settings", gtk); err != nil {
			return err
		}
	}

	// Qt reads the style and icons from qt5ct/qt6ct. Fonts there are stored
	// as serialised Qt values, so they are left to those tools.
	qt := map[string]string{"icon_theme": a.IconTheme}
	if a.QtStyle != "" {
		qt["style"] = a.QtStyle
	}
	for _, name := range []string{"qt5ct/qt5ct.conf", "qt6ct/qt6ct.conf"} {
		path := filepath.Join(home, name)
		if !exists(filepath.Dir(path)) {
			continue // that toolkit's settings tool is not installed
		}
		if err := writeINI(path, "Appearance", qt); err != nil {
			return err
		}
	}

	// The XDG cursor fallback, for applications that read neither.
	cursorDefault := filepath.Join(dataHome(), "icons/default/index.theme")
	if err := writeFile(cursorDefault, "[Icon Theme]\nName=Default\nComment=Voidbleed default cursor\nInherits="+a.CursorTheme+"\n"); err != nil {
		return err
	}

	// niri's own cursor, in the per-machine include so the shipped config
	// stays untouched.
	if err := writeNiriCursor(filepath.Join(home, "niri/local.kdl"), a.CursorTheme, a.CursorSize); err != nil {
		return err
	}

	for key, value := range map[string]string{
		"gtk-theme":    a.GTKTheme,
		"icon-theme":   a.IconTheme,
		"cursor-theme": a.CursorTheme,
		"cursor-size":  size,
		"font-name":    a.FontName,
		"color-scheme": a.ColorScheme,
	} {
		if value == "" {
			continue
		}
		if err := c.Run.Run(ctx, sys.Command("gsettings", "set", gsettingsSchema, key, value)); err != nil {
			return err
		}
	}
	return nil
}

// InstalledThemes scans the directories the toolkits scan.
func InstalledThemes() Themes {
	var t Themes
	for _, dir := range themeDirs("themes") {
		for _, name := range subdirs(dir) {
			if exists(filepath.Join(dir, name, "gtk-3.0")) || exists(filepath.Join(dir, name, "gtk-4.0")) {
				t.GTK = append(t.GTK, name)
			}
		}
	}
	for _, dir := range themeDirs("icons") {
		for _, name := range subdirs(dir) {
			// A cursor theme is a directory with cursors in it; some have no
			// index.theme at all, so that is the only reliable test.
			if exists(filepath.Join(dir, name, "cursors")) {
				t.Cursors = append(t.Cursors, name)
				continue
			}
			index := filepath.Join(dir, name, "index.theme")
			if !exists(index) || name == "default" {
				continue
			}
			ini := readINI(index)
			if ini["Icon Theme"]["Hidden"] == "true" {
				continue
			}
			if _, ok := ini["Icon Theme"]; ok {
				t.Icons = append(t.Icons, name)
			}
		}
	}
	// Qt styles worth offering; qt6ct lists more, but these are the ones a
	// Voidbleed machine actually has.
	t.QtStyle = []string{"Fusion", "Windows", "kvantum", "kvantum-dark"}
	for _, list := range []*[]string{&t.GTK, &t.Icons, &t.Cursors} {
		*list = unique(*list)
	}
	return t
}

func themeDirs(kind string) []string {
	return []string{
		filepath.Join(dataHome(), kind),
		filepath.Join(os.Getenv("HOME"), "."+kind),
		"/usr/local/share/" + kind,
		"/usr/share/" + kind,
	}
}

func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			names = append(names, e.Name())
		}
	}
	return names
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// writeNiriCursor replaces the cursor block in niri's per-machine include,
// leaving everything else in the file alone.
func writeNiriCursor(path, theme string, size int) error {
	body, _ := os.ReadFile(path)
	var kept []string
	skip := false
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case skip:
			if trimmed == "}" {
				skip = false
			}
			continue
		case strings.HasPrefix(trimmed, "cursor {"):
			skip = !strings.HasSuffix(trimmed, "}")
			continue
		}
		kept = append(kept, line)
	}
	text := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if text != "" {
		text += "\n\n"
	}
	text += "// Written by the Voidbleed control centre.\ncursor {\n    xcursor-theme " +
		strconv.Quote(theme) + "\n    xcursor-size " + strconv.Itoa(size) + "\n}\n"
	return writeFile(path, text)
}

// Small helpers for the INI-shaped files the toolkits use.

func readINI(path string) map[string]map[string]string {
	out := map[string]map[string]string{}
	body, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	section := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";"):
			continue
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = strings.Trim(line, "[]")
			out[section] = map[string]string{}
		default:
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			out[section][strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}

// writeINI sets keys in one section, keeping every other line of the file.
func writeINI(path, section string, values map[string]string) error {
	body, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	written := map[string]bool{}
	inSection := false
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inSection {
				out = append(out, remaining(values, written)...)
			}
			inSection = strings.Trim(trimmed, "[]") == section
			out = append(out, line)
			continue
		}
		if inSection {
			if key, _, ok := strings.Cut(trimmed, "="); ok {
				key = strings.TrimSpace(key)
				if value, found := values[key]; found {
					if value != "" {
						out = append(out, key+"="+value)
					}
					written[key] = true
					continue
				}
			}
		}
		out = append(out, line)
	}
	if !inSection {
		out = append(out, "["+section+"]")
	}
	out = append(out, remaining(values, written)...)
	return writeFile(path, strings.Join(out, "\n")+"\n")
}

func remaining(values map[string]string, written map[string]bool) []string {
	var keys []string
	for key := range values {
		if !written[key] && values[key] != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
		written[key] = true
	}
	return out
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func configHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(os.Getenv("HOME"), ".config")
}

func dataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(os.Getenv("HOME"), ".local/share")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
