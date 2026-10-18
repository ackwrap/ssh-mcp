//go:build !windows

package tray

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/ackwrap/ssh-mcp/internal/mcpserver"
	"github.com/ackwrap/ssh-mcp/internal/web"
)

func Run(openWebUI bool) error {
	port, err := web.GetPort()
	if err != nil {
		return err
	}
	runtime := mcpserver.NewRuntime()
	defer runtime.Reset()
	app, err := web.NewServer(nil, runtime)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("无法启动 WebUI，端口 %d 可能被占用: %w", port, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "SSH-MCP WebUI: http://127.0.0.1:%d（前台运行，Ctrl+C 退出）\n", port)
	var openBrowser func() error
	if openWebUI {
		openBrowser = func() error { return OpenWebUI(port, "") }
	}
	return serveWebUI(ctx, listener, app.Handler(), openBrowser)
}

// macOS/Linux run the management service in the foreground without a native tray.
func serveWebUI(ctx context.Context, listener net.Listener, handler http.Handler, openBrowser func() error) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	if openBrowser != nil {
		if err := openBrowser(); err != nil {
			fmt.Fprintln(os.Stderr, "无法自动打开浏览器，请手动访问 WebUI:", err)
		}
	}
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := server.Shutdown(shutdown)
		<-done
		return err
	}
}

func OpenWebUI(port int, path string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, command, fmt.Sprintf("http://127.0.0.1:%d%s", port, path)).Run()
}
