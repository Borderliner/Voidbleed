// Package sys is the installer's only way to change the machine. Every
// command and file write goes through a Runner, so the same install steps can
// run for real or be recorded by DryRun for previews and golden tests.
package sys

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Cmd is one process invocation.
type Cmd struct {
	Name string
	Args []string
	// Stdin is fed to the process. Mark Secret when it holds a password so it
	// never reaches logs or transcripts.
	Stdin  string
	Secret bool
	// Chroot runs the command inside this directory via chroot(8).
	Chroot string
}

func Command(name string, args ...string) Cmd { return Cmd{Name: name, Args: args} }

// InChroot returns a copy of c that runs inside dir.
func (c Cmd) InChroot(dir string) Cmd { c.Chroot = dir; return c }

// WithStdin returns a copy of c that is fed stdin.
func (c Cmd) WithStdin(stdin string, secret bool) Cmd {
	c.Stdin, c.Secret = stdin, secret
	return c
}

// Shell runs script with sh -c, for pipelines such as tar | tar.
func Shell(script string) Cmd { return Command("sh", "-c", script) }

func (c Cmd) String() string {
	var b strings.Builder
	if c.Chroot != "" {
		fmt.Fprintf(&b, "chroot %s ", c.Chroot)
	}
	b.WriteString(c.Name)
	for _, a := range c.Args {
		b.WriteByte(' ')
		b.WriteString(quote(a))
	}
	return b.String()
}

func quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"$`\\|&;<>()*?[]{}!#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type Runner interface {
	// Run executes cmd; its combined output is streamed line by line to the log.
	Run(ctx context.Context, cmd Cmd) error
	// Output executes cmd and returns stdout.
	Output(ctx context.Context, cmd Cmd) (string, error)
	WriteFile(path string, data []byte, perm fs.FileMode) error
	AppendFile(path string, data []byte) error
	ReadFile(path string) ([]byte, error)
	MkdirAll(path string, perm fs.FileMode) error
	Symlink(target, link string) error
	RemoveAll(path string) error
	Exists(path string) bool
	Glob(pattern string) ([]string, error)
}

// Real runs everything on the machine. Log receives every command and output
// line; it must be safe to call from multiple goroutines.
type Real struct {
	Log func(line string)
}

func (r Real) log(line string) {
	if r.Log != nil {
		r.Log(line)
	}
}

func (r Real) command(ctx context.Context, cmd Cmd) *exec.Cmd {
	name, args := cmd.Name, cmd.Args
	if cmd.Chroot != "" {
		name, args = "chroot", append([]string{cmd.Chroot, cmd.Name}, cmd.Args...)
	}
	c := exec.CommandContext(ctx, name, args...)
	// A clean, predictable environment inside chroots and for xbps.
	c.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "HOME=/root", "TERM=dumb"}
	if cmd.Stdin != "" {
		c.Stdin = strings.NewReader(cmd.Stdin)
	}
	return c
}

func (r Real) Run(ctx context.Context, cmd Cmd) error {
	r.log("$ " + cmd.String())
	c := r.command(ctx, cmd)
	pipe, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	c.Stderr = c.Stdout
	var tail tailBuffer
	if err := c.Start(); err != nil {
		return fmt.Errorf("%s: %w", cmd.Name, err)
	}
	s := bufio.NewScanner(pipe)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Text()
		tail.add(line)
		r.log(line)
	}
	if err := c.Wait(); err != nil {
		if msg := tail.String(); msg != "" {
			return fmt.Errorf("%s failed: %w\n%s", cmd.Name, err, msg)
		}
		return fmt.Errorf("%s failed: %w", cmd.Name, err)
	}
	return nil
}

