package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	port   = flag.String("port", "8080", "Web server port")
	apiUrl = flag.String("api", "http://localhost:8888", "API gateway URL")
	static = flag.String("static", "./", "Static files directory")
	// cors 允许的跨域来源；空 = 不发送 CORS 头（默认同源部署，页面与 API 同源无需 CORS）。
	// 开发期前端分离部署时显式传入来源（如 http://localhost:5173），
	// 不再默认 * + Authorization——任意页面拿到 token 后即可跨域调用
	corsOrigin = flag.String("cors", "", "Allowed CORS origin (empty = same-origin only, no CORS headers)")
)

func corsMiddleware(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	flag.Parse()

	apiTarget, err := url.Parse(*apiUrl)
	if err != nil {
		fmt.Printf("Failed to parse API URL: %v\n", err)
		os.Exit(1)
	}

	// 上游/响应超时：下载流式响应可能持续数分钟，Transport 不设整体超时，
	// 但必须限制拨号与响应头等待（防上游半开连接拖死代理）
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(apiTarget)
			pr.Out.Host = apiTarget.Host
			// 网关只转发 Grpc-Metadata- 前缀的 header 到 gRPC metadata，
			// 这里把 Authorization 转换为 Grpc-Metadata-Authorization
			if auth := pr.In.Header.Get("Authorization"); auth != "" {
				pr.Out.Header.Set("Grpc-Metadata-Authorization", auth)
			}
		},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	http.Handle("/api/", corsMiddleware(*corsOrigin, http.StripPrefix("/api", proxy)))
	http.Handle("/", http.FileServer(http.Dir(*static)))

	srv := &http.Server{
		Addr: fmt.Sprintf("0.0.0.0:%s", *port),
		// ReadHeaderTimeout 封堵 slowloris（连接建立后永不发完请求头）；
		// Body 读写不设整体超时——上传/流式下载可持续数分钟
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		fmt.Printf("Web server started at http://0.0.0.0:%s\n", *port)
		fmt.Printf("Frontend page: http://0.0.0.0:%s/index.html\n", *port)
		fmt.Printf("API proxy: http://0.0.0.0:%s/api/ -> %s\n", *port, *apiUrl)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("Server failed to start: %v\n", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	fmt.Println("Shutting down web server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
