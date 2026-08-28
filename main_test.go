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
