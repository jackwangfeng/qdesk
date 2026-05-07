// qdesk is the operator CLI: it parses a .qdesk YAML test, drives the
// sandbox via qdesk-control, and writes an HTML report.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jeffwang/qdesk/internal/llm"
	"github.com/jeffwang/qdesk/internal/runner"
	"github.com/jeffwang/qdesk/pkg/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "run":
		os.Exit(cmdRun(args))
	case "version":
		fmt.Println("qdesk", version.String())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `qdesk — AI-driven UI testing

Usage:
  qdesk run <test.yaml> [flags]
  qdesk version

Flags for "run":
  --control       control plane URL (linux-chrome target; default: http://127.0.0.1:8080)
  --api-key       bearer token (linux-chrome); defaults to $QDESK_API_KEY or $QDESK_DEV_KEY
  --mac-endpoint  qdesk-mac --listen URL override (mac-host target)
  --llm           model: gemini-2.5-flash (default), gemini-2.5-pro
  --gemini-key    gemini api key; defaults to $GEMINI_API_KEY
  --out           output directory; default: ./qdesk-runs/<name>-<timestamp>

Examples:
  # Linux Docker (default):
  export GEMINI_API_KEY=...
  export QDESK_DEV_KEY=secret
  qdesk run tests/recompdaily-login.yaml

  # Mac host (test reads spec.mac.api_key_env, e.g. QDESK_MAC_API_KEY):
  export GEMINI_API_KEY=...
  export QDESK_MAC_API_KEY=...
  qdesk run tests/wechat-reply.qdesk.yaml

`)
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	control := fs.String("control", "http://127.0.0.1:8080", "control plane URL (linux-chrome target)")
	apiKey := fs.String("api-key", firstNonEmpty(os.Getenv("QDESK_API_KEY"), os.Getenv("QDESK_DEV_KEY")), "bearer token (linux-chrome target)")
	macEndpoint := fs.String("mac-endpoint", "", "qdesk-mac --listen URL override (mac-host target)")
	model := fs.String("llm", "gemini-2.5-flash", "LLM model")
	geminiKey := fs.String("gemini-key", os.Getenv("GEMINI_API_KEY"), "Gemini API key")
	outDir := fs.String("out", "", "output directory")
	maxIter := fs.Int("max-iter-step", 8, "max turns per step")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "missing test file")
		usage()
		return 2
	}
	testPath := fs.Arg(0)

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// 1. Parse test spec.
	spec, err := runner.ParseFile(testPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		return 1
	}
	if spec.Name == "" {
		spec.Name = strings.TrimSuffix(filepath.Base(testPath), filepath.Ext(testPath))
	}
	if *outDir == "" {
		ts := time.Now().Format("20060102-150405")
		*outDir = filepath.Join("qdesk-runs", fmt.Sprintf("%s-%s", safeName(spec.Name), ts))
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir out:", err)
		return 1
	}

	// 2. Build LLM agent.
	if *geminiKey == "" {
		fmt.Fprintln(os.Stderr, "GEMINI_API_KEY (or --gemini-key) is required")
		return 1
	}
	if !strings.HasPrefix(*model, "gemini") {
		fmt.Fprintf(os.Stderr, "only gemini models supported in v0.1; got %q\n", *model)
		return 1
	}
	agent := &llm.Gemini{
		APIKey: *geminiKey,
		Model:  *model,
	}
	if spec.LLM != "" {
		agent.Model = spec.LLM
	}

	// 3. Run.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("▶ qdesk run %s\n", testPath)
	fmt.Printf("  control: %s\n", *control)
	fmt.Printf("  llm:     %s\n", agent.Name())
	fmt.Printf("  out:     %s\n\n", *outDir)

	trace, err := runner.Run(ctx, spec, runner.Options{
		ControlURL:     *control,
		APIKey:         *apiKey,
		MacEndpoint:    *macEndpoint,
		Agent:          agent,
		OutDir:         *outDir,
		MaxIterPerStep: *maxIter,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "run error:", err)
	}
	if trace == nil {
		return 2
	}

	// 4. Print summary.
	fmt.Println()
	switch trace.Status {
	case runner.StatusPass:
		fmt.Printf("✅ PASS  (%d step(s), %s)\n",
			len(trace.Steps), trace.FinishedAt.Sub(trace.StartedAt).Round(time.Millisecond))
	case runner.StatusFail:
		fmt.Printf("❌ FAIL  (%d step(s), %s)\n",
			len(trace.Steps), trace.FinishedAt.Sub(trace.StartedAt).Round(time.Millisecond))
	default:
		fmt.Printf("⚠ ERROR  %s\n", trace.Diagnosis)
	}
	for _, v := range trace.Verifies {
		mark := "✓"
		if !v.Passed {
			mark = "✗"
		}
		fmt.Printf("  %s %s — %s\n", mark, v.Expectation, truncate(v.Evidence, 80))
	}
	if trace.Diagnosis != "" && trace.Status != runner.StatusPass {
		fmt.Printf("\nDiagnosis:\n  %s\n", indent(trace.Diagnosis, "  "))
	}
	abs, _ := filepath.Abs(filepath.Join(*outDir, "report.html"))
	fmt.Printf("\n📄 report: file://%s\n", abs)

	if trace.Status == runner.StatusPass {
		return 0
	}
	return 1
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "test"
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func indent(s, pad string) string {
	return strings.ReplaceAll(s, "\n", "\n"+pad)
}
