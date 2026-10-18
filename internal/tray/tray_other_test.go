//go:build !windows

package tray

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRejectsInvalidPort(t *testing.T) {
	t.Setenv("SSH_MCP_PORT", "0")
	if err := Run(false); err == nil || !strings.Contains(err.Error(), "SSH_MCP_PORT") {
		t.Fatalf("invalid port error = %v", err)
	}
}

func TestRunRejectsOccupiedPort(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("SSH_MCP_PORT", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	if err := Run(false); err == nil || !strings.Contains(err.Error(), "无法启动 WebUI") {
		t.Fatalf("occupied port error = %v", err)
	}
}

func TestForegroundWebUI(t *testing.T) {
	for _, browserFails := range []bool{false, true} {
		t.Run(fmt.Sprint(browserFails), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var opened atomic.Bool
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, "ready")
			})
			done := make(chan error, 1)
			go func() {
				done <- serveWebUI(ctx, listener, handler, func() error {
					opened.Store(true)
					if browserFails {
						return fmt.Errorf("headless environment")
					}
					return nil
				})
			}()
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			response, err := client.Get("http://" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || string(body) != "ready" {
				t.Fatalf("response=%q err=%v", body, err)
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("foreground service did not stop")
			}
			if !opened.Load() {
				t.Fatal("browser opener was not called")
			}
			if _, err := client.Get("http://" + listener.Addr().String()); err == nil {
				t.Fatal("listener remained open after shutdown")
			}
		})
	}
}
