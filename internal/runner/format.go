package runner

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// actionJSON renders a protocol.Action in compact form for the HTML report.
func actionJSON(a *protocol.Action) string {
	if a == nil {
		return ""
	}
	b, err := json.Marshal(a)
	if err != nil {
		return fmt.Sprintf("%+v", a)
	}
	return string(b)
}

// durationFmt formats the gap between two times in seconds.
func durationFmt(a, b time.Time) string {
	d := b.Sub(a)
	if d < 0 {
		return ""
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
