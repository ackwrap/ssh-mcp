package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ssh-mcp/ssh-mcp-go/internal/mcpserver"
	"github.com/ssh-mcp/ssh-mcp-go/internal/tray"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

type launchMode int

const (
	launchModeStdio launchMode = iota
	launchModeTray
	launchModeOpen
)

func resolveLaunchMode(mcpMode, trayMode, openWebUI bool) (launchMode, error) {
	selected := 0
	for _, enabled := range []bool{mcpMode, trayMode, openWebUI} {
		if enabled {
			selected++
		}
	}
	if selected > 1 {
		return launchModeStdio, errors.New("--mcp、--tray 和 --open 不能同时使用")
	}
	if trayMode {
		return launchModeTray, nil
	}
	if openWebUI {
		return launchModeOpen, nil
	}
	return launchModeStdio, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ssh-mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "用法: ssh-mcp [--version] [--mcp | --tray | --open]")
		flags.PrintDefaults()
	}
	var version, shortVersion, mcpMode, trayMode, openWebUI bool
	flags.BoolVar(&version, "version", false, "显示版本并退出")
	flags.BoolVar(&shortVersion, "v", false, "显示版本并退出")
	flags.BoolVar(&mcpMode, "mcp", false, "以 stdio 方式运行 MCP 服务器（兼容旧配置，默认行为）")
	flags.BoolVar(&trayMode, "tray", false, "启动或连接托盘实例")
	flags.BoolVar(&openWebUI, "open", false, "启动或连接托盘实例后打开 WebUI")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "未知参数: %s\n", flags.Arg(0))
		return 2
	}
	if version || shortVersion {
		fmt.Fprintf(stdout, "ssh-mcp %s\n", mcpserver.Version)
		return 0
	}
	mode, err := resolveLaunchMode(mcpMode, trayMode, openWebUI)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if mode == launchModeTray || mode == launchModeOpen {
		if err := tray.Run(mode == launchModeOpen); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	server := mcpserver.NewServer(mcpserver.NewRuntime())
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
