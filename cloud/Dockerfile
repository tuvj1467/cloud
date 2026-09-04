# syntax=docker/dockerfile:1

# ---------- 构建阶段 ----------
FROM golang:1.25-alpine AS builder

WORKDIR /app

# 先复制依赖清单，利用 Docker 层缓存加速重复构建
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并编译（CGO 关闭生成静态二进制，可在精简镜像中运行）
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/storage .

# ---------- 运行阶段 ----------
FROM alpine:3.22

# tzdata 保证 Loc: Local 时区解析正确
RUN apk add --no-cache tzdata

WORKDIR /app
COPY --from=builder /app/storage /app/storage

# 对象存储数据目录（compose 中挂载卷持久化）
RUN mkdir -p /data/cloud_storage

EXPOSE 8081

# 配置文件由 docker-compose 挂载到 /app/etc/storage.yaml
# （见 deploy/cloud/storage.yaml，容器网络内 MySQL/Redis 用服务名互连）
ENTRYPOINT ["/app/storage", "-f", "etc/storage.yaml"]
