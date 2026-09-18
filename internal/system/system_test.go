package system

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"voidbleed/internal/sys"
)

// fakeRunner answers from a table of canned output and records what it was
// asked to run.
type fakeRunner struct {
	out  map[string]string
	seen []string
}

func (f *fakeRunner) Run(ctx context.Context, cmd sys.Cmd) error {
	f.seen = append(f.seen, cmd.String())
	return nil
}

func (f *fakeRunner) Output(ctx context.Context, cmd sys.Cmd) (string, error) {
	line := cmd.String()
	f.seen = append(f.seen, line)
	for match, out := range f.out {
		if strings.Contains(line, match) {
			return out, nil
		}
	}
	return "", nil
}

func (f *fakeRunner) WriteFile(string, []byte, fs.FileMode) error { return nil }
func (f *fakeRunner) AppendFile(string, []byte) error             { return nil }
func (f *fakeRunner) ReadFile(string) ([]byte, error)             { return nil, nil }
func (f *fakeRunner) MkdirAll(string, fs.FileMode) error          { return nil }
func (f *fakeRunner) Symlink(string, string) error                { return nil }
func (f *fakeRunner) RemoveAll(string) error                      { return nil }
func (f *fakeRunner) Exists(string) bool                          { return false }
func (f *fakeRunner) Glob(string) ([]string, error)               { return nil, nil }

func TestDisableStopsBeforeUnlinking(t *testing.T) {
	cmd := DisableCmd("iptables").String()
	down, unlink := strings.Index(cmd, "sv down"), strings.Index(cmd, "rm -f")
	if down < 0 || unlink < 0 {
		t.Fatalf("disable does not both stop and unlink: %s", cmd)
	}
	if down > unlink {
		t.Errorf("disable unlinks before stopping, which cannot work:\n%s", cmd)
	}
	if !strings.Contains(cmd, "|| true") {
		t.Errorf("stopping a service that is already down must not fail the action:\n%s", cmd)
	}
}

func TestParseServiceStatus(t *testing.T) {
	for _, tc := range []struct {
		line       string
		name, want string
		pid        string
	}{
		{"run: /var/service/dbus: (pid 1204) 84231s", "dbus", "run", "1204"},
		{"down: /var/service/cupsd: 12s, normally up", "cupsd", "down", ""},
	} {
		got, ok := parseStatus(tc.line)
		if !ok {
			t.Fatalf("could not read %q", tc.line)
		}
		if got.Name != tc.name || got.State != tc.want || got.PID != tc.pid {
			t.Errorf("%q read as %+v", tc.line, got)
		}
	}
}
