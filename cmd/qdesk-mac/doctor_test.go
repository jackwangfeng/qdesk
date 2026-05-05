package main

import (
	"strings"
	"testing"
)

func TestFormatHealthIncludesPermissionPanelHints(t *testing.T) {
	out := formatHealthReport(false, false)
	if !strings.Contains(out, "Screen Recording") {
		t.Errorf("missing Screen Recording mention: %s", out)
	}
	if !strings.Contains(out, "Accessibility") {
		t.Errorf("missing Accessibility mention: %s", out)
	}
	if !strings.Contains(out, "x-apple.systempreferences:") {
		t.Errorf("missing settings panel URL: %s", out)
	}
}

func TestFormatHealthAllGreen(t *testing.T) {
	out := formatHealthReport(true, true)
	if !strings.Contains(out, "All permissions granted") {
		t.Errorf("expected green message; got %s", out)
	}
}
