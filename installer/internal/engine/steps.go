package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"voidbleed/installer/internal/config"
	"voidbleed/installer/internal/hw"
	"voidbleed/installer/internal/sys"
)

// Step is one unit of the install shown in the progress view.
type Step struct {
	ID     string
	Title  string
	Weight int // relative duration, for the overall progress bar
	Run    func(ctx context.Context, x *Exec) error
}

// Exec is the state shared by steps while an install runs.
type Exec struct {
	Plan *Plan
	R    sys.Runner
	// NeedNetwork is decided in preflight: network installs and packages
	// missing from the live image require it. Online records whether the
	// mirror answered; Flatpak setup is deferred to first boot when offline.
	NeedNetwork bool
	Online      bool
	missing     []string
	uuids       map[string]string
	luksUUID    string
	mounted     bool
	chrootReady bool
	opened      bool
}

func (x *Exec) run(ctx context.Context, name string, args ...string) error {
	return x.R.Run(ctx, sys.Command(name, args...))
}

func (x *Exec) chroot(ctx context.Context, name string, args ...string) error {
	return x.R.Run(ctx, sys.Command(name, args...).InChroot(x.Plan.Paths.Target))
}

// Steps returns the install sequence for plan.
func Steps(p *Plan) []Step {
	steps := []Step{
		{"preflight", "Checking the machine", 1, preflight},
		{"partition", "Partitioning " + p.Config.Disk.Device, 1, partition},
	}
	if p.Config.Disk.Encrypt {
		steps = append(steps, Step{"encrypt", "Encrypting the root partition", 3, encrypt})
	}
	steps = append(steps,
		Step{"format", "Creating filesystems", 2, format},
		Step{"mount", "Mounting the new system", 1, mount},
	)
	if p.Offline() {
		steps = append(steps,
			Step{"copy", "Copying the live system", 30, copyLive},
			// userdel refuses to remove a user with running processes when
			// /proc is mounted in the chroot, so clean up before mounting it.
			Step{"cleanup", "Removing live session leftovers", 3, cleanupLive},
			Step{"chroot", "Preparing the new system", 1, prepareChroot},
			Step{"packages", "Adjusting software", 8, adjustPackages},
		)
	} else {
		steps = append(steps,
			Step{"chroot", "Preparing the new system", 1, prepareChroot},
			Step{"install", "Installing packages", 40, installPackages},
		)
	}
	steps = append(steps,
		Step{"system", "Configuring language, time and storage", 4, configureSystem},
		Step{"swap", "Setting up swap", 1, setupSwap},
		Step{"users", "Creating your account", 2, createUsers},
		Step{"services", "Enabling services", 1, enableServices},
	)
	if len(p.Selection.Flatpaks) > 0 || slices.Contains(p.Groups, "flatpak") {
		steps = append(steps, Step{"flatpak", "Setting up Flatpak apps", 15, setupFlatpak})
	}
	steps = append(steps,
		Step{"bootloader", "Installing the bootloader", 8, bootloader},
		Step{"finish", "Finishing up", 1, finish},
	)
	return steps
}

func preflight(ctx context.Context, x *Exec) error {
	p := x.Plan
	if !p.Facts.UEFI || !p.Facts.UEFI64 {
		return errors.New("Voidbleed needs a machine booted in 64-bit UEFI mode")
	}
	var disk *hw.Disk
	for i := range p.Facts.Disks {
		if p.Facts.Disks[i].Path == p.Config.Disk.Device {
			disk = &p.Facts.Disks[i]
		}
	}
	if disk == nil {
		return fmt.Errorf("disk %s not found", p.Config.Disk.Device)
	}
	if disk.LiveMedium {
		return fmt.Errorf("%s holds the running live system", disk.Path)
	}
	var mounted []string
	for _, part := range disk.Partitions {
		if part.Mountpoint != "" {
			mounted = append(mounted, part.Path+" on "+part.Mountpoint)
		}
	}
	if len(mounted) > 0 {
		return fmt.Errorf("%s is in use: %s", disk.Path, strings.Join(mounted, ", "))
	}
	need := uint64(MinDiskBytes)
	if p.Config.Swap.Mode == config.SwapPartition || p.Config.Swap.Mode == config.SwapFile {
		need += uint64(p.Config.Swap.SizeMiB) << 20
	}
	if disk.SizeBytes < need {
		return fmt.Errorf("%s is %d GiB; at least %d GiB is needed", disk.Path, disk.SizeBytes>>30, need>>30)
	}

	x.NeedNetwork = !p.Offline()
	if p.Offline() {
		for _, pkg := range p.Packages {
			if _, err := x.R.Output(ctx, sys.Command("xbps-query", "-p", "pkgver", pkg)); err != nil {
				x.missing = append(x.missing, pkg)
			}
		}
		if len(x.missing) > 0 {
			x.NeedNetwork = true
		}
	}
	probe := strings.TrimRight(p.Config.Install.Mirror, "/") + "/current/x86_64-repodata"
	_, probeErr := x.R.Output(ctx, sys.Command("curl", "-fsI", "--max-time", "15", probe))
	x.Online = probeErr == nil
	if x.NeedNetwork && !x.Online {
		reason := "network install"
		if p.Offline() && len(x.missing) > 0 {
			reason = "these packages aren't on the live image: " + strings.Join(x.missing, ", ")
		}
		return fmt.Errorf("no connection to %s (needed for %s)", p.Config.Install.Mirror, reason)
	}
	if x.R.Exists(p.Paths.Target) {
		if out, _ := x.R.Output(ctx, sys.Command("findmnt", "-rno", "TARGET", "-R", p.Paths.Target)); strings.TrimSpace(out) != "" {
			return fmt.Errorf("%s is already mounted; unmount it first", p.Paths.Target)
		}
	}
	return nil
}

