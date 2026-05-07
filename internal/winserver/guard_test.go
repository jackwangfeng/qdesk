package winserver

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGuardEmptyExeSkipsCheck(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		t.Fatalf("FrontApp should NOT be called when expected_exe is empty")
		return FrontApp{}, nil
	}}
	if err := requireForeground(context.Background(), n, ""); err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
}

func TestGuardMatchPasses(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		return FrontApp{Exe: "notepad.exe"}, nil
	}}
	if err := requireForeground(context.Background(), n, "notepad.exe"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestGuardCaseInsensitive(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		return FrontApp{Exe: "Notepad.EXE"}, nil
	}}
	if err := requireForeground(context.Background(), n, "notepad.exe"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestGuardMismatchFails(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		return FrontApp{Exe: "explorer.exe", Title: "File Explorer"}, nil
	}}
	err := requireForeground(context.Background(), n, "notepad.exe")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "explorer.exe") {
		t.Errorf("error should name actual front exe; got %q", err.Error())
	}
}

func TestGuardFrontAppErrorPropagates(t *testing.T) {
	want := errors.New("syscall failed")
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) { return FrontApp{}, want }}
	err := requireForeground(context.Background(), n, "notepad.exe")
	if err == nil || !strings.Contains(err.Error(), "syscall failed") {
		t.Fatalf("want wrapped syscall failed, got %v", err)
	}
}
