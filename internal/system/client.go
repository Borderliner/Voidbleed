// Package system is what the control centre knows how to do to a running
// Voidbleed machine: packages, Flatpaks, services, firmware, appearance and
// the firewall. Everything here is a thin, typed wrapper around the tools
// Void already ships -- reading is cheap and unprivileged, changing anything
// goes through sudo.
package system

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"voidbleed/internal/sys"
)

// Client runs commands for one machine.
type Client struct {
	Run sys.Runner
	// Password unlocks sudo when the program is not already root. It is held
	// only in memory, for as long as the program runs.
	Password string
	// Demo answers from canned output and writes nothing: the interface can
	// be driven end to end without a machine to change.
	Demo bool
	uid  int
}

func New(log func(string)) *Client {
	return &Client{Run: sys.Real{Log: log, Env: os.Environ()}, uid: os.Getuid()}
}

// NewWith is for tests that want to watch what would be run.
func NewWith(r sys.Runner, uid int) *Client { return &Client{Run: r, uid: uid} }

// NewDemo reads and writes nothing at all.
func NewDemo(r sys.Runner) *Client { return &Client{Run: r, uid: 1000, Demo: true} }

// Root reports whether commands already run with full privileges.
func (c *Client) Root() bool { return c.uid == 0 }

// NeedsPassword reports whether sudo will ask for one: it will not when the
// program is root, and it will not while a sudo timestamp is still valid.
func (c *Client) NeedsPassword(ctx context.Context) bool {
	if c.Demo || c.Root() || c.Password != "" {
		return false
	}
	return exec.CommandContext(ctx, "sudo", "-n", "true").Run() != nil
}

// CheckPassword reports whether the password would unlock sudo, so the user
// finds out at the prompt rather than half way through an install.
func (c *Client) CheckPassword(ctx context.Context, password string) bool {
	if c.Demo {
		return true
	}
	cmd := exec.CommandContext(ctx, "sudo", "-S", "-p", "", "-k", "true")
	cmd.Stdin = strings.NewReader(password + "\n")
	return cmd.Run() == nil
}

// priv wraps a command so it runs as root: unchanged when the program already
// is, through sudo otherwise. -S reads the password from stdin, and an empty
// prompt keeps sudo from writing one into the captured output.
func (c *Client) priv(cmd sys.Cmd) sys.Cmd {
	if c.Root() {
		return cmd
	}
	args := append([]string{"-S", "-p", "", cmd.Name}, cmd.Args...)
	out := sys.Command("sudo", args...)
	if c.Password != "" {
		out = out.WithStdin(c.Password+"\n", true)
	}
	return out
}

// output runs a read-only command and returns its stdout.
func (c *Client) output(ctx context.Context, name string, args ...string) (string, error) {
	return c.Run.Output(ctx, sys.Command(name, args...))
}

// lines splits command output, dropping blanks.
func lines(out string) []string {
	var result []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimRight(l, " \t\r"); l != "" {
			result = append(result, l)
		}
	}
	return result
}

// splitPkgver turns "firefox-142.0_1" into ("firefox", "142.0_1"), the way
// xbps names packages: the version is everything after the last dash.
func splitPkgver(pkgver string) (name, version string) {
	i := strings.LastIndex(pkgver, "-")
	if i < 0 {
		return pkgver, ""
	}
	return pkgver[:i], pkgver[i+1:]
}

// Have reports whether a program is installed, so pages for tools this
// machine lacks can say so instead of failing.
func Have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
