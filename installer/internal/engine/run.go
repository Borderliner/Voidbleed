package engine

import (
	"context"
	"fmt"
	"time"
)

type EventKind int

const (
	StepStarted EventKind = iota
	StepFinished
	StepFailed
	Finished
)

// Event reports install progress to the TUI or the CLI.
type Event struct {
	Kind     EventKind
	Index    int // step index
	Step     Step
	Err      error
	Elapsed  time.Duration
	Progress float64 // 0..1 of total step weight completed
}

// Run executes steps in order, sending events to events (which may be nil).
// On failure it unmounts and closes what the install opened, and returns the
// step's error.
func Run(ctx context.Context, x *Exec, steps []Step, events chan<- Event) error {
	total := 0
	for _, s := range steps {
		total += s.Weight
	}
	send := func(e Event) {
		if events != nil {
			events <- e
		}
	}
	done := 0
	for i, s := range steps {
		start := time.Now()
		send(Event{Kind: StepStarted, Index: i, Step: s, Progress: float64(done) / float64(total)})
		if err := s.Run(ctx, x); err != nil {
			err = fmt.Errorf("%s: %w", s.Title, err)
			if cleanupErr := x.Cleanup(context.WithoutCancel(ctx)); cleanupErr != nil {
				err = fmt.Errorf("%w\n(cleanup also failed: %v)", err, cleanupErr)
			}
			send(Event{Kind: StepFailed, Index: i, Step: s, Err: err, Elapsed: time.Since(start), Progress: float64(done) / float64(total)})
			return err
		}
		done += s.Weight
		send(Event{Kind: StepFinished, Index: i, Step: s, Elapsed: time.Since(start), Progress: float64(done) / float64(total)})
	}
	send(Event{Kind: Finished, Progress: 1})
	return nil
}
