package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.qdesk.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestParseLinuxChromeDefault(t *testing.T) {
	p := writeTemp(t, `
name: legacy
url: http://example.com
goal: visit example
expect: ["page loads"]
`)
	spec, err := ParseFile(p)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.Target != TargetLinuxChrome {
		t.Errorf("expected default target=%q, got %q", TargetLinuxChrome, spec.Target)
	}
	if spec.Template != "ubuntu-chrome" {
		t.Errorf("expected default template=ubuntu-chrome, got %q", spec.Template)
	}
}

func TestParseMacHostHappyPath(t *testing.T) {
	p := writeTemp(t, `
name: wechat reply
target: mac-host
mac:
  endpoint: http://127.0.0.1:8765
  api_key_env: QDESK_MAC_API_KEY
  bundle_id: com.tencent.xinWeChat
goal: send a message
expect: ["sent"]
`)
	spec, err := ParseFile(p)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.Target != TargetMacHost {
		t.Errorf("target=%q, want mac-host", spec.Target)
	}
	if spec.Mac == nil || spec.Mac.BundleID != "com.tencent.xinWeChat" {
		t.Errorf("mac block lost: %+v", spec.Mac)
	}
	if spec.Template != "" {
		t.Errorf("mac-host should not get default ubuntu-chrome template; got %q", spec.Template)
	}
}

func TestParseMacHostMissingMacBlock(t *testing.T) {
	p := writeTemp(t, `
name: bad
target: mac-host
goal: nope
expect: [x]
`)
	_, err := ParseFile(p)
	if err == nil {
		t.Fatalf("expected error for missing mac: block")
	}
	if !strings.Contains(err.Error(), "requires a 'mac:' block") {
		t.Errorf("error should mention required mac block; got %q", err.Error())
	}
}

func TestParseLinuxChromeRejectsMacBlock(t *testing.T) {
	p := writeTemp(t, `
name: bad
mac:
  endpoint: http://x
  api_key_env: K
  bundle_id: b
goal: nope
expect: [x]
`)
	_, err := ParseFile(p)
	if err == nil {
		t.Fatalf("expected error: mac block on linux-chrome target")
	}
	if !strings.Contains(err.Error(), "only valid when target") {
		t.Errorf("error should mention target mismatch; got %q", err.Error())
	}
}

func TestParseUnknownTarget(t *testing.T) {
	p := writeTemp(t, `
name: bad
target: solaris-host
goal: x
expect: [y]
`)
	_, err := ParseFile(p)
	if err == nil {
		t.Fatalf("expected error for unknown target")
	}
	if !strings.Contains(err.Error(), "unknown target") {
		t.Errorf("error should mention unknown target; got %q", err.Error())
	}
}

func TestParseMacHostRejectsURL(t *testing.T) {
	p := writeTemp(t, `
name: bad
target: mac-host
url: http://example.com
mac:
  endpoint: http://x
  api_key_env: K
  bundle_id: b
goal: x
expect: [y]
`)
	_, err := ParseFile(p)
	if err == nil {
		t.Fatalf("expected error: url not valid for mac-host")
	}
}
