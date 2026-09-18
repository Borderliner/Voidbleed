package system

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"voidbleed/internal/sys"
)

// SnapshotDir is where the installer mounts the @snapshots subvolume.
const SnapshotDir = "/.snapshots"

// Snapshot is one btrfs snapshot of the root subvolume.
type Snapshot struct {
	Name string
	Path string
	Made time.Time
}

// SnapshotsAvailable reports whether this machine can take them at all: a
// btrfs root, and the tools to work with it. A machine on ext4 is told nothing
// about snapshots, because there is nothing it could do with them.
func SnapshotsAvailable() bool {
	return RootFilesystem() == "btrfs" && Have("btrfs")
}

// Snapshots lists what is in the snapshot directory, newest first. Reading the
// directory needs no privileges; making and deleting them does.
func (c *Client) Snapshots(ctx context.Context) ([]Snapshot, error) {
	entries, err := os.ReadDir(SnapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // btrfs root without the subvolume mounted
		}
		return nil, err
	}
	var snaps []Snapshot
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s := Snapshot{Name: e.Name(), Path: filepath.Join(SnapshotDir, e.Name())}
		if info, err := e.Info(); err == nil {
			s.Made = info.ModTime()
		}
		snaps = append(snaps, s)
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Made.After(snaps[j].Made) })
	return snaps, nil
}

// SnapshotName is what a snapshot taken now is called: the time it was made,
// and a label when one was given.
func SnapshotName(label string) string {
	name := time.Now().Format("2006-01-02-1504")
	label = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r == ' ':
			return '-'
		}
		return -1
	}, strings.TrimSpace(label))
	if label != "" {
		name += "-" + label
	}
	return name
}

// SnapshotCreateCmd takes a read-only snapshot of the running root. Read-only
// is the point: a snapshot that can be written to is not a record of anything.
func SnapshotCreateCmd(name string) sys.Cmd {
	return sys.Command("btrfs", "subvolume", "snapshot", "-r", "/", filepath.Join(SnapshotDir, name))
}

func SnapshotDeleteCmd(name string) sys.Cmd {
	return sys.Command("btrfs", "subvolume", "delete", filepath.Join(SnapshotDir, name))
}

// SnapshotRestoreHint is what to do with a snapshot once you have one. A
// running root cannot replace itself, so this program does not pretend it can:
// it says where the files are and how a full rollback is actually done.
const SnapshotRestoreHint = `A snapshot is a read-only copy of the root subvolume, kept on the same disk.

To get a file back, copy it out of the snapshot directory — it is an ordinary directory tree.

To roll the whole system back, the root subvolume has to be swapped while it is not mounted: boot the Voidbleed ISO, mount the top of the filesystem (mount -o subvolid=5 /dev/… /mnt), move @ aside, snapshot the one you want back into place as @, and reboot.

Snapshots share space with the filesystem they came from, and they are not a backup: a disk that fails takes them with it.`
