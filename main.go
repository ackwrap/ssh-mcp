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

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ssh-mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "用法: ssh-mcp [--version] [--mcp | --open]")
		flags.PrintDefaults()
	}
	var version, shortVersion, mcpMode, openWebUI bool
	flags.BoolVar(&version, "version", false, "显示版本并退出")
	flags.BoolVar(&shortVersion, "v", false, "显示版本并退出")
	flags.BoolVar(&mcpMode, "mcp", false, "以 stdio 方式运行 MCP 服务器")
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
	if mcpMode && openWebUI {
		fmt.Fprintln(stderr, "--open 不能与 --mcp 同时使用")
		return 2
	}
	if mcpMode {
		server := mcpserver.NewServer(mcpserver.NewRuntime())
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if err := tray.Run(openWebUI); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
