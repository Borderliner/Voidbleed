package system

import (
	"context"
	"os"

	"voidbleed/internal/sys"
)

// Stream runs commands one after another, handing every output line to log as
// it arrives. It stops at the first failure, so a transaction that cannot
// start does not pretend the rest worked.
func (c *Client) Stream(ctx context.Context, log func(string), privileged bool, cmds ...sys.Cmd) error {
	runner := c.streamer(log)
	for _, cmd := range cmds {
		if privileged {
			cmd = c.priv(cmd)
		}
		if err := runner.Run(ctx, cmd); err != nil {
			return err
		}
	}
	return nil
}

// streamer builds the runner for one streamed action: the real one normally,
// and whatever was injected in tests and demo mode.
func (c *Client) streamer(log func(string)) sys.Runner {
	if _, real := c.Run.(sys.Real); !real {
		return c.Run
	}
	return sys.Real{Log: log, Env: os.Environ()}
}

// privOutput runs a read-only command that still needs root, such as asking
// runit for the state of a service.
func (c *Client) privOutput(ctx context.Context, name string, args ...string) (string, error) {
	return c.Run.Output(ctx, c.priv(sys.Command(name, args...)))
}
