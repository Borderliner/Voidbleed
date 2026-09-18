package system

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"voidbleed/internal/sys"
)

// Where runit keeps services. Enabling one means linking it into the runlevel
// directory; /var/service is a symlink to the current runlevel, so the link is
// made in "default" directly -- that is the one that survives a reboot.
const (
	ServiceDir = "/etc/sv"
	EnabledDir = "/etc/runit/runsvdir/default"
	LiveDir    = "/var/service"
)

// Service is one runit service.
type Service struct {
	Name    string
	Enabled bool   // linked into the runlevel: it comes up at boot
	State   string // "run", "down", "" when unknown (reading it needs root)
	Since   string // how long it has been in that state
	PID     string
	Down    bool // /etc/sv/<name>/down: supervised, but not started
}

func (s Service) Running() bool { return s.State == "run" }

// Services lists every service definition on the machine and whether it is
// enabled. The run state is left empty: runit keeps its supervise sockets
// root-only, so Status fills it in when there are privileges to do so.
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	entries, err := os.ReadDir(ServiceDir)
	if err != nil {
		return nil, err
	}
	enabled := map[string]bool{}
	if links, err := os.ReadDir(EnabledDir); err == nil {
		for _, l := range links {
			enabled[l.Name()] = true
		}
	}
	var services []Service
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		name := e.Name()
		_, down := os.Stat(filepath.Join(ServiceDir, name, "down"))
		services = append(services, Service{
			Name:    name,
			Enabled: enabled[name],
			Down:    down == nil,
		})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services, nil
}

// Status asks runit how each enabled service is doing. It needs root, so the
// interface only calls it once there is a password.
func (c *Client) Status(ctx context.Context, services []Service) []Service {
	var names []string
	for _, s := range services {
		if s.Enabled {
			names = append(names, filepath.Join(LiveDir, s.Name))
		}
	}
	if len(names) == 0 {
		return services
	}
	out, err := c.privOutput(ctx, "sv", append([]string{"status"}, names...)...)
	if err != nil && strings.TrimSpace(out) == "" {
		return services
	}
	states := map[string]Service{}
	for _, line := range lines(out) {
		if s, ok := parseStatus(line); ok {
			states[s.Name] = s
		}
	}
	for i, s := range services {
		if got, ok := states[s.Name]; ok {
			services[i].State, services[i].Since, services[i].PID = got.State, got.Since, got.PID
		}
	}
	return services
}

// parseStatus reads one line of `sv status`:
//
//	run: /var/service/dbus: (pid 1234) 4231s
//	down: /var/service/cupsd: 12s, normally up
func parseStatus(line string) (Service, bool) {
	state, rest, ok := strings.Cut(line, ": ")
	if !ok {
		return Service{}, false
	}
	path, rest, ok := strings.Cut(rest, ": ")
	if !ok {
		return Service{}, false
	}
	s := Service{Name: filepath.Base(path), State: strings.TrimSpace(state)}
	if pid, after, found := strings.Cut(rest, ")"); found && strings.HasPrefix(pid, "(pid ") {
		s.PID = strings.TrimPrefix(pid, "(pid ")
		rest = strings.TrimSpace(after)
	}
	s.Since = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), ", normally up"))
	return s, true
}

// Service actions.

// EnableCmd links a service into the runlevel so it starts at boot, and runit
// picks it up within seconds without anything else being asked of it.
func EnableCmd(name string) sys.Cmd {
	return sys.Command("ln", "-sfn", filepath.Join(ServiceDir, name), filepath.Join(EnabledDir, name))
}

func DisableCmd(name string) sys.Cmd {
	return sys.Command("rm", "-f", filepath.Join(EnabledDir, name))
}

func ServiceCmd(action, name string) sys.Cmd {
	return sys.Command("sv", action, filepath.Join(LiveDir, name))
}
