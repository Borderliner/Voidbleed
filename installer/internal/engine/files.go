package engine

import (
	"fmt"
	"strings"

	"voidbleed/installer/internal/config"
)

// fstab renders /etc/fstab. uuids maps device path to filesystem UUID.
func (p *Plan) fstab(uuids map[string]string) string {
	var b strings.Builder
	b.WriteString("# /etc/fstab: written by the Voidbleed installer\n")
	b.WriteString("# <device>\t<mountpoint>\t<type>\t<options>\t<dump>\t<pass>\n")
	root := "UUID=" + uuids[p.RootDev]
	switch p.Config.Disk.Filesystem {
	case config.Btrfs:
		for _, sv := range p.Subvolumes {
			opts := "subvol=" + sv.Name + "," + btrfsOptions
			if sv.NoCompress {
				opts = "subvol=" + sv.Name + ",noatime"
			}
			fmt.Fprintf(&b, "%s\t%s\tbtrfs\t%s\t0\t0\n", root, sv.Mountpoint, opts)
		}
	default:
		fmt.Fprintf(&b, "%s\t/\text4\tdefaults,noatime\t0\t1\n", root)
	}
	fmt.Fprintf(&b, "UUID=%s\t/boot\tvfat\tumask=0077\t0\t2\n", uuids[p.ESP])
	switch {
	case p.SwapPart != "":
		fmt.Fprintf(&b, "UUID=%s\tnone\tswap\tdefaults\t0\t0\n", uuids[p.SwapPart])
	case p.SwapFile != "":
		fmt.Fprintf(&b, "%s\tnone\tswap\tdefaults\t0\t0\n", p.SwapFile)
	}
	b.WriteString("tmpfs\t/tmp\ttmpfs\tdefaults,nosuid,nodev\t0\t0\n")
	return b.String()
}

func crypttab(luksUUID string) string {
	return "# <name>\t<device>\t<keyfile>\t<options>\n" +
		fmt.Sprintf("%s\tUUID=%s\tnone\tluks,discard\n", MapperName, luksUUID)
}

func (p *Plan) dracutConf() string {
	s := "# Voidbleed: only include what this machine needs.\nhostonly=\"yes\"\n"
	if p.Config.Disk.Encrypt {
		s += "add_dracutmodules+=\" crypt \"\ninstall_items+=\" /etc/crypttab \"\n"
	}
	return s
}

func (p *Plan) grubCmdline(luksUUID string) string {
	args := append([]string{}, p.Cmdline...)
	if p.Config.Disk.Encrypt {
		args = append(args, "rd.luks.uuid="+luksUUID, "rd.luks.options=discard")
	}
	return strings.Join(args, " ")
}

// setShellVars sets KEY="value" assignments in a shell-style config file,
// replacing existing (possibly commented) assignments and appending the rest.
func setShellVars(content string, vars [][2]string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if content == "" {
		lines = nil
	}
	for _, kv := range vars {
		key, line := kv[0], fmt.Sprintf("%s=%q", kv[0], kv[1])
		done := false
		for i, l := range lines {
			trimmed := strings.TrimLeft(strings.TrimSpace(l), "#")
			if strings.HasPrefix(trimmed, key+"=") {
				if !done {
					lines[i] = line
					done = true
				} else {
					lines[i] = "#" + strings.TrimLeft(l, "#")
				}
			}
		}
		if !done {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// enableLocale uncomments the locale's line in /etc/default/libc-locales.
func enableLocale(content, locale string) (string, bool) {
	lines := strings.Split(content, "\n")
	found := false
	for i, l := range lines {
		fields := strings.Fields(strings.TrimLeft(l, "#"))
		if len(fields) >= 1 && fields[0] == locale {
			lines[i] = strings.TrimLeft(l, "#")
			found = true
		}
	}
	return strings.Join(lines, "\n"), found
}

func (p *Plan) niriLocal() string {
	return "// Written by the Voidbleed installer: settings for this machine.\n" +
		"// Anything here overrides config.kdl.\n\n" +
		"input {\n    keyboard {\n        xkb {\n" +
		fmt.Sprintf("            layout %q\n", strings.Join(p.Config.System.KeyboardLayouts, ",")) +
		"        }\n    }\n}\n"
}

func (p *Plan) greeterKeyboard() string {
	return fmt.Sprintf("\n[keyboard]\nlayout = %q\n", strings.Join(p.Config.System.KeyboardLayouts, ","))
}
