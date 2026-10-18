package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ackwrap/ssh-mcp/internal/config"
	"github.com/ackwrap/ssh-mcp/internal/secure"
	"github.com/ackwrap/ssh-mcp/internal/sshclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	Name       = "ssh-mcp"
	Version    = "0.1.5"
	MaxTimeout = 600
)

type Runtime struct {
	mu      sync.Mutex
	manager *sshclient.Manager
	path    string
}

func NewRuntime() *Runtime { return &Runtime{} }

func OpenStore() (*config.OpenStore, error) {
	store, err := config.NewStore("", "")
	if err != nil {
		return nil, err
	}
	if !store.Exists() {
		return nil, errors.New("尚未初始化：请先打开 WebUI 设置主密码并添加服务器")
	}
	salt, err := store.GetSaltHex()
	if err != nil {
		return nil, err
	}
	key, err := secure.ResolveKey(salt)
	if err != nil {
		return nil, err
	}
	return config.Open(store, key)
}

func (r *Runtime) Manager() (*sshclient.Manager, error) {
	store, err := OpenStore()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manager == nil || r.path != store.Store.Path {
		if r.manager != nil {
			r.manager.CloseAll()
		}
		r.manager = sshclient.NewManager(store)
		r.path = store.Store.Path
	} else {
		r.manager.SetStore(store)
	}
	return r.manager, nil
}

func (r *Runtime) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manager != nil {
		r.manager.CloseAll()
	}
	r.manager = nil
	r.path = ""
}

