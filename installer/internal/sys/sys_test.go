package sys

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdString(t *testing.T) {
	c := Command("mkfs.ext4", "-F", "-L", "voidbleed", "/dev/vda2")
	if got := c.String(); got != "mkfs.ext4 -F -L voidbleed /dev/vda2" {
		t.Fatal(got)
	}
	c = Shell("tar -C / -cf - . | tar -xf - -C '/mnt/x'").InChroot("/mnt/t")
	want := `chroot /mnt/t sh -c 'tar -C / -cf - . | tar -xf - -C '\''/mnt/x'\'''`
	if got := c.String(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestDryRunRedactsSecrets(t *testing.T) {
	d := NewDryRun()
	ctx := context.Background()
	d.Run(ctx, Command("cryptsetup", "luksFormat", "/dev/vda2").WithStdin("hunter2hunter2", true))
	d.Run(ctx, Command("sfdisk", "/dev/vda").WithStdin("label: gpt\n", false))
	out := d.Transcript()
	if strings.Contains(out, "hunter2") || !strings.Contains(out, "[secret]") {
		t.Fatalf("secret leaked or not marked:\n%s", out)
	}
	if !strings.Contains(out, "| label: gpt") {
		t.Fatalf("non-secret stdin not recorded:\n%s", out)
	}
}

func TestDryRunFiles(t *testing.T) {
	d := NewDryRun()
	d.WriteFile("/mnt/etc/hostname", []byte("voidbleed\n"), 0o644)
	d.AppendFile("/mnt/etc/hostname", []byte("x\n"))
	b, err := d.ReadFile("/mnt/etc/hostname")
	if err != nil || string(b) != "voidbleed\nx\n" || !d.Exists("/mnt/etc/hostname") {
		t.Fatalf("got %q %v", b, err)
	}
	if _, err := d.ReadFile("/nope"); err == nil {
		t.Fatal("missing file read succeeded")
	}
}

func TestRealRunStreamsAndReportsFailure(t *testing.T) {
	var lines []string
	r := Real{Log: func(l string) { lines = append(lines, l) }}
	ctx := context.Background()
	if err := r.Run(ctx, Shell("echo one; echo two >&2")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "one\ntwo") {
		t.Fatalf("output not streamed: %v", lines)
	}
	err := r.Run(ctx, Shell("echo boom-detail; exit 3"))
	if err == nil || !strings.Contains(err.Error(), "boom-detail") {
		t.Fatalf("failure should carry output tail: %v", err)
	}
	out, err := r.Output(ctx, Command("sh", "-c", "printf hi").WithStdin("", false))
	if err != nil || out != "hi" {
		t.Fatalf("output: %q %v", out, err)
	}
}

func TestRealStdin(t *testing.T) {
	r := Real{}
	out, err := r.Output(context.Background(), Command("cat").WithStdin("piped", true))
	if err != nil || out != "piped" {
		t.Fatalf("got %q %v", out, err)
	}
}

func TestRealWriteFileSetsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "etc", "sudoers.d", "x")
	if err := (Real{}).WriteFile(path, []byte("x"), 0o440); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o440 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}
