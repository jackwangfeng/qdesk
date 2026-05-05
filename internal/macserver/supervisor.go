// Package macserver implements the Go side of qdesk-mac: spawning the Swift
// helper, marshalling JSON-RPC requests over stdio, and exposing the MCP
// tool surface to callers.
package macserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// Supervisor owns the helper child process and the JSON-RPC framing.
// All public methods are safe for concurrent use; Call serialises requests
// internally because the helper is single-threaded over stdio.
type Supervisor struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	mu       sync.Mutex // serialises Call
	nextID   atomic.Int64
	closed   atomic.Bool
	closeErr error
}

// Spawn starts the helper binary and returns a Supervisor wrapping it.
func Spawn(ctx context.Context, binary string) (*Supervisor, error) {
	cmd := exec.CommandContext(ctx, binary)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start helper: %w", err)
	}
	return &Supervisor{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: stderr,
	}, nil
}

// Call sends one request and waits for the response. Helper is single-threaded
// so this MUST be serialised.
func (s *Supervisor) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if s.closed.Load() {
		return nil, errors.New("supervisor closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id := int(s.nextID.Add(1))
	req := macproto.Request{ID: id, Method: method, Params: params}
	frame, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	frame = append(frame, '\n')

	// Default 30s deadline if caller didn't supply one.
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(30 * time.Second)
	}

	if _, err := s.stdin.Write(frame); err != nil {
		return nil, fmt.Errorf("write frame: %w", err)
	}

	// Read one response line.
	type readResult struct {
		line []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := s.stdout.ReadBytes('\n')
		ch <- readResult{line, err}
	}()
	var line []byte
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Until(deadline)):
		return nil, errors.New("helper response timeout")
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("read response: %w", r.err)
		}
		line = r.line
	}

	var resp macproto.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (line=%q)", err, string(line))
	}
	if resp.ID != id {
		return nil, fmt.Errorf("id mismatch: got=%d want=%d", resp.ID, id)
	}
	if resp.Error != nil {
		return nil, &HelperError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return resp.Result, nil
}

// Close terminates the helper. SIGTERM, then SIGKILL after 5 seconds.
func (s *Supervisor) Close() error {
	if s.closed.Swap(true) {
		return s.closeErr
	}
	_ = s.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		s.closeErr = <-done
	case err := <-done:
		s.closeErr = err
	}
	return s.closeErr
}

// HelperError is returned when the helper sends a structured error response.
type HelperError struct {
	Code    string
	Message string
}

func (e *HelperError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
