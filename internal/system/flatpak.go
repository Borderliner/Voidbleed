package system

import (
	"context"
	"encoding/json"
	"sort"

	"voidbleed/internal/sys"
)

// Flatpak is one installed or updatable Flathub application.
type Flatpak struct {
	ID           string `json:"application_id"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Branch       string `json:"branch"`
	Origin       string `json:"origin"`
	Installation string `json:"installation"` // "system" or "user"
	Runtime      bool   // platforms and runtimes, not applications
	Update       bool   // an update is waiting
}

// FlatpakRemote is a configured source, usually just Flathub.
type FlatpakRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Flatpaks lists installed applications, marking the ones with updates.
// Runtimes are listed too but flagged, so the page can keep them out of the
// way: nobody installs a platform on purpose.
func (c *Client) Flatpaks(ctx context.Context) ([]Flatpak, error) {
	apps, err := c.flatpakList(ctx, "list", "--app", "-j")
	if err != nil {
		return nil, err
	}
	runtimes, _ := c.flatpakList(ctx, "list", "--runtime", "-j")
	for i := range runtimes {
		runtimes[i].Runtime = true
	}
	apps = append(apps, runtimes...)

	// remote-ls --updates reaches the network; a machine that is offline
	// still gets its list, just without the update marks.
	if updates, err := c.flatpakList(ctx, "remote-ls", "--updates", "-j"); err == nil {
		waiting := make(map[string]bool, len(updates))
		for _, u := range updates {
			waiting[u.ID] = true
		}
		for i := range apps {
			apps[i].Update = waiting[apps[i].ID]
		}
	}
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Runtime != apps[j].Runtime {
			return !apps[i].Runtime
		}
		return apps[i].Name < apps[j].Name
	})
	return apps, nil
}

func (c *Client) flatpakList(ctx context.Context, args ...string) ([]Flatpak, error) {
	out, err := c.output(ctx, "flatpak", args...)
	if err != nil {
		return nil, err
	}
	var items []Flatpak
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		return nil, err
	}
	return items, nil
}

// FlatpakRemotes lists where applications can come from.
func (c *Client) FlatpakRemotes(ctx context.Context) ([]FlatpakRemote, error) {
	out, err := c.output(ctx, "flatpak", "remotes", "-j")
	if err != nil {
		return nil, err
	}
	var remotes []FlatpakRemote
	if err := json.Unmarshal([]byte(out), &remotes); err != nil {
		return nil, err
	}
	return remotes, nil
}

// SearchFlatpak asks the remotes for applications matching a term.
func (c *Client) SearchFlatpak(ctx context.Context, term string) ([]Flatpak, error) {
	// search has no --json; its columns are tab separated.
	out, err := c.output(ctx, "flatpak", "search", "--columns=application,name,version,remotes", term)
	if err != nil {
		return nil, nil // "No matches found" is an error exit, not a problem
	}
	var found []Flatpak
	for _, line := range lines(out) {
		cols := splitTabs(line)
		if len(cols) < 2 {
			continue
		}
		app := Flatpak{ID: cols[0], Name: cols[1]}
		if len(cols) > 2 {
			app.Version = cols[2]
		}
		if len(cols) > 3 {
			app.Origin = cols[3]
		}
		found = append(found, app)
	}
	return found, nil
}

// Flatpak actions. --system keeps applications available to every account,
// which is where the installer puts them too.

func FlatpakInstallCmd(remote, id string) sys.Cmd {
	return sys.Command("flatpak", "install", "--system", "--noninteractive", "-y", remote, id)
}

func FlatpakRemoveCmd(id string) sys.Cmd {
	return sys.Command("flatpak", "uninstall", "--system", "--noninteractive", "-y", id)
}

func FlatpakUpdateCmd(ids ...string) sys.Cmd {
	return sys.Command("flatpak", append([]string{"update", "--system", "--noninteractive", "-y"}, ids...)...)
}

// FlatpakPruneCmd removes runtimes nothing uses any more.
func FlatpakPruneCmd() sys.Cmd {
	return sys.Command("flatpak", "uninstall", "--system", "--unused", "--noninteractive", "-y")
}

func splitTabs(line string) []string {
	var cols []string
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '\t' {
			cols = append(cols, line[start:i])
			start = i + 1
		}
	}
	return append(cols, line[start:])
}
