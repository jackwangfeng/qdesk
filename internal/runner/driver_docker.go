package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/jeffwang/qdesk/pkg/client"
	"github.com/jeffwang/qdesk/pkg/protocol"
)

// DockerDriver drives a fresh per-session container via qdesk-control.
// This is the original qdesk-run path, factored out from runner.Run so
// it sits behind the Driver interface.
type DockerDriver struct {
	ControlURL string
	APIKey     string
}

func (d *DockerDriver) Setup(ctx context.Context, spec *TestSpec) (DriverSession, error) {
	c := client.New(d.ControlURL, d.APIKey)
	sess, err := c.CreateSession(ctx, &client.CreateSessionRequest{
		Template:   spec.Template,
		TTLSeconds: spec.TTLSeconds,
		OpenURL:    spec.URL,
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	// Give Chromium a moment to render the initial page after OpenURL.
	if spec.URL != "" {
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			_ = c.DeleteSession(ctx, sess.ID)
			return nil, ctx.Err()
		}
	}
	return &dockerSession{c: c, sessionID: sess.ID}, nil
}

type dockerSession struct {
	c         *client.Client
	sessionID string
}

func (s *dockerSession) Screenshot(ctx context.Context) ([]byte, error) {
	return s.c.Screenshot(ctx, s.sessionID)
}

func (s *dockerSession) Action(ctx context.Context, a *protocol.Action) error {
	_, err := s.c.Action(ctx, s.sessionID, a)
	return err
}

func (s *dockerSession) Close(ctx context.Context) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = ctx // unused: we want a fresh timeout for tear-down even if ctx is cancelled
	return s.c.DeleteSession(stopCtx, s.sessionID)
}
