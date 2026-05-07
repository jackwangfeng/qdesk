package winserver

import (
	"context"
	"fmt"
	"strings"
)

// requireForeground returns nil when expectedExe is empty (no check)
// or when the foreground exe basename matches expectedExe (case
// insensitive). Otherwise returns a structured error naming what's
// actually in front so the LLM can decide whether to call activate.
func requireForeground(ctx context.Context, n Native, expectedExe string) error {
	if expectedExe == "" {
		return nil
	}
	fa, err := n.FrontApp(ctx)
	if err != nil {
		return fmt.Errorf("frontApp: %w", err)
	}
	if !strings.EqualFold(fa.Exe, expectedExe) {
		return fmt.Errorf("foreground-mismatch: front exe is %q (title %q), expected %q; call windows.activate first",
			fa.Exe, fa.Title, expectedExe)
	}
	return nil
}
