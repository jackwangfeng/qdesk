// qdesk-control is the multi-session control plane.
//
// It owns the SQLite session store, talks to Docker to start/stop sandbox
// containers, and proxies /screenshot and /actions to the right agentd.
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

	"github.com/jeffwang/qdesk/internal/control"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "listen address")
	dbPath := flag.String("db", "qdesk-control.db", "SQLite database path")
	image := flag.String("image", "qdesk/ubuntu-chrome:dev", "sandbox image")
	devKey := flag.String("dev-key", os.Getenv("QDESK_DEV_KEY"),
		"dev-mode shared bearer token (env QDESK_DEV_KEY); empty disables")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	store, err := control.OpenStore(*dbPath)
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	srv := control.NewServer(control.Config{
		DBPath:     *dbPath,
		Image:      *image,
		DevKey:     *devKey,
		DefaultTTL: 10 * time.Minute,
		MaxTTL:     60 * time.Minute,
		GCInterval: 30 * time.Second,
	}, store, control.DockerRuntime{})

	rootCtx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	stopGC := srv.StartGC(rootCtx)
	defer stopGC()

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("qdesk-control listening", "addr", *listen, "image", *image, "dev_key", *devKey != "")
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		slog.Error("listen failed", "err", err)
		os.Exit(1)
	case <-rootCtx.Done():
		slog.Info("shutting down")
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = httpSrv.Shutdown(shutdownCtx)
	}
}
