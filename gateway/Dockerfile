# syntax=docker/dockerfile:1

# ---------- 构建阶段 ----------
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/gateway .

# ---------- 运行阶段 ----------
FROM alpine:3.22

WORKDIR /app
COPY --from=builder /app/gateway /app/gateway

# protoset.pb 是运行时加载的 proto 描述符，必须随镜像分发
COPY --from=builder /app/protoset.pb /app/protoset.pb
COPY --from=builder /app/static /app/static

EXPOSE 8888

# 配置文件由 docker-compose 挂载到 /app/gateway.yaml
# （见 deploy/gateway/gateway.yaml，gRPC 目标为容器服务名 cloud:8081）
ENTRYPOINT ["/app/gateway", "-f", "gateway.yaml"]
