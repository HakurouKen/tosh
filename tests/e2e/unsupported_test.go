//go:build !darwin && !linux

package e2e

import "testing"

func TestPTYPlatform(t *testing.T) {
	t.Skip("PTY e2e 目前支持 macOS 和 Linux")
}
