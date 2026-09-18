package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"voidbleed/internal/sys"
)

// Firewall is what is guarding the machine, if anything.
//
// Void ships three backends that each bring their own runit service, and
// running two at once is how people lock themselves out of their own boxes.
// The control centre drives ufw -- the one with a usable command line -- and
// reports the others rather than touching them.
type Firewall struct {
	Backend   string // "ufw", "nftables", "iptables" or ""
	Installed bool
	Enabled   bool // starts at boot
	Active    bool // filtering right now
	Policy    string
	Rules     []FirewallRule
	// Others names the backends installed besides ufw, which is worth saying
	// out loud before anyone turns a second one on; OtherEnabled is the ones
	// already starting at boot.
	Others       []string
	OtherEnabled []string
	// Unknown is set when the state could not be read without a password.
	Unknown bool
}

type FirewallRule struct {
	Number string
	To     string
	Action string
	From   string
}

var firewallBackends = []string{"ufw", "nftables", "iptables"}

// FirewallState reads what is installed and, when there are privileges for
// it, what is actually running.
func (c *Client) FirewallState(ctx context.Context) Firewall {
	fw := Firewall{}
	// Only ufw is driven from here. The others are reported so a machine with
	// iptables rules of its own does not look unprotected, but this page will
	// not run ufw commands at something that is not ufw.
	for _, name := range firewallBackends {
		if !Have(name) {
			continue
		}
		if name == "ufw" {
			fw.Backend, fw.Installed = name, true
		} else {
			fw.Others = append(fw.Others, name)
		}
	}
	if !fw.Installed {
		for _, name := range fw.Others {
			if _, err := os.Lstat(filepath.Join(EnabledDir, name)); err == nil {
				fw.OtherEnabled = append(fw.OtherEnabled, name)
			}
		}
		return fw
	}
	_, err := os.Lstat(filepath.Join(EnabledDir, fw.Backend))
	fw.Enabled = err == nil

	out, err := c.privOutput(ctx, "ufw", "status", "verbose")
	if err != nil && strings.TrimSpace(out) == "" {
		fw.Unknown = true
		return fw
	}
	parseUfw(out, &fw)
	return fw
}

// parseUfw reads `ufw status verbose`:
//
//	Status: active
//	Default: deny (incoming), allow (outgoing), disabled (routed)
//	To                         Action      From
//	22/tcp                     ALLOW IN    Anywhere
func parseUfw(out string, fw *Firewall) {
	for _, line := range lines(out) {
		switch {
		case strings.HasPrefix(line, "Status:"):
			fw.Active = strings.Contains(line, "active")
		case strings.HasPrefix(line, "Default:"):
			fw.Policy = strings.TrimSpace(strings.TrimPrefix(line, "Default:"))
		case strings.HasPrefix(line, "To") || strings.HasPrefix(line, "--"):
			continue
		default:
			if rule, ok := parseUfwRule(line); ok {
				fw.Rules = append(fw.Rules, rule)
			}
		}
	}
}

func parseUfwRule(line string) (FirewallRule, bool) {
	// Numbered output puts "[ 1] " first; both shapes are accepted.
	rule := FirewallRule{}
	if strings.HasPrefix(line, "[") {
		num, rest, ok := strings.Cut(strings.TrimPrefix(line, "["), "]")
		if !ok {
			return rule, false
		}
		rule.Number, line = strings.TrimSpace(num), rest
	}
	// The three columns are separated by runs of spaces.
	fields := strings.Split(strings.TrimSpace(line), "  ")
	var cols []string
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			cols = append(cols, f)
		}
	}
	if len(cols) < 3 {
		return rule, false
	}
	rule.To, rule.Action, rule.From = cols[0], cols[1], cols[2]
	return rule, true
}

// Firewall actions. Enabling ufw also enables its service, so the rules come
// back after a reboot -- a firewall that forgets is worse than none.

func FirewallEnableCmds() []sys.Cmd {
	return []sys.Cmd{
		sys.Command("ufw", "--force", "enable"),
		EnableCmd("ufw"),
	}
}

func FirewallDisableCmds() []sys.Cmd {
	return []sys.Cmd{
		sys.Command("ufw", "disable"),
		DisableCmd("ufw"),
	}
}

func FirewallRuleCmd(action, rule string) sys.Cmd {
	return sys.Command("ufw", action, rule)
}

func FirewallDeleteCmd(number string) sys.Cmd {
	return sys.Command("ufw", "--force", "delete", number)
}

// FirewallDefaultCmd sets the policy for traffic no rule matches.
func FirewallDefaultCmd(policy, direction string) sys.Cmd {
	return sys.Command("ufw", "default", policy, direction)
}
