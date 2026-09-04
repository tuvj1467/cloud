# syntax=docker/dockerfile:1

# 注意：本 Dockerfile 的构建上下文是仓库根目录（依赖根 go.mod）
#   docker build -f web/Dockerfile .

# ---------- 构建阶段 ----------
FROM golang:1.25-alpine AS builder

WORKDIR /app

# web/server.go 仅依赖标准库，使用仓库根 go.mod
COPY go.mod ./
COPY web/ ./web/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/webserver ./web

# ---------- 运行阶段 ----------
FROM alpine:3.22

WORKDIR /app
COPY --from=builder /app/webserver /app/webserver

# 前端静态资源（server.go 的 -static 参数指向这里）
COPY web/index.html /app/static/
COPY web/css /app/static/css/
COPY web/js /app/static/js/

EXPOSE 8080

# -api 指向 compose 网络内的网关服务；本地直连时改为 http://localhost:8888
ENTRYPOINT ["/app/webserver", "-port", "8080", "-api", "http://gateway:8888", "-static", "/app/static"]
