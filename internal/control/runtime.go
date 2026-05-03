package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Runtime abstracts the container backend so we can swap Docker for
// Firecracker / Kata / etc. later without touching the HTTP layer.
type Runtime interface {
	Start(ctx context.Context, opts StartOpts) (*ContainerHandle, error)
	Stop(ctx context.Context, containerID string) error
	WaitReady(ctx context.Context, hostPort int) error
	OpenURL(ctx context.Context, containerID, url string) error
}

// StartOpts captures everything needed to launch a sandbox.
type StartOpts struct {
	Image      string
	Name       string // container name (must be unique on host)
	Resolution [2]int // [width, height]; zero defaults to 1920x1080
	OpenURL    string // optional Chromium target after start
}

// ContainerHandle is what Runtime.Start returns.
type ContainerHandle struct {
	ContainerID string
	HostPort    int // host port mapped to container :7878
}

// DockerRuntime spawns containers via the local `docker` CLI.
type DockerRuntime struct{}

// Start runs the image with bridge networking, host-gateway add-host (so the
// sandbox can reach the host's services), and a host-side port for /agentd.
func (DockerRuntime) Start(ctx context.Context, opts StartOpts) (*ContainerHandle, error) {
	if opts.Image == "" {
		return nil, fmt.Errorf("StartOpts.Image is required")
	}
	if opts.Name == "" {
		opts.Name = "qdesk-" + randHex(6)
	}
	res := opts.Resolution
	if res[0] == 0 || res[1] == 0 {
		res = [2]int{1920, 1080}
	}

	port, err := pickFreePort()
	if err != nil {
		return nil, fmt.Errorf("pick port: %w", err)
	}
	args := []string{
		"run", "-d", "--rm",
		"--name", opts.Name,
		"--add-host=host.docker.internal:host-gateway",
		"-p", fmt.Sprintf("%d:7878", port),
		"-e", fmt.Sprintf("RES=%dx%dx24", res[0], res[1]),
		opts.Image,
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return &ContainerHandle{
		ContainerID: strings.TrimSpace(string(out)),
		HostPort:    port,
	}, nil
}

// Stop removes the container (uses docker rm -f because containers were
// started with --rm).
func (DockerRuntime) Stop(ctx context.Context, containerID string) error {
	if containerID == "" {
		return nil
	}
	out, err := exec.CommandContext(ctx, "docker", "rm", "-f", containerID).CombinedOutput()
	if err != nil {
		// docker rm on missing container is non-fatal here.
		s := strings.TrimSpace(string(out))
		if strings.Contains(s, "No such container") {
			return nil
		}
		return fmt.Errorf("docker rm %s: %w: %s", containerID, err, s)
	}
	return nil
}

// WaitReady polls /health until the agentd inside answers or the context
// is cancelled.
func (DockerRuntime) WaitReady(ctx context.Context, hostPort int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", hostPort)
	client := &http.Client{Timeout: 1 * time.Second}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sandbox not ready after timeout (port %d)", hostPort)
		}
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// OpenURL launches Chromium inside the sandbox pointing at url.
//
// Uses `docker exec -d` so it returns immediately; Chromium runs detached
// inside the container.
func (DockerRuntime) OpenURL(ctx context.Context, containerID, url string) error {
	if url == "" {
		return nil
	}
	cmd := fmt.Sprintf(
		`DISPLAY=:99 chromium --no-sandbox --disable-gpu --disable-dev-shm-usage `+
			`--user-data-dir=/tmp/cr-%s --no-first-run --window-size=1920,1080 `+
			`--window-position=0,0 %q > /tmp/chromium.log 2>&1 &`,
		randHex(4), url,
	)
	out, err := exec.CommandContext(ctx, "docker", "exec", "-d", containerID,
		"bash", "-c", cmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("chromium launch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// pickFreePort asks the kernel for an available TCP port.
func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
