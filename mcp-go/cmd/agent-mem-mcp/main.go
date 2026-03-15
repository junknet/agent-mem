package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
		log.Fatalf("[CRITICAL] 配置加载失败: %v", err)
	}

	app, err := NewApp(settings)
	if err != nil {
		log.Fatalf("[CRITICAL] 应用初始化失败: %v", err)
	}
	defer app.Close()

	if err := app.EnsureSchema(context.Background(), *resetDB); err != nil {
		log.Fatalf("[CRITICAL] 数据库 schema 初始化失败: %v", err)
	}
	if *resetOnly {
		return
	}

	server := buildServer(app)

	switch strings.ToLower(*transport) {
	case "stdio":
		ctx := context.Background()
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("[CRITICAL] STDIO 传输失败: %v", err)
		}
		return
	case "sse", "streamable", "http", "both":
		// 继续 HTTP 模式
	default:
		log.Fatalf("[CRITICAL] 不支持的 transport: %s", *transport)
	}

	logger := slog.Default()

	mux := http.NewServeMux()
	if *transport == "sse" || *transport == "http" || *transport == "both" {
		sseHandler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil)
		mux.Handle("/sse", sseHandler)
		log.Println("已注册 SSE 端点: /sse")
	}
	if *transport == "streamable" || *transport == "http" || *transport == "both" {
		streamHandler := mcp.NewStreamableHTTPHandler(
			func(*http.Request) *mcp.Server { return server },
			&mcp.StreamableHTTPOptions{
				SessionTimeout: 30 * time.Minute,
				Logger:         logger,
			},
		)
		mux.Handle("/mcp", streamHandler)
		log.Println("已注册 Streamable HTTP 端点: /mcp")
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","ts":%d}`, time.Now().Unix())
	})
	registerHTTPRoutes(mux, app)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	handler := requireToken(mux, envOrDefault("AGENT_MEM_HTTP_TOKEN", ""))
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		// WriteTimeout 必须为 0：SSE 和 Streamable HTTP 都需要长连接流，
		// 非零值会在超时后强制关闭连接，导致客户端 session 丢失。
		WriteTimeout:   0,
		IdleTimeout:    300 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// 优雅退出：监听 SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("MCP 服务启动: http://%s (pid=%d)", addr, os.Getpid())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[CRITICAL] HTTP 服务异常退出: %v", err)
		}
	}()

	sig := <-quit
	log.Printf("收到信号 %v，开始优雅关闭...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[CRITICAL] 优雅关闭失败: %v", err)
	}
	log.Println("服务已关闭")
}
