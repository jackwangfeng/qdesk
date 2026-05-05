// qdesk-mac is a Model Context Protocol (MCP) stdio server that lets an AI
// agent control the host macOS WeChat through generic input primitives plus
// a small set of Accessibility-API helpers.
//
// Wire protocol: JSON-RPC 2.0 over stdin/stdout (one JSON object per line).
// Spawns qdesk-mac-helper (Swift) as a child process for native macOS APIs.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		fmt.Fprintln(os.Stderr, "doctor: not yet implemented")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "qdesk-mac: not yet implemented")
	os.Exit(1)
}
