//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jeffwang/qdesk/internal/winnative"
	"github.com/jeffwang/qdesk/internal/winserver"
)

func main() {
	listen := flag.String("listen",
		envOr("QDESK_WIN_LISTEN", "127.0.0.1:8765"),
		"HTTP listen address (e.g. 0.0.0.0:8765)")
	apiKey := flag.String("api-key",
		os.Getenv("QDESK_WIN_API_KEY"),
		"shared bearer token (env QDESK_WIN_API_KEY). Required.")
	trustedCIDR := flag.String("trusted-cidr",
		os.Getenv("QDESK_WIN_TRUSTED_CIDR"),
		"comma-separated CIDR allowlist (e.g. 100.64.0.0/10 for Tailscale)")
	trustTSHeaders := flag.Bool("trust-tailscale-headers", false,
		"trust Tailscale-User-Login / Tailscale-User-Name headers (set ONLY behind `tailscale serve`)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv := winserver.NewMCPServer(winnative.New())
	cfg := httpConfig{
		Listen:               *listen,
		APIKey:               *apiKey,
		TrustedCIDR:          *trustedCIDR,
		TrustTailscaleHeader: *trustTSHeaders,
	}
	logf("qdesk-win starting; listen=%s api_key=%v trusted_cidr=%q ts_headers=%v",
		cfg.Listen, cfg.APIKey != "", cfg.TrustedCIDR, cfg.TrustTailscaleHeader)
	if err := runHTTP(ctx, srv, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "qdesk-win: %v\n", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func logf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "qdesk-win: "+fmt.Sprintf(format, args...))
}
