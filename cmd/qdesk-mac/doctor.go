package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/internal/macproto"
	"github.com/jeffwang/qdesk/internal/macserver"
)

const (
	srPanelURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	axPanelURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
)

// runDoctorReal probes the helper for permission status and prints a
// remediation report. Returns 0 if all permissions granted, 2 if any are
// missing (so scripts can branch), 1 on operational error.
func runDoctorReal() int {
	helperPath := defaultHelperPath()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sup, err := macserver.Spawn(ctx, helperPath)
	if err != nil {
		fmt.Println("ERROR: cannot spawn helper at", helperPath)
		fmt.Println("  ", err)
		fmt.Println("Make sure qdesk-mac-helper is installed alongside qdesk-mac.")
		return 1
	}
	defer sup.Close()

	raw, err := sup.Call(ctx, macproto.MethodHealth, json.RawMessage(`{}`))
	if err != nil {
		fmt.Println("ERROR: health RPC failed:", err)
		return 1
	}
	var h macproto.HealthResponse
	if err := json.Unmarshal(raw, &h); err != nil {
		fmt.Println("ERROR: cannot decode health:", err)
		return 1
	}
	report := formatHealthReport(h.ScreenRecordingGranted, h.AccessibilityGranted)
	fmt.Println(report)
	if !h.ScreenRecordingGranted {
		_ = exec.Command("open", srPanelURL).Run()
	}
	if !h.AccessibilityGranted {
		_ = exec.Command("open", axPanelURL).Run()
	}
	if h.ScreenRecordingGranted && h.AccessibilityGranted {
		return 0
	}
	return 2
}

func formatHealthReport(sr, ax bool) string {
	if sr && ax {
		return "All permissions granted. qdesk-mac is ready."
	}
	var b strings.Builder
	b.WriteString("qdesk-mac: missing permissions\n\n")
	b.WriteString("Helper binary path (grant TCC permissions to THIS path):\n")
	b.WriteString("  " + defaultHelperPath() + "\n\n")
	if !sr {
		b.WriteString("[ ] Screen Recording — needed to capture screenshots\n")
		b.WriteString("    Open: " + srPanelURL + "\n")
		b.WriteString("    Add the helper binary above to the list and enable it.\n\n")
	}
	if !ax {
		b.WriteString("[ ] Accessibility — needed for click/type/key and AX tree access\n")
		b.WriteString("    Open: " + axPanelURL + "\n")
		b.WriteString("    Add the helper binary above to the list and enable it.\n\n")
	}
	b.WriteString("After granting, run `qdesk-mac doctor` again.")
	return b.String()
}
