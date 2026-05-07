package runner

import (
	"context"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// Driver abstracts "spin up a target environment, screenshot it, send
// actions to it, tear it down". One impl per target type:
//   - DockerDriver: drives a per-session container via qdesk-control.
//   - MacDriver:    drives the user's local macOS via qdesk-mac --listen.
//
// The Run loop calls Setup once per test, then operates on the returned
// DriverSession. Close is called on test exit (success or failure).
type Driver interface {
	Setup(ctx context.Context, spec *TestSpec) (DriverSession, error)
}

// DriverSession is one in-flight drive. All methods are safe to call
// after Setup and before Close.
type DriverSession interface {
	Screenshot(ctx context.Context) ([]byte, error)
	Action(ctx context.Context, a *protocol.Action) error
	Close(ctx context.Context) error
}

// pickDriver returns the Driver matching spec.Target, using opts for
// any target-specific configuration (control URL / api keys / endpoints).
func pickDriver(spec *TestSpec, opts Options) (Driver, error) {
	switch spec.Target {
	case "", TargetLinuxChrome:
		return &DockerDriver{ControlURL: opts.ControlURL, APIKey: opts.APIKey}, nil
	case TargetMacHost:
		return newMacDriver(spec, opts)
	default:
		return nil, &targetError{got: spec.Target}
	}
}

type targetError struct{ got string }

func (e *targetError) Error() string {
	return "unknown target " + e.got + " (valid: linux-chrome, mac-host)"
}