func (r Real) Output(ctx context.Context, cmd Cmd) (string, error) {
	c := r.command(ctx, cmd)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return string(out), fmt.Errorf("%s failed: %w: %s", cmd.String(), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func (r Real) WriteFile(path string, data []byte, perm fs.FileMode) error {
	r.log("write " + path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

func (r Real) AppendFile(path string, data []byte) error {
	r.log("append " + path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (Real) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (Real) MkdirAll(path string, perm fs.FileMode) error { return os.MkdirAll(path, perm) }

func (r Real) Symlink(target, link string) error {
	r.log("link " + link + " -> " + target)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	_ = os.Remove(link)
	return os.Symlink(target, link)
}

func (r Real) RemoveAll(path string) error {
	r.log("remove " + path)
	return os.RemoveAll(path)
}

func (Real) Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (Real) Glob(pattern string) ([]string, error) { return filepath.Glob(pattern) }

// tailBuffer keeps the last lines of a command's output for error messages.
type tailBuffer struct{ lines []string }

func (t *tailBuffer) add(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > 12 {
		t.lines = t.lines[1:]
	}
}

func (t *tailBuffer) String() string { return strings.Join(t.lines, "\n") }

// DryRun records what would happen without touching the machine. Outputs and
// Files provide canned answers for Output and ReadFile/Exists.
type DryRun struct {
	mu      sync.Mutex
	entries []string
	Outputs map[string]string // keyed by Cmd.String()
	Files   map[string]string
	// DefaultOutput answers Output calls without a canned response.
	DefaultOutput func(cmd Cmd) string
	// Fail, when set, makes Run and Output return its error for cmd.
	Fail func(cmd Cmd) error
}

func NewDryRun() *DryRun {
	return &DryRun{Outputs: map[string]string{}, Files: map[string]string{}}
}

func (d *DryRun) record(format string, a ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = append(d.entries, fmt.Sprintf(format, a...))
}

// Transcript is every recorded operation, one per line (multi-line file
// contents are indented).
func (d *DryRun) Transcript() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Join(d.entries, "\n") + "\n"
}

func (d *DryRun) Run(_ context.Context, cmd Cmd) error {
	line := "run   " + cmd.String()
	switch {
	case cmd.Stdin != "" && cmd.Secret:
		line += "  <<< [secret]"
	case cmd.Stdin != "":
		line += "  <<<\n" + indent(cmd.Stdin)
	}
	d.record("%s", line)
	if d.Fail != nil {
		return d.Fail(cmd)
	}
	return nil
}

func (d *DryRun) Output(_ context.Context, cmd Cmd) (string, error) {
	d.record("query %s", cmd.String())
	if d.Fail != nil {
		if err := d.Fail(cmd); err != nil {
			return "", err
		}
	}
	if out, ok := d.Outputs[cmd.String()]; ok {
		return out, nil
	}
	if d.DefaultOutput != nil {
		return d.DefaultOutput(cmd), nil
	}
	return "", nil
}

func (d *DryRun) WriteFile(path string, data []byte, perm fs.FileMode) error {
	d.record("write %s (%#o)\n%s", path, perm, indent(string(data)))
	d.mu.Lock()
	d.Files[path] = string(data)
	d.mu.Unlock()
	return nil
}

func (d *DryRun) AppendFile(path string, data []byte) error {
	d.record("append %s\n%s", path, indent(string(data)))
	d.mu.Lock()
	d.Files[path] += string(data)
	d.mu.Unlock()
	return nil
}

func (d *DryRun) ReadFile(path string) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if content, ok := d.Files[path]; ok {
		return []byte(content), nil
	}
	return nil, fmt.Errorf("dry run: %s: %w", path, fs.ErrNotExist)
}

func (d *DryRun) MkdirAll(path string, perm fs.FileMode) error {
	d.record("mkdir %s", path)
	return nil
}

func (d *DryRun) Symlink(target, link string) error {
	d.record("link  %s -> %s", link, target)
	return nil
}

func (d *DryRun) RemoveAll(path string) error {
	d.record("rm    %s", path)
	return nil
}

func (d *DryRun) Exists(path string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.Files[path]
	return ok
}

func (d *DryRun) Glob(pattern string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var matches []string
	for path := range d.Files {
		if ok, _ := filepath.Match(pattern, path); ok {
			matches = append(matches, path)
		}
	}
	return matches, nil
}

func indent(s string) string {
	s = strings.TrimRight(s, "\n")
	return "      | " + strings.ReplaceAll(s, "\n", "\n      | ")
}

// WriterLog returns a Real.Log function that writes lines to w; safe for
// concurrent use.
func WriterLog(w io.Writer) func(string) {
	var mu sync.Mutex
	return func(line string) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintln(w, line)
	}
}
