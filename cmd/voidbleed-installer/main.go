// voidbleed-installer installs Voidbleed. Without flags it starts the TUI;
// with --config it installs unattended from a TOML file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"voidbleed/internal/catalog"
	"voidbleed/internal/config"
	"voidbleed/internal/engine"
	"voidbleed/internal/hw"
	"voidbleed/internal/sys"
	"voidbleed/internal/tui"
)

func main() {
	var (
		configPath = flag.String("config", "", "install unattended from this TOML file")
		yes        = flag.Bool("yes", false, "with --config: really erase the disk and install")
		dryRun     = flag.Bool("dry-run", false, "with --config: print every command instead of running it")
		demo       = flag.Bool("demo", false, "run the TUI against fake hardware; nothing is changed")
		catalogDir = flag.String("catalog", catalog.DefaultDir, "catalog directory")
		logPath    = flag.String("log", engine.DefaultPaths().Log, "install log")
	)
	flag.Parse()

	if err := run(*configPath, *yes, *dryRun, *demo, *catalogDir, *logPath); err != nil {
		fmt.Fprintln(os.Stderr, "voidbleed-installer:", err)
		os.Exit(1)
	}
}

func run(configPath string, yes, dryRun, demo bool, catalogDir, logPath string) error {
	cat, err := catalog.Load(catalogDir)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	if configPath == "" {
		return tui.Run(tui.Options{Catalog: cat, Demo: demo, LogPath: logPath})
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	facts, err := hw.New().Detect()
	if err != nil {
		return err
	}
	paths := engine.DefaultPaths()
	paths.Log = logPath
	plan, err := engine.NewPlan(cfg, facts, cat, paths)
	if err != nil {
		return err
	}
	steps := engine.Steps(plan)

	if dryRun {
		d := sys.NewDryRun()
		// Keep going past machine checks so the whole sequence is visible.
		plan.Facts.UEFI, plan.Facts.UEFI64 = true, true
		if err := engine.Run(context.Background(), &engine.Exec{Plan: plan, R: d}, steps, nil); err != nil {
			fmt.Fprintln(os.Stderr, "(dry run stopped:", err, ")")
		}
		fmt.Print(d.Transcript())
		return nil
	}

	fmt.Println("Voidbleed will be installed with:")
	for _, line := range plan.Summary() {
		fmt.Println("  " + line)
	}
	if !yes {
		return errors.New("this erases " + cfg.Disk.Device + "; re-run with --yes to install")
	}
	if os.Geteuid() != 0 {
		return errors.New("must run as root")
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logf := sys.WriterLog(logFile)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	events := make(chan engine.Event, 16)
	done := make(chan error, 1)
	x := &engine.Exec{Plan: plan, R: sys.Real{Log: logf}}
	go func() {
		done <- engine.Run(ctx, x, steps, events)
		close(events)
	}()
	return report(os.Stdout, events, done, len(steps))
}

// report prints one line per finished step for unattended installs.
func report(w io.Writer, events <-chan engine.Event, done <-chan error, total int) error {
	start := time.Now()
	for e := range events {
		switch e.Kind {
		case engine.StepStarted:
			fmt.Fprintf(w, "[%2d/%d] %s…\n", e.Index+1, total, e.Step.Title)
		case engine.StepFailed:
			fmt.Fprintf(w, "        failed after %s\n", e.Elapsed.Round(time.Second))
		case engine.Finished:
			fmt.Fprintf(w, "Installed in %s. Reboot into Voidbleed.\n", time.Since(start).Round(time.Second))
		}
	}
	if err := <-done; err != nil {
		return fmt.Errorf("install failed: %s", strings.TrimSpace(err.Error()))
	}
	return nil
}