func NewServer(runtime *Runtime) *mcp.Server {
	if runtime == nil {
		runtime = NewRuntime()
	}
	server := mcp.NewServer(&mcp.Implementation{Name: Name, Version: Version, Title: "SSH-MCP"}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_list_servers", Title: "列出服务器与跳板机", Description: "列出已启用的服务器（含 group 分组）和跳板机，不含凭据。主机及代理地址默认以 ****** 隐藏，只有用户在 WebUI 设置中关闭隐藏 IP 后才显示真实地址。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input emptyInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			servers := make([]map[string]any, 0, len(store.Config.Servers))
			for _, endpoint := range store.Config.Servers {
				if endpoint.Disabled {
					continue
				}
				servers = append(servers, config.PublicEndpoint(structMap(endpoint), store.Config.Settings.AllowMCPAddresses))
			}
			jumps := make([]map[string]any, 0, len(store.Config.JumpHosts))
			for _, endpoint := range store.Config.JumpHosts {
				jumps = append(jumps, config.PublicEndpoint(structMap(endpoint), store.Config.Settings.AllowMCPAddresses))
			}
			return nil, map[string]any{"servers": servers, "jumphosts": jumps}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_add_server", Title: "新增服务器", Description: "新增一台服务器。config 包含 name、host、port、username、auth，可选 group、jump_via 和 proxy。启用/停用状态只能在 WebUI 中修改。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input configInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			if err := checkMCPServerChanges(input.Config); err != nil {
				return nil, nil, err
			}
			endpoint, err := store.AddServer(input.Config)
			if err != nil {
				return nil, nil, err
			}
			runtime.Reset()
			return nil, map[string]any{"ok": true, "name": endpoint.Name}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_update_server", Title: "更新服务器", Description: "按名称部分更新已启用的服务器，changes 中的字段（含 group 分组）覆盖原值。启用/停用状态只能在 WebUI 中修改。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input updateInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			if _, err := availableServer(store, input.Name); err != nil {
				return nil, nil, err
			}
			if err := checkMCPServerChanges(input.Changes); err != nil {
				return nil, nil, err
			}
			endpoint, err := store.UpdateServer(input.Name, input.Changes)
			if err != nil {
				return nil, nil, err
			}
			runtime.Reset()
			return nil, map[string]any{"ok": true, "name": endpoint.Name}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_delete_server", Title: "删除服务器", Description: "删除一台服务器。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input nameInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			if _, err := availableServer(store, input.Name); err != nil {
				return nil, nil, err
			}
			if err := store.DeleteServer(input.Name); err != nil {
				return nil, nil, err
			}
			runtime.Reset()
			return nil, map[string]any{"ok": true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_add_jumphost", Title: "新增跳板机", Description: "新增一台跳板机，jump_via 可指向上一级跳板机。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input configInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			endpoint, err := store.AddJumpHost(input.Config)
			if err != nil {
				return nil, nil, err
			}
			runtime.Reset()
			return nil, map[string]any{"ok": true, "name": endpoint.Name}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_update_jumphost", Title: "更新跳板机", Description: "按名称部分更新跳板机配置。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input updateInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			endpoint, err := store.UpdateJumpHost(input.Name, input.Changes)
			if err != nil {
				return nil, nil, err
			}
			runtime.Reset()
			return nil, map[string]any{"ok": true, "name": endpoint.Name}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_delete_jumphost", Title: "删除跳板机", Description: "删除一台跳板机，仍被引用时拒绝删除。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input nameInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			if err := store.DeleteJumpHost(input.Name); err != nil {
				return nil, nil, err
			}
			runtime.Reset()
			return nil, map[string]any{"ok": true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_exec", Title: "远程执行命令", Description: "在指定服务器执行 shell 命令，非零退出码视为失败。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input execInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			endpoint, err := availableServer(store, input.Server)
			if err != nil {
				return nil, nil, err
			}
			manager, err := runtime.Manager()
			if err != nil {
				return nil, nil, err
			}
			result, err := manager.Exec(endpoint, input.Command, boundedTimeout(input.Timeout), input.Cwd, input.Env)
			if err != nil {
				var commandErr *sshclient.CommandError
				if errors.As(err, &commandErr) {
					exitCode := "未知"
					if commandErr.ExitCode != nil {
						exitCode = fmt.Sprint(*commandErr.ExitCode)
					}
					return nil, nil, fmt.Errorf("命令执行失败（退出码 %s）:\nstdout: %s\nstderr: %s", exitCode, truncate(commandErr.Stdout, 2000), truncate(commandErr.Stderr, 2000))
				}
				return nil, nil, err
			}
			return nil, map[string]any{"server": input.Server, "ok": true, "stdout": result.Stdout, "stderr": result.Stderr, "exit_code": result.ExitCode}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_exec_multi", Title: "批量远程执行", Description: "在多台服务器逐个执行同一命令，单台失败不影响其他服务器。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input execMultiInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			if len(input.Servers) == 0 {
				return nil, nil, errors.New("请至少指定一个服务器")
			}
			targets := make([]*config.Server, 0, len(input.Servers))
			for _, name := range input.Servers {
				endpoint, findErr := availableServer(store, strings.TrimSpace(name))
				if findErr != nil {
					return nil, nil, findErr
				}
				targets = append(targets, endpoint)
			}
			manager, err := runtime.Manager()
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"results": manager.ExecMulti(targets, input.Command, boundedTimeout(input.Timeout))}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_test_connection", Title: "测试连接", Description: "测试指定服务器的 SSH 连通性。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input serverInput) (*mcp.CallToolResult, map[string]any, error) {
			store, err := OpenStore()
			if err != nil {
				return nil, nil, err
			}
			endpoint, err := availableServer(store, input.Server)
			if err != nil {
				return nil, nil, err
			}
			manager, err := runtime.Manager()
			if err != nil {
				return nil, nil, err
			}
			ok, message := manager.Test(endpoint, 10*time.Second)
			return nil, map[string]any{"server": input.Server, "ok": ok, "message": message}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_upload", Title: "上传文件", Description: "通过 SFTP 将本地文件上传到远程服务器。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input uploadInput) (*mcp.CallToolResult, map[string]any, error) {
			endpoint, manager, err := toolTarget(runtime, input.Server)
			if err != nil {
				return nil, nil, err
			}
			output, err := manager.Upload(endpoint, input.LocalPath, input.RemotePath)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_download", Title: "下载文件", Description: "通过 SFTP 将远程文件下载到本地。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input downloadInput) (*mcp.CallToolResult, map[string]any, error) {
			endpoint, manager, err := toolTarget(runtime, input.Server)
			if err != nil {
				return nil, nil, err
			}
			output, err := manager.Download(endpoint, input.RemotePath, input.LocalPath)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_read_file", Title: "读取远程文件", Description: "读取远程文本文件，默认最多 1MB。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input readInput) (*mcp.CallToolResult, map[string]any, error) {
			endpoint, manager, err := toolTarget(runtime, input.Server)
			if err != nil {
				return nil, nil, err
			}
			limit := input.MaxBytes
			if limit == 0 {
				limit = 1_000_000
			}
			content, err := manager.ReadFile(endpoint, input.Path, limit)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"server": input.Server, "path": input.Path, "content": content}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_write_file", Title: "写入远程文件", Description: "覆盖写入远程文本文件。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input writeInput) (*mcp.CallToolResult, map[string]any, error) {
			endpoint, manager, err := toolTarget(runtime, input.Server)
			if err != nil {
				return nil, nil, err
			}
			output, err := manager.WriteFile(endpoint, input.Path, input.Content)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_list_dir", Title: "列出远程目录", Description: "列出远程目录内容，目录在前并按名称排序。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input listDirInput) (*mcp.CallToolResult, map[string]any, error) {
			endpoint, manager, err := toolTarget(runtime, input.Server)
			if err != nil {
				return nil, nil, err
			}
			path := input.Path
			if path == "" {
				path = "."
			}
			items, err := manager.ListDir(endpoint, path)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{"server": input.Server, "path": path, "items": items}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "ssh_stat", Title: "远程文件属性", Description: "获取远程文件或目录的大小、修改时间和权限。"},
		func(ctx context.Context, req *mcp.CallToolRequest, input pathInput) (*mcp.CallToolResult, map[string]any, error) {
			endpoint, manager, err := toolTarget(runtime, input.Server)
			if err != nil {
				return nil, nil, err
			}
			output, err := manager.Stat(endpoint, input.Path)
			return nil, output, err
		})

	return server
}

