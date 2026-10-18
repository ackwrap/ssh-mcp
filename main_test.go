package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCLIFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 || strings.TrimSpace(stdout.String()) != "ssh-mcp 0.1.4" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--mcp", "--open"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr.String(), "不能") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestStdioMCP(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestStdioMCPHelper$")
	command.Env = append(os.Environ(), "SSH_MCP_TEST_STDIO=1")
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 16 {
		t.Fatalf("tool count = %d", len(result.Tools))
	}
}

func TestStdioMCPHelper(t *testing.T) {
	if os.Getenv("SSH_MCP_TEST_STDIO") != "1" {
		return
	}
	os.Exit(run(nil, os.Stdout, os.Stderr))
}

func TestResolveLaunchMode(t *testing.T) {
	tests := []struct {
		name            string
		mcp, tray, open bool
		want            launchMode
		wantErr         bool
	}{
		{name: "default is stdio", want: launchModeStdio},
		{name: "legacy mcp flag", mcp: true, want: launchModeStdio},
		{name: "tray", tray: true, want: launchModeTray},
		{name: "open", open: true, want: launchModeOpen},
		{name: "conflicting modes", tray: true, open: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveLaunchMode(test.mcp, test.tray, test.open)
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, test.wantErr)
			}
			if !test.wantErr && got != test.want {
				t.Fatalf("mode=%v want=%v", got, test.want)
			}
		})
	}
}
