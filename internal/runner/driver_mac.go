package runner

import (
	"context"
	"errors"
	"os"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// MacDriver drives the user's local macOS via qdesk-mac --listen.
// One MacDriver per Run; sets up by calling mac.activate on the
// configured bundle_id, then translates each protocol.Action into the
// matching mac.* MCP tool call (with target_bundle_id as guard).
type MacDriver struct {
	endpoint string
	apiKey   string
	bundleID string
}

// newMacDriver constructs a MacDriver from spec + Options. Resolves the
// API key from the env var named in spec.Mac.APIKeyEnv at construction
// time; opts.MacEndpoint (if set) overrides spec.Mac.Endpoint so CI can
// inject a per-runner URL.
func newMacDriver(spec *TestSpec, opts Options) (Driver, error) {
	if spec.Mac == nil {
		return nil, errors.New("mac-host target requires a mac: block")
	}
	endpoint := spec.Mac.Endpoint
	if opts.MacEndpoint != "" {
		endpoint = opts.MacEndpoint
	}
	key := os.Getenv(spec.Mac.APIKeyEnv)
	if key == "" {
		return nil, errors.New("env var " + spec.Mac.APIKeyEnv + " is empty")
	}
	return &MacDriver{
		endpoint: endpoint,
		apiKey:   key,
		bundleID: spec.Mac.BundleID,
	}, nil
}

func (d *MacDriver) Setup(ctx context.Context, spec *TestSpec) (DriverSession, error) {
	return nil, errors.New("MacDriver.Setup: not implemented yet")
}

// macSession is the in-flight DriverSession for MacDriver.
type macSession struct {
	endpoint string
	apiKey   string
	bundleID string
}

func (s *macSession) Screenshot(ctx context.Context) ([]byte, error) {
	return nil, errors.New("macSession.Screenshot: not implemented yet")
}

func (s *macSession) Action(ctx context.Context, a *protocol.Action) error {
	return errors.New("macSession.Action: not implemented yet")
}

func (s *macSession) Close(ctx context.Context) error {
	return nil
}