func partition(ctx context.Context, x *Exec) error {
	p := x.Plan
	disk := p.Config.Disk.Device
	script := "label: gpt\n" +
		fmt.Sprintf("size=%dMiB, type=U, name=\"EFI\"\n", espSizeMiB)
	if p.SwapPart != "" {
		script += fmt.Sprintf("size=%dMiB, type=S, name=\"swap\"\n", p.Config.Swap.SizeMiB)
	}
	script += "type=L, name=\"voidbleed\"\n"

	steps := []sys.Cmd{
		sys.Command("wipefs", "--all", "--force", disk),
		sys.Command("sfdisk", "--wipe", "always", "--wipe-partitions", "always", disk).WithStdin(script, false),
		sys.Command("udevadm", "settle", "--timeout=30"),
	}
	for _, part := range []string{p.ESP, p.SwapPart, p.RootPart} {
		if part != "" {
			steps = append(steps, sys.Command("wipefs", "--all", "--force", part))
		}
	}
	for _, c := range steps {
		if err := x.R.Run(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func encrypt(ctx context.Context, x *Exec) error {
	p := x.Plan
	pass := p.Config.Disk.Passphrase
	if err := x.R.Run(ctx, sys.Command("cryptsetup", "luksFormat", "--type", "luks2", "--batch-mode",
		"--label", "voidbleed", "--key-file", "-", p.RootPart).WithStdin(pass, true)); err != nil {
		return err
	}
	if err := x.R.Run(ctx, sys.Command("cryptsetup", "open", "--allow-discards", "--key-file", "-",
		p.RootPart, MapperName).WithStdin(pass, true)); err != nil {
		return err
	}
	x.opened = true
	return nil
}

func format(ctx context.Context, x *Exec) error {
	p := x.Plan
	cmds := []sys.Cmd{sys.Command("mkfs.vfat", "-F", "32", "-n", "EFI", p.ESP)}
	if p.SwapPart != "" {
		cmds = append(cmds, sys.Command("mkswap", "-L", "swap", p.SwapPart))
	}
	if p.Config.Disk.Filesystem == config.Btrfs {
		cmds = append(cmds, sys.Command("mkfs.btrfs", "-f", "-L", "voidbleed", p.RootDev))
	} else {
		cmds = append(cmds, sys.Command("mkfs.ext4", "-F", "-L", "voidbleed", p.RootDev))
	}
	for _, c := range cmds {
		if err := x.R.Run(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func mount(ctx context.Context, x *Exec) error {
	p := x.Plan
	t := p.Paths.Target
	if err := x.R.MkdirAll(t, 0o755); err != nil {
		return err
	}
	if p.Config.Disk.Filesystem == config.Btrfs {
		if err := x.run(ctx, "mount", p.RootDev, t); err != nil {
			return err
		}
		for _, sv := range p.Subvolumes {
			if err := x.run(ctx, "btrfs", "subvolume", "create", path.Join(t, sv.Name)); err != nil {
				return err
			}
		}
		if err := x.run(ctx, "umount", t); err != nil {
			return err
		}
		for _, sv := range p.Subvolumes { // "/" first, so the rest mount inside it
			opts := "subvol=" + sv.Name + "," + btrfsOptions
			if sv.NoCompress {
				opts = "subvol=" + sv.Name + ",noatime"
			}
			dir := p.T(sv.Mountpoint)
			if err := x.R.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if err := x.run(ctx, "mount", "-o", opts, p.RootDev, dir); err != nil {
				return err
			}
		}
	} else if err := x.run(ctx, "mount", "-o", "noatime", p.RootDev, t); err != nil {
		return err
	}
	x.mounted = true
	if err := x.R.MkdirAll(p.T("boot"), 0o755); err != nil {
		return err
	}
	return x.run(ctx, "mount", "-o", "umask=0077", p.ESP, p.T("boot"))
}

// copyLive copies the running live system, like void-installer's local mode.
// /boot is copied separately because the ESP (vfat) can't hold ownership.
func copyLive(ctx context.Context, x *Exec) error {
	t := x.Plan.Paths.Target
	script := "tar --create --one-file-system --xattrs --acls --numeric-owner" +
		" --exclude=./boot/* --exclude=./tmp/* --exclude=./var/cache/xbps/*" +
		" --exclude=./var/tmp/* -f - -C / ." +
		" | tar --extract --xattrs --xattrs-include='*' --acls --numeric-owner" +
		" --preserve-permissions -f - -C " + t
	if err := x.R.Run(ctx, sys.Shell(script)); err != nil {
		return err
	}
	return x.run(ctx, "cp", "-r", "/boot/.", x.Plan.T("boot")+"/")
}

// prepareChroot mounts the pseudo filesystems chroot commands need and, for
// network installs, seeds the repository keys and Voidbleed's xbps rules
// (kernel pin, os-release noextract) before the first package lands.
func prepareChroot(ctx context.Context, x *Exec) error {
	p := x.Plan
	if !p.Offline() {
		for _, dir := range []string{"var/db/xbps/keys", "usr/share/xbps.d", "etc"} {
			if err := x.R.MkdirAll(p.T(dir), 0o755); err != nil {
				return err
			}
		}
		seed := "cp " + p.Paths.XbpsKeys + "/*.plist " + p.T("var/db/xbps/keys") + "/" +
			" && cp " + p.Paths.XbpsConfDir + "/05-voidbleed-*.conf " + p.Paths.XbpsConfDir + "/20-voiders-community.conf " + p.T("usr/share/xbps.d") + "/"
		if err := x.R.Run(ctx, sys.Shell(seed)); err != nil {
			return err
		}
	}
	for _, dir := range []string{"dev", "proc", "sys", "run"} {
		if err := x.R.MkdirAll(p.T(dir), 0o755); err != nil {
			return err
		}
	}
	cmds := []sys.Cmd{
		sys.Command("mount", "--rbind", "/dev", p.T("dev")),
		sys.Command("mount", "--make-rslave", p.T("dev")),
		sys.Command("mount", "-t", "proc", "proc", p.T("proc")),
		sys.Command("mount", "--rbind", "/sys", p.T("sys")),
		sys.Command("mount", "--make-rslave", p.T("sys")),
		sys.Command("mount", "-t", "tmpfs", "-o", "mode=0755,nosuid,nodev", "tmpfs", p.T("run")),
		sys.Command("cp", "-L", "/etc/resolv.conf", p.T("etc/resolv.conf")),
	}
	for _, c := range cmds {
		if err := x.R.Run(ctx, c); err != nil {
			return err
		}
	}
	x.chrootReady = true
	return nil
}

func installPackages(ctx context.Context, x *Exec) error {
	p := x.Plan
	args := append([]string{"-S", "-y", "-r", p.Paths.Target}, p.Repositories()...)
	if err := x.R.Run(ctx, sys.Command("xbps-install", append(args, p.Packages...)...)); err != nil {
		return err
	}
	return x.run(ctx, "xbps-reconfigure", "-r", p.Paths.Target, "-f", "base-files")
}

// cleanupLive removes what only makes sense in the live session and restores
// the files the live ISO modified.
func cleanupLive(ctx context.Context, x *Exec) error {
	p := x.Plan
	liveUser := "anon"
	if conf, err := x.R.ReadFile(p.Paths.LiveConf); err == nil {
		for _, line := range strings.Split(string(conf), "\n") {
			if v, ok := strings.CutPrefix(line, "USERNAME="); ok && v != "" {
				liveUser = strings.Trim(v, `"'`)
			}
		}
	}
	if err := x.chroot(ctx, "userdel", "-r", liveUser); err != nil {
		return err
	}
	for _, f := range []string{
		"etc/motd", "etc/issue", "etc/default/live.conf",
		"etc/sudoers.d/99-void-live", "etc/sudoers.d/zz-voidbleed-live",
		"etc/polkit-1/rules.d/void-live.rules",
		"etc/runit/core-services/90-voidbleed-live.sh",
		"etc/sv/greetd/down",
	} {
		if err := x.R.RemoveAll(p.T(f)); err != nil {
			return err
		}
	}
	// Every install gets its own D-Bus machine id and entropy seed.
	for _, f := range []string{"var/lib/dbus/machine-id", "var/lib/seedrng"} {
		if err := x.R.RemoveAll(p.T(f)); err != nil {
			return err
		}
	}
	if err := x.chroot(ctx, "dbus-uuidgen", "--ensure"); err != nil {
		return err
	}
	// Undo the live ISO's edits using what the packages ship: greetd's
	// autologin config and the live user's home baked into the Qt skeleton.
	if err := x.run(ctx, "cp", p.T("usr/share/voidbleed/greetd/config.toml"), p.T("etc/greetd/config.toml")); err != nil {
		return err
	}
	if err := x.run(ctx, "sed", "-i", "s|/home/"+liveUser+"|@HOME@|g", p.T("etc/skel/.config/qt6ct/qt6ct.conf")); err != nil {
		return err
	}
	return x.run(ctx, "sed", "-i", `s/^GETTY_ARGS="--noclear -a [^"]*"/GETTY_ARGS="--noclear"/`, p.T("etc/sv/agetty-tty1/conf"))
}

// adjustPackages removes optional software baked into the live image that
// wasn't selected, then installs selected software the image doesn't carry.
func adjustPackages(ctx context.Context, x *Exec) error {
	p := x.Plan
	t := p.Paths.Target
	if raw, err := x.R.ReadFile(p.Paths.LiveGroups); err == nil {
		keep := map[string]bool{}
		for _, pkg := range p.Packages {
			keep[pkg] = true
		}
		var remove []string
		for _, line := range strings.Split(string(raw), "\n") {
			id := strings.TrimSpace(line)
			if id == "" || strings.HasPrefix(id, "#") || slices.Contains(p.Groups, id) {
				continue
			}
			g, ok := p.Catalog.Group(id)
			if !ok {
				continue
			}
			for _, pkg := range g.Packages {
				if !keep[pkg] && !slices.Contains(remove, pkg) {
					remove = append(remove, pkg)
				}
			}
		}
		for _, pkg := range remove {
			// Skip packages something else still needs.
			if out, _ := x.R.Output(ctx, sys.Command("xbps-query", "-r", t, "-X", pkg)); strings.TrimSpace(out) != "" {
				continue
			}
			if err := x.run(ctx, "xbps-remove", "-r", t, "-R", "-y", pkg); err != nil {
				return err
			}
		}
	}
	if len(x.missing) == 0 {
		return nil
	}
	args := append([]string{"-S", "-y", "-r", t}, p.Repositories()...)
	return x.R.Run(ctx, sys.Command("xbps-install", append(args, x.missing...)...))
}

func (x *Exec) uuid(ctx context.Context, dev string) (string, error) {
	if u, ok := x.uuids[dev]; ok {
		return u, nil
	}
	out, err := x.R.Output(ctx, sys.Command("blkid", "-s", "UUID", "-o", "value", dev))
	if err != nil {
		return "", err
	}
	u := strings.TrimSpace(out)
	if u == "" {
		return "", fmt.Errorf("no filesystem UUID on %s", dev)
	}
	if x.uuids == nil {
		x.uuids = map[string]string{}
	}
	x.uuids[dev] = u
	return u, nil
}

func configureSystem(ctx context.Context, x *Exec) error {
	p := x.Plan
	s := p.Config.System
	write := func(rel, content string) error { return x.R.WriteFile(p.T(rel), []byte(content), 0o644) }

	if err := write("etc/hostname", s.Hostname+"\n"); err != nil {
		return err
	}
	if err := x.R.AppendFile(p.T("etc/hosts"), []byte(fmt.Sprintf("127.0.1.1\t%s.localdomain\t%s\n", s.Hostname, s.Hostname))); err != nil {
		return err
	}
	if err := write("etc/locale.conf", "LANG="+s.Locale+"\nLC_COLLATE=C\n"); err != nil {
		return err
	}
	if raw, err := x.R.ReadFile(p.T("etc/default/libc-locales")); err == nil {
		updated, found := enableLocale(string(raw), s.Locale)
		if !found {
			return fmt.Errorf("locale %s is not available", s.Locale)
		}
		if err := write("etc/default/libc-locales", updated); err != nil {
			return err
		}
	}
	if err := x.chroot(ctx, "xbps-reconfigure", "-f", "glibc-locales"); err != nil {
		return err
	}
	if err := x.R.Symlink("/usr/share/zoneinfo/"+s.Timezone, p.T("etc/localtime")); err != nil {
		return err
	}
	rc, _ := x.R.ReadFile(p.T("etc/rc.conf"))
	if err := write("etc/rc.conf", setShellVars(string(rc), [][2]string{{"KEYMAP", s.Keymap}, {"HARDWARECLOCK", "UTC"}})); err != nil {
		return err
	}

	devices := []string{p.RootDev, p.ESP}
	if p.SwapPart != "" {
		devices = append(devices, p.SwapPart)
	}
	for _, dev := range devices {
		if _, err := x.uuid(ctx, dev); err != nil {
			return err
		}
	}
	if err := write("etc/fstab", p.fstab(x.uuids)); err != nil {
		return err
	}
	if p.Config.Disk.Encrypt {
		u, err := x.uuid(ctx, p.RootPart)
		if err != nil {
			return err
		}
		x.luksUUID = u
		if err := x.R.WriteFile(p.T("etc/crypttab"), []byte(crypttab(u)), 0o600); err != nil {
			return err
		}
	}
	return write("etc/dracut.conf.d/voidbleed.conf", p.dracutConf())
}

func setupSwap(ctx context.Context, x *Exec) error {
	p := x.Plan
	if p.SwapFile == "" {
		return nil
	}
	size := strconv.Itoa(p.Config.Swap.SizeMiB)
	file := p.T(p.SwapFile)
	if p.Config.Disk.Filesystem == config.Btrfs {
		return x.run(ctx, "btrfs", "filesystem", "mkswapfile", "--size", size+"m", file)
	}
	for _, c := range []sys.Cmd{
		sys.Command("fallocate", "-l", size+"M", file),
		sys.Command("chmod", "600", file),
		sys.Command("mkswap", file),
	} {
		if err := x.R.Run(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func createUsers(ctx context.Context, x *Exec) error {
	p := x.Plan
	u := p.Config.User
	t := p.Paths.Target

	if u.RootPassword != "" {
		if err := x.R.Run(ctx, sys.Command("chpasswd", "-c", "SHA512").WithStdin("root:"+u.RootPassword+"\n", true).InChroot(t)); err != nil {
			return err
		}
	} else if err := x.chroot(ctx, "passwd", "-l", "root"); err != nil {
		return err
	}

	var groups []string
	if raw, err := x.R.ReadFile(p.T("etc/group")); err == nil {
		existing := map[string]bool{}
		for _, line := range strings.Split(string(raw), "\n") {
			if name, _, ok := strings.Cut(line, ":"); ok {
				existing[name] = true
			}
		}
		for _, g := range userGroups {
			if existing[g] {
				groups = append(groups, g)
			}
		}
	} else {
		groups = []string{"wheel", "audio", "video", "input"}
	}
	args := []string{"-m", "-G", strings.Join(groups, ",")}
	if u.FullName != "" {
		args = append(args, "-c", u.FullName)
	}
	if err := x.chroot(ctx, "useradd", append(args, u.Name)...); err != nil {
		return err
	}
	if err := x.R.Run(ctx, sys.Command("chpasswd", "-c", "SHA512").WithStdin(u.Name+":"+u.Password+"\n", true).InChroot(t)); err != nil {
		return err
	}

	home := "/home/" + u.Name
	if err := x.run(ctx, "sed", "-i", "s|@HOME@|"+home+"|g", p.T(home, ".config/qt6ct/qt6ct.conf")); err != nil {
		return err
	}
	if err := x.R.WriteFile(p.T(home, ".config/niri/local.kdl"), []byte(p.niriLocal()), 0o644); err != nil {
		return err
	}
	if err := x.chroot(ctx, "chown", "-R", u.Name+":"+u.Name, home+"/.config"); err != nil {
		return err
	}
	if err := x.chroot(ctx, "runuser", "-u", u.Name, "--", "env", "HOME="+home, "xdg-user-dirs-update"); err != nil {
		return err
	}

	greeter := p.T("var/lib/noctalia-greeter/greeter.toml")
	existing, _ := x.R.ReadFile(greeter)
	if !strings.Contains(string(existing), "[keyboard]") {
		if err := x.R.AppendFile(greeter, []byte(p.greeterKeyboard())); err != nil {
			return err
		}
		return x.chroot(ctx, "chown", "_greeter:_greeter", "/var/lib/noctalia-greeter/greeter.toml")
	}
	return nil
}

func enableServices(ctx context.Context, x *Exec) error {
	p := x.Plan
	dir := p.T("etc/runit/runsvdir/default")
	for _, svc := range p.ManagedServices {
		if !slices.Contains(p.Services, svc) {
			if err := x.R.RemoveAll(path.Join(dir, svc)); err != nil {
				return err
			}
		}
	}
	for _, svc := range p.Services {
		if !x.R.Exists(p.T("etc/sv", svc)) {
			return fmt.Errorf("service %s is not installed", svc)
		}
		if err := x.R.Symlink("/etc/sv/"+svc, path.Join(dir, svc)); err != nil {
			return err
		}
	}
	return nil
}

func setupFlatpak(ctx context.Context, x *Exec) error {
	p := x.Plan
	if !x.Online {
		// Add Flathub and install the apps once the machine is online.
		pending := strings.Join(p.Selection.Flatpaks, "\n")
		if pending != "" {
			pending += "\n"
		}
		if err := x.R.WriteFile(p.T(FirstbootFlatpaks), []byte(pending), 0o644); err != nil {
			return err
		}
		return x.R.Symlink("/etc/sv/voidbleed-firstboot", p.T("etc/runit/runsvdir/default/voidbleed-firstboot"))
	}
	if err := x.chroot(ctx, "flatpak", "remote-add", "--system", "--if-not-exists", "flathub", FlathubRepo); err != nil {
		return err
	}
	if len(p.Selection.Flatpaks) == 0 {
		return nil
	}
	args := append([]string{"install", "--system", "--noninteractive", "-y", "flathub"}, p.Selection.Flatpaks...)
	return x.chroot(ctx, "flatpak", args...)
}

func bootloader(ctx context.Context, x *Exec) error {
	p := x.Plan
	grub, _ := x.R.ReadFile(p.T("etc/default/grub"))
	updated := setShellVars(string(grub), [][2]string{
		{"GRUB_DISTRIBUTOR", "Voidbleed"},
		{"GRUB_CMDLINE_LINUX_DEFAULT", p.grubCmdline(x.luksUUID)},
	})
	if err := x.R.WriteFile(p.T("etc/default/grub"), []byte(updated), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"grub-install", "--target=x86_64-efi", "--efi-directory=/boot", "--bootloader-id=Voidbleed", "--recheck"},
		// Also install to the fallback path for firmware that loses NVRAM entries.
		{"grub-install", "--target=x86_64-efi", "--efi-directory=/boot", "--removable", "--recheck"},
		// Rebuilds the initramfs (dracut) for the pinned kernel.
		{"xbps-reconfigure", "-f", p.Kernel},
		{"grub-mkconfig", "-o", "/boot/grub/grub.cfg"},
	} {
		if err := x.chroot(ctx, args[0], args[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func finish(ctx context.Context, x *Exec) error {
	p := x.Plan
	if p.Paths.Log != "" && x.R.Exists(p.Paths.Log) {
		if err := x.run(ctx, "install", "-Dm600", p.Paths.Log, p.T("var/log/voidbleed-install.log")); err != nil {
			return err
		}
	}
	if err := x.run(ctx, "sync"); err != nil {
		return err
	}
	return x.Cleanup(ctx)
}

// Cleanup unmounts the target and closes the encrypted volume. It is safe to
// call after a failure at any point and more than once.
func (x *Exec) Cleanup(ctx context.Context) error {
	var errs []error
	if x.mounted || x.chrootReady {
		if err := x.run(ctx, "umount", "-R", x.Plan.Paths.Target); err != nil {
			errs = append(errs, err)
		} else {
			x.mounted, x.chrootReady = false, false
		}
	}
	if x.opened {
		if err := x.run(ctx, "cryptsetup", "close", MapperName); err != nil {
			errs = append(errs, err)
		} else {
			x.opened = false
		}
	}
	return errors.Join(errs...)
}