type emptyInput struct{}
type configInput struct {
	Config map[string]any `json:"config" jsonschema:"服务器或跳板机配置对象"`
}
type updateInput struct {
	Name    string         `json:"name"`
	Changes map[string]any `json:"changes"`
}
type nameInput struct {
	Name string `json:"name"`
}
type serverInput struct {
	Server string `json:"server"`
}
type execInput struct {
	Server  string            `json:"server"`
	Command string            `json:"command"`
	Timeout int               `json:"timeout,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}
type execMultiInput struct {
	Servers []string `json:"servers"`
	Command string   `json:"command"`
	Timeout int      `json:"timeout,omitempty"`
}
type uploadInput struct {
	Server     string `json:"server"`
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
}
type downloadInput struct {
	Server     string `json:"server"`
	RemotePath string `json:"remote_path"`
	LocalPath  string `json:"local_path"`
}
type readInput struct {
	Server   string `json:"server"`
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}
type writeInput struct {
	Server  string `json:"server"`
	Path    string `json:"path"`
	Content string `json:"content"`
}
type listDirInput struct {
	Server string `json:"server"`
	Path   string `json:"path,omitempty"`
}
type pathInput struct {
	Server string `json:"server"`
	Path   string `json:"path"`
}

func boundedTimeout(seconds int) time.Duration {
	if seconds == 0 {
		seconds = 30
	}
	if seconds < 1 {
		seconds = 1
	}
	if seconds > MaxTimeout {
		seconds = MaxTimeout
	}
	return time.Duration(seconds) * time.Second
}
func availableServer(store *config.OpenStore, name string) (*config.Server, error) {
	endpoint, err := store.GetServer(name)
	if err != nil || endpoint.Disabled {
		return nil, errors.New("服务器不存在或不可用")
	}
	return endpoint, nil
}

func checkMCPServerChanges(changes map[string]any) error {
	if _, exists := changes["disabled"]; exists {
		return errors.New("启用/停用状态只能在 WebUI 中修改")
	}
	return nil
}

func toolTarget(runtime *Runtime, name string) (*config.Server, *sshclient.Manager, error) {
	store, err := OpenStore()
	if err != nil {
		return nil, nil, err
	}
	endpoint, err := availableServer(store, name)
	if err != nil {
		return nil, nil, err
	}
	manager, err := runtime.Manager()
	return endpoint, manager, err
}
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func structMap(value any) map[string]any {
	data, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(data, &result)
	return result
}
