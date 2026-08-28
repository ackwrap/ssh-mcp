package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const serverName = "ssh-mcp"

type ToolInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Installed   bool   `json:"installed"`
	Configured  bool   `json:"configured"`
	CommandPath string `json:"command_path"`
	ConfigPath  string `json:"config_path"`
	Method      string `json:"method"`
	Note        string `json:"note"`
	RestartHint string `json:"restart_hint"`
}

type Result struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	ConfigPath string `json:"config_path"`
	BackupPath string `json:"backup_path,omitempty"`
}

type Options struct {
	ServerCommand []string
	HomeDir       string
	AppData       string
	LocalAppData  string
	Getenv        func(string) string
	LookPath      func(string) (string, error)
	Run           func(context.Context, string, ...string) (string, error)
}

type Manager struct {
	serverCommand []string
	homeDir       string
	appData       string
	localAppData  string
	getenv        func(string) string
	lookPath      func(string) (string, error)
	run           func(context.Context, string, ...string) (string, error)
	mu            sync.Mutex
}

func NewManager(options Options) (*Manager, error) {
	if len(options.ServerCommand) == 0 || strings.TrimSpace(options.ServerCommand[0]) == "" {
		return nil, errors.New("MCP 服务器启动命令不能为空")
	}
	home := options.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户目录失败: %w", err)
		}
	}
	getenv := options.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	appData := options.AppData
	if appData == "" {
		appData = getenv("APPDATA")
	}
	if appData == "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	localAppData := options.LocalAppData
	if localAppData == "" {
		localAppData = getenv("LOCALAPPDATA")
	}
	if localAppData == "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	run := options.Run
	if run == nil {
		run = runCommand
	}
	return &Manager{
		serverCommand: append([]string(nil), options.ServerCommand...),
		homeDir:       home,
		appData:       appData,
		localAppData:  localAppData,
		getenv:        getenv,
		lookPath:      lookPath,
		run:           run,
	}, nil
}

func (m *Manager) ServerCommand() []string {
	return append([]string(nil), m.serverCommand...)
}
