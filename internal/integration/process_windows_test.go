//go:build windows

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCommandSupportsWindowsCmdShim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.cmd")
	if err := os.WriteFile(path, []byte("@echo off\r\necho shim-ok\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := runCommand(context.Background(), path, "--ignored")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "shim-ok") {
		t.Fatalf("output = %q", output)
	}
}
