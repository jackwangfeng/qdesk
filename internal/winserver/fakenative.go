package winserver

import (
	"context"
	"errors"
)

// FakeNative is a programmable Native for tests. Each method
// delegates to the corresponding Fn field; nil Fn returns
// `errors.New("fake: <Method> not wired")`, which most tests should
// treat as a bug — a code path called a method tests forgot to wire up.
// (Grep that string back here when triaging a test failure.)
type FakeNative struct {
	FrontAppFn       func() (FrontApp, error)
	ActivateFn       func(ActivateReq) (ActivateResp, error)
	ScreenshotFn     func() (Screenshot, error)
	ClickFn          func(ClickReq) error
	TypeFn           func(string) error
	KeyFn            func(string) error
	ScrollFn         func(ScrollReq) error
	ClipboardPasteFn func(string) (ClipboardResp, error)
}

func (f *FakeNative) FrontApp(_ context.Context) (FrontApp, error) {
	if f.FrontAppFn == nil {
		return FrontApp{}, errors.New("fake: FrontApp not wired")
	}
	return f.FrontAppFn()
}
func (f *FakeNative) Activate(_ context.Context, r ActivateReq) (ActivateResp, error) {
	if f.ActivateFn == nil {
		return ActivateResp{}, errors.New("fake: Activate not wired")
	}
	return f.ActivateFn(r)
}
func (f *FakeNative) Screenshot(_ context.Context) (Screenshot, error) {
	if f.ScreenshotFn == nil {
		return Screenshot{}, errors.New("fake: Screenshot not wired")
	}
	return f.ScreenshotFn()
}
func (f *FakeNative) Click(_ context.Context, r ClickReq) error {
	if f.ClickFn == nil {
		return errors.New("fake: Click not wired")
	}
	return f.ClickFn(r)
}
func (f *FakeNative) Type(_ context.Context, t string) error {
	if f.TypeFn == nil {
		return errors.New("fake: Type not wired")
	}
	return f.TypeFn(t)
}
func (f *FakeNative) Key(_ context.Context, c string) error {
	if f.KeyFn == nil {
		return errors.New("fake: Key not wired")
	}
	return f.KeyFn(c)
}
func (f *FakeNative) Scroll(_ context.Context, r ScrollReq) error {
	if f.ScrollFn == nil {
		return errors.New("fake: Scroll not wired")
	}
	return f.ScrollFn(r)
}
func (f *FakeNative) ClipboardPaste(_ context.Context, t string) (ClipboardResp, error) {
	if f.ClipboardPasteFn == nil {
		return ClipboardResp{}, errors.New("fake: ClipboardPaste not wired")
	}
	return f.ClipboardPasteFn(t)
}
