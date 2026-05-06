package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jeffwang/qdesk/internal/macserver"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		os.Exit(runDoctor())
	}

	helperPath := flag.String("helper",
		envOr("QDESK_MAC_HELPER", defaultHelperPath()),
		"path to qdesk-mac-helper binary")
	listen := flag.String("listen",
		os.Getenv("QDESK_MAC_LISTEN"),
		"HTTP listen address (e.g. 127.0.0.1:8765). If empty, run in stdio MCP mode.")
	apiKey := flag.String("api-key",
		os.Getenv("QDESK_MAC_API_KEY"),
		"shared bearer token for HTTP mode (env QDESK_MAC_API_KEY). Required when --listen is set; HTTP mode refuses to start with an empty key.")
	noCaffeinate := flag.Bool("no-caffeinate", false,
		"HTTP mode only: do NOT spawn `caffeinate -di` to keep the Mac awake while serving")
	trustedCIDR := flag.String("trusted-cidr",
		os.Getenv("QDESK_MAC_TRUSTED_CIDR"),
		"HTTP mode only: comma-separated CIDR allowlist. Connections from outside these ranges get 403 even with a valid bearer key. Recommended for Tailscale: 100.64.0.0/10. Empty = no IP filter.")
	trustTSHeaders := flag.Bool("trust-tailscale-headers", false,
		"HTTP mode only: trust Tailscale-User-Login / Tailscale-User-Name request headers (set by `tailscale serve`). Logs the identity of every authenticated request. Set ONLY when you front qdesk-mac with `tailscale serve` — otherwise an attacker can spoof.")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	sup, err := macserver.Spawn(ctx, *helperPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "qdesk-mac: spawn helper failed: %v\n", err)
		os.Exit(1)
	}
	defer sup.Close()

	srv := macserver.NewMCPServer(sup)

	if *listen != "" {
		cfg := httpConfig{
			Listen:               *listen,
			APIKey:               *apiKey,
			Caffeinate:           !*noCaffeinate,
			TrustedCIDR:          *trustedCIDR,
			TrustTailscaleHeader: *trustTSHeaders,
		}
		logf("qdesk-mac starting in HTTP mode; helper=%s listen=%s api_key=%v trusted_cidr=%q ts_headers=%v",
			*helperPath, cfg.Listen, cfg.APIKey != "", cfg.TrustedCIDR, cfg.TrustTailscaleHeader)
		if err := runHTTP(ctx, srv, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "qdesk-mac: %v\n", err)
			os.Exit(1)
		}
		return
	}

	logf("qdesk-mac starting in stdio MCP mode; helper=%s", *helperPath)
	runStdio(ctx, srv)
}

// runStdio implements the original MCP stdio loop: one JSON-RPC request per
// line on stdin, one response per line on stdout. MCP convention requires
// stdout to carry ONLY framed responses; logs go to stderr.
func runStdio(ctx context.Context, srv *macserver.MCPServer) {
	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := in.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, os.ErrClosed) || err.Error() == "EOF" {
				return
			}
			logf("read: %v", err)
			return
		}
		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			continue
		}
		var req macserver.RPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			logf("invalid JSON-RPC: %v", err)
			continue
		}
		if len(req.ID) == 0 {
			continue // notifications: ignore
		}
		resp := srv.Handle(ctx, &req)
		b, _ := json.Marshal(resp)
		b = append(b, '\n')
		if _, err := out.Write(b); err != nil {
			logf("write: %v", err)
			return
		}
		_ = out.Flush()
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func defaultHelperPath() string {
	exe, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(exe), "qdesk-mac-helper")
	}
	return "/usr/local/bin/qdesk-mac-helper"
}

func logf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "qdesk-mac: "+fmt.Sprintf(format, args...))
}

// runDoctor is implemented in doctor.go.
func runDoctor() int { return runDoctorReal() }
