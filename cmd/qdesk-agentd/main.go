// qdesk-agentd is the in-sandbox HTTP daemon. It exposes /health,
// /screenshot, and POST /actions over the configured listen address.
//
// Inside a qdesk sandbox container it runs as PID 1's child after Xvfb and
// the window manager have started.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jeffwang/qdesk/internal/agentd"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:7878", "listen address")
	display := flag.String("display", ":99", "X display (DISPLAY env)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	state := &agentd.AppState{
		Screen: &agentd.ScrotScreen{Display: *display},
		Input:  &agentd.XdotoolInput{Display: *display},
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           agentd.NewRouter(state),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("qdesk-agentd listening", "addr", *listen, "display", *display)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		slog.Error("listen failed", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = srv.Shutdown(shutdownCtx)
	}
}
