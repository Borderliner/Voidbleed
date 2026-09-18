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

func TestFirewallWithoutUfwOffersNothingToRun(t *testing.T) {
	restore := Have
	Have = func(name string) bool { return name == "iptables" }
	defer func() { Have = restore }()

	runner := &fakeRunner{}
	fw := NewWith(runner, 0).FirewallState(context.Background())
	if fw.Installed {
		t.Error("ufw is not installed, so the page must not think it can drive one")
	}
	if len(fw.Others) != 1 || fw.Others[0] != "iptables" {
		t.Errorf("iptables should be reported: %+v", fw.Others)
	}
	for _, line := range runner.seen {
		if strings.Contains(line, "ufw") {
			t.Errorf("ran a ufw command on a machine without ufw: %s", line)
		}
	}
}
func TestParseUfwStatus(t *testing.T) {
	var fw Firewall
	parseUfw(`Status: active
Default: deny (incoming), allow (outgoing), disabled (routed)

To                         Action      From
--                         ------      ----
[ 1] 22/tcp                ALLOW IN    Anywhere
`, &fw)
	if !fw.Active {
		t.Error("an active firewall was read as inactive")
	}
	if len(fw.Rules) != 1 || fw.Rules[0].Number != "1" || fw.Rules[0].To != "22/tcp" {
		t.Errorf("rules read as %+v", fw.Rules)
	}
}

func TestInstalledPackages(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"xbps-query -l": "ii btop-1.4.7_1   Monitor of resources\nii niri-26.04_1   Compositor\n",
		"xbps-query -m": "btop-1.4.7_1\n",
		"xbps-query -O": "niri-26.04_1\n",
	}}
	pkgs, err := NewWith(runner, 1000).Installed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages", len(pkgs))
	}
	btop := pkgs[0]
	if btop.Name != "btop" || btop.Version != "1.4.7_1" || !btop.Manual || btop.Orphan {
		t.Errorf("btop read as %+v", btop)
	}
	if !pkgs[1].Orphan {
		t.Errorf("niri should be marked an orphan: %+v", pkgs[1])
	}
}
func TestUpdateCheckDoesNotNeedRoot(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"xbps-install": "niri-26.05_1 update x86_64 https://repo 9437184 3145728\n",
	}}
	c := NewWith(runner, 1000)
	updates, err := c.Updates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Name != "niri" || updates[0].NewVersion != "26.05_1" {
		t.Errorf("updates read as %+v", updates)
	}
	for _, line := range runner.seen {
		if strings.Contains(line, "xbps-install") && !strings.Contains(line, "-M") {
			t.Errorf("update check would write to /var/db/xbps: %s", line)
		}
		if strings.Contains(line, "sudo") {
			t.Errorf("update check asked for root: %s", line)
		}
	}
}
