package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

var (
	port   = flag.String("port", "8080", "Web server port")
	apiUrl = flag.String("api", "http://localhost:8888", "API gateway URL")
	static = flag.String("static", "./", "Static files directory")
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

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

	proxy := httputil.NewSingleHostReverseProxy(apiTarget)

	// 网关只转发 Grpc-Metadata- 前缀的 header 到 gRPC metadata
	// 这里把 Authorization 转换为 Grpc-Metadata-Authorization
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		if auth := req.Header.Get("Authorization"); auth != "" {
			req.Header.Set("Grpc-Metadata-Authorization", auth)
		}
	}

	http.Handle("/api/", corsMiddleware(http.StripPrefix("/api", proxy)))
	http.Handle("/", http.FileServer(http.Dir(*static)))

	addr := fmt.Sprintf("0.0.0.0:%s", *port)
	fmt.Printf("Web server started at http://%s\n", addr)
	fmt.Printf("Frontend page: http://%s/index.html\n", addr)
	fmt.Printf("API proxy: http://%s/api/\n", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("Server failed to start: %v\n", err)
		os.Exit(1)
	}
}
