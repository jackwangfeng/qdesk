//go:build windows

package winnative

import (
	"context"

	"github.com/jeffwang/qdesk/internal/winserver"
)

// Native is the real winserver.Native implementation. Returned by New.
type Native struct{}

func New() *Native { return &Native{} }

func (Native) FrontApp(_ context.Context) (winserver.FrontApp, error) {
	return frontApp()
}
func (Native) Activate(_ context.Context, r winserver.ActivateReq) (winserver.ActivateResp, error) {
	return activate(r)
}
func (Native) Screenshot(_ context.Context) (winserver.Screenshot, error) {
	return screenshot()
}
func (Native) Click(_ context.Context, r winserver.ClickReq) error {
	return click(r)
}
func (Native) Type(_ context.Context, t string) error {
	return typeText(t)
}
func (Native) Key(_ context.Context, c string) error {
	return keyCombo(c)
}
func (Native) Scroll(_ context.Context, r winserver.ScrollReq) error {
	return scroll(r)
}
func (Native) ClipboardPaste(_ context.Context, t string) (winserver.ClipboardResp, error) {
	return clipboardPaste(t)
}
