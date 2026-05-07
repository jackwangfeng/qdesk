//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "qdesk-win is windows-only; build with GOOS=windows")
	os.Exit(1)
}
