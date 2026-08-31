package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 || strings.TrimSpace(stdout.String()) != "ssh-mcp 0.1.0" {
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
