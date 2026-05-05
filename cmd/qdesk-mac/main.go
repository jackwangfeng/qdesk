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
	flag.Parse()

	logf("qdesk-mac starting; helper=%s", *helperPath)

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

// runDoctor is implemented in doctor.go (Task 13).
func runDoctor() int { return runDoctorReal() }
