//go:build windows

package tray

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ackwrap/ssh-mcp/internal/mcpserver"
	"github.com/ackwrap/ssh-mcp/internal/web"
	"github.com/getlantern/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	mutexName = `Local\SSH-MCP-Singleton`
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue  = "SSH-MCP"
)

var singleton windows.Handle

//go:embed icon.ico
var appIcon []byte

type service struct {
	mu      sync.Mutex
	port    int
	server  *http.Server
	runtime *mcpserver.Runtime
}

func Run(openWebUI bool) error {
	port, err := web.GetPort()
	if err != nil {
		return err
	}
	unique, err := singleInstance()
	if err != nil {
		return err
	}
	if !unique {
		if openWebUI {
			return OpenWebUI(port, "")
		}
		return nil
	}

	runtime := mcpserver.NewRuntime()
	svc := &service{port: port, runtime: runtime}
	if err := svc.start(); err != nil {
		return err
	}
	if openWebUI {
		_ = OpenWebUI(port, "")
	}

	systray.Run(func() {
		systray.SetIcon(appIcon)
		systray.SetTitle("SSH-MCP")
		systray.SetTooltip("SSH-MCP")
		openItem := systray.AddMenuItem("打开 WebUI", "打开本地管理页面")
		restartItem := systray.AddMenuItem("重启服务", "重启 WebUI 与 HTTP MCP")
		resetItem := systray.AddMenuItem("重置密码", "打开密码设置页面")
		systray.AddSeparator()
		autostartItem := systray.AddMenuItemCheckbox("开机自启", "登录 Windows 时启动 SSH-MCP", autostartEnabled())
		systray.AddSeparator()
		quitItem := systray.AddMenuItem("退出", "退出 SSH-MCP")
		go func() {
			for {
				select {
				case <-openItem.ClickedCh:
					_ = OpenWebUI(port, "")
				case <-restartItem.ClickedCh:
					_ = svc.restart()
				case <-resetItem.ClickedCh:
					_ = OpenWebUI(port, "/#settings")
				case <-autostartItem.ClickedCh:
					enabled := !autostartEnabled()
					if setAutostart(enabled) == nil {
						if enabled {
							autostartItem.Check()
						} else {
							autostartItem.Uncheck()
						}
					}
				case <-quitItem.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}, func() { _ = svc.stop() })
	return nil
}

func (s *service) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, err := web.NewServer(nil, s.runtime)
	if err != nil {
		return err
	}
	s.server = &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", s.port), Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second}
	server := s.server
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "WebUI 服务异常:", err)
		}
	}()
	if !waitForService(s.port, 15*time.Second) {
		_ = server.Close()
		return fmt.Errorf("WebUI 服务启动失败，端口 %d 可能被占用", s.port)
	}
	return nil
}

func (s *service) stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.server.Shutdown(ctx)
	s.server = nil
	s.runtime.Reset()
	return err
}

func (s *service) restart() error {
	if err := s.stop(); err != nil {
		return err
	}
	return s.start()
}

func waitForService(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
		if err == nil {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func OpenWebUI(port int, path string) error {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func singleInstance() (bool, error) {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return false, err
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return false, nil
	}
	singleton = handle
	return true, nil
}

func autostartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue(runValue)
	return err == nil
}

func setAutostart(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !enabled {
		err := key.DeleteValue(runValue)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return key.SetStringValue(runValue, `"`+executable+`" --tray`)
}
