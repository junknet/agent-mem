package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	var (
		host      = flag.String("host", defaultHost, "监听地址")
		port      = flag.Int("port", defaultPort, "监听端口")
		transport = flag.String("transport", "http", "传输方式：http/sse/streamable/stdio")
		config    = flag.String("config", "", "配置文件路径")
		resetDB   = flag.Bool("reset-db", false, "重建数据库表结构（清空数据）")
		resetOnly = flag.Bool("reset-only", false, "仅执行数据库重建/迁移后退出")
	)
	flag.Parse()

	settings, err := loadSettings(*config)
	if err != nil {
		panic(err)
	}

	app, err := NewApp(settings)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	if err := app.EnsureSchema(context.Background(), *resetDB); err != nil {
		panic(err)
	}
	if *resetOnly {
		return
	}

	server := buildServer(app)

	switch strings.ToLower(*transport) {
	case "stdio":
		ctx := context.Background()
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			panic(err)
		}
		return
	case "sse", "streamable", "http", "both":
		// 继续 HTTP 模式
	default:
		panic(fmt.Errorf("不支持的 transport: %s", *transport))
	}

	mux := http.NewServeMux()
	if *transport == "sse" || *transport == "http" || *transport == "both" {
		sseHandler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil)
		mux.Handle("/sse", sseHandler)
	}
	if *transport == "streamable" || *transport == "http" || *transport == "both" {
		streamHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
		mux.Handle("/mcp", streamHandler)
	}
	registerHTTPRoutes(mux, app)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	fmt.Printf("MCP 服务启动: http://%s\n", addr)
	handler := requireToken(mux, envOrDefault("AGENT_MEM_HTTP_TOKEN", ""))
	if err := http.ListenAndServe(addr, handler); err != nil {
		panic(err)
	}
}
