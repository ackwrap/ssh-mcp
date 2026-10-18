package integration

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func testManager(t *testing.T, lookPath func(string) (string, error), run func(context.Context, string, ...string) (string, error)) *Manager {
	t.Helper()
	home := t.TempDir()
	if lookPath == nil {
		lookPath = func(string) (string, error) { return "", errors.New("not installed in test sandbox") }
	}
	if run == nil {
		// HomeDir only redirects our file access, not a real CLI's user scope.
		// Never fall back to runCommand, especially for `claude mcp add/remove`.
		run = func(_ context.Context, command string, _ ...string) (string, error) {
			t.Fatalf("unexpected external CLI execution in test: %s", command)
			return "", errors.New("external CLI execution blocked")
		}
	}
	manager, err := NewManager(Options{
		ServerCommand: []string{`C:\Program Files\SSH-MCP\ssh-mcp.exe`},
		ServerURL:     testServerURL,
		HomeDir:       home,
		AppData:       filepath.Join(home, "AppData", "Roaming"),
		LocalAppData:  filepath.Join(home, "AppData", "Local"),
		Getenv:        func(string) string { return "" },
		LookPath:      lookPath,
		Run:           run,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
