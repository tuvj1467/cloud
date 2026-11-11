# ONE CLOUD — 云对象存储系统

> 基于 go-zero + ent 构建的轻量级对象存储服务，支持桶管理、文件上传下载、秒传、分片上传、文件分享等完整 OSS 能力。

## 目录

- [项目简介](#项目简介)
- [技术栈](#技术栈)
- [核心特性](#核心特性)
- [架构设计](#架构设计)
- [项目结构](#项目结构)
- [快速开始](#快速开始)
- [API 总览](#api-总览)
- [数据库设计](#数据库设计)
- [开发与贡献](#开发与贡献)
- [文档导航](#文档导航)

## 项目简介

ONE CLOUD 是一个生产级的对象存储服务，采用 **gRPC 微服务 + HTTP 网关** 的架构。核心服务（`cloud/`）负责所有业务逻辑，网关（`gateway/`）负责 REST → gRPC 协议转换，二者通过 `protoset.pb` 解耦。

系统从基础的文件上传下载出发，逐步扩展到桶管理、秒传、分片上传、文件分享等高级能力，并配套了密码加密（bcrypt）、存量数据迁移、错误码规范、日志补全等健壮性改造，可直接用于小型团队的私有对象存储场景。

## 技术栈

| 层级 | 技术 | 说明 |
|------|------|------|
| 服务框架 | [go-zero](https://github.com/zeromicro/go-zero) | gRPC 服务（v1.6）+ HTTP 网关（v1.10） |
| ORM | [ent](https://entgo.io/) v0.14 | 实体定义 + 代码生成 |
| 数据库 | MySQL 8.x | 用户/桶/文件元数据 |
| 缓存 | Redis 7.x | 秒传索引、分片状态、分享缓存 |
| 存储后端 | 本地磁盘 / MinIO / S3 兼容端点 | 物理文件存储，驱动抽象层可切换（minio-go） |
| 接口定义 | Protobuf + protoc-gen-validate | 自动生成 gRPC 代码与参数校验 |
| 认证 | JWT (golang-jwt v4) | 无状态鉴权，白名单匿名接口除外 |
| 密码加密 | bcrypt | 单向加密 + 存量明文渐进式迁移 |

## 核心特性

### ✅ 已实现

- **用户认证**：账号/手机/邮箱注册登录、验证码登录、密码重置、找回账号
- **桶管理**：创建 / 查询 / 更新 / 删除桶，容量限制，乐观锁并发控制
- **文件服务**：
  - 基础：上传、下载、删除、重命名、文件列表（用户维度 / 桶维度）
  - 高级：文件复制（物理复制 + 新建元数据）
- **秒传**：基于 MD5 的 Redis 索引 + DB 兜底，命中即返回
- **分片上传**：初始化 / 上传分片 / 合并 / 取消，支持断点续传与幂等重传
- **文件分享**：生成提取码，支持过期时间与访问次数限制，匿名可访问
- **健壮性**：
  - bcrypt 密码加密 + 存量明文渐进式迁移
  - 存量 File 数据自动迁移（bucket_id 为空回填 default 桶）
  - 统一错误码（10xxx - 50xxx 段）
  - 关键路径日志补全（logx）
- **P7 安全与健壮性修复**（[设计文档](doc/11_P7_安全与健壮性修复设计.md)）：
  - 验证码链路真实校验（一次性消费 / 失败计数 / 频率限制，dev 日志发送）
  - 存储路径安全（SafeJoin + 后缀/upload_id 白名单，封堵路径穿越）
  - 桶配额原子化（单条 UPDATE，杜绝并发超卖）+ 上传大小治理（MaxUploadSize）
  - 删除事务化（文件级联分享 / force 删桶级联清理）+ 分片完整性校验与临时目录清理
  - 分片上传 GC：启动 30s 后首扫 + 每 12h 周期清理孤儿 .tmp 目录与过期 pending 分片记录
  - 生产面收敛：gRPC reflection 仅 dev 模式开启
- **用量统计与趋势**：文件数/总容量/分桶用量（/file/stats）+ 30 天趋势（/file/stats/trend，每日快照）+ 前端统计卡与趋势图
- **管理后台**（[doc/14](doc/14_管理后台设计.md)）：角色模型（admin）+ 用户管理（搜索/改角色/禁用）+ 全局统计 + 管理操作审计 + 强制删除；前端管理面板按角色显隐
- **MinIO/S3 驱动**（[doc/04](doc/04_存储引擎设计.md)）：S3 兼容端点读写、原生分片（ETag 校验/服务端拷贝就位）、预签名 URL——**已通过真实 MinIO 服务运行验证**（冒烟 + 端到端）
- **预签名下载**：`GET /file/presign`（需登录）生成直连下载 URL——绕过网关体积限制；本地驱动自动回落普通下载（前端无感）
- **P8 Web 前端完善**（[设计文档](doc/12_P8_前端完善设计.md)）：
  - Vue 3 本地化重构（零构建链），组件化覆盖全部 25 个 REST 接口
  - 分片上传（3 并发 / 断点重试 / 取消 / 秒传标识）+ 字节级进度直传
  - 匿名文件分享（创建 / 访问页 / DownloadByShare 匿名下载）
  - XSS 根治、401 自动登出、错误码中文提示、完整分页与排序、图片文件在线预览

### 🚧 规划中

- 图片处理（缩放 / 裁剪 / 水印）

## 架构设计

```
                    ┌─────────────────────┐
                    │      Gateway        │
                    │   (HTTP :8888)      │
                    │  REST → gRPC 转换   │
                    └──────────┬──────────┘
                               │ gRPC
                    ┌──────────▼──────────┐
                    │   Storage RPC       │
                    │   (:8081)            │
                    │  ┌───────────────┐  │
                    │  │  Logic 层     │  │  业务逻辑
                    │  └───────┬───────┘  │
                    │          │          │
                    │  ┌───────▼───────┐  │
                    │  │ Repository 层 │  │  数据访问
                    │  └───────┬───────┘  │
                    │          │          │
                    │  ┌───────▼───────┐  │
                    │  │ ent ORM       │  │  实体操作
                    │  └───────┬───────┘  │
                    └──────────┼──────────┘
                               │
         ┌─────────────────────┼──────────────────────┐
         ▼                     ▼                      ▼
   ┌───────────┐        ┌────────────┐        ┌─────────────┐
   │   MySQL   │        │   Redis    │        │   Storage   │
   │ (元数据)  │        │  (缓存索引) │        │ (物理文件)  │
   └───────────┘        └────────────┘        └─────────────┘
```

## 项目结构

```
ONE_CLOUD/
├── cloud/                        # 核心 RPC 服务
│   ├── main.go                   # 服务入口（拦截器链/清理任务启动）
│   ├── etc/
│   │   └── storage.yaml          # 服务配置（MySQL/Redis/Storage/JWT/限流）
│   ├── proto/                    # Protobuf 定义与生成脚本
│   │   ├── service.proto         # gRPC 服务定义（46 个端点）
│   │   ├── file/user/bucket/base.proto
│   │   ├── .proto/service/       # 生成产物（.pb.go，提交入库）
│   │   └── generate_all.cmd      # 三步再生成脚本（消息/gRPC/protoset）
│   ├── ent/
│   │   ├── schema/               # ent 实体定义（手写，9 张表）
│   │   │   ├── user/bucket/file/file_part/file_share.go
│   │   │   ├── share_access_log.go   # 分享访问审计
│   │   │   ├── admin_audit_log.go    # 管理操作审计
│   │   │   ├── storage_snapshot.go   # 用量每日快照
│   │   │   └── mixin.go              # 公共字段混合（时间/版本）
│   │   └── ent/                  # ent 生成代码（自动生成，勿手动改）
│   ├── internal/
│   │   ├── config/               # 配置结构
│   │   ├── logic/                # 业务逻辑
│   │   │   ├── user/             #   用户/认证/管理后台
│   │   │   ├── bucket/           #   桶模块
│   │   │   └── file/             #   文件（分片/分享/回收站/统计）
│   │   ├── middleware/           # 中间件（JWT + 限流拦截器）
│   │   ├── pkg/                  # 基础包
│   │   │   ├── cache/            #   Redis 缓存（唯一访问层，nil 安全）
│   │   │   ├── cleanup/          #   定时清扫（回收站/审计/.tmp）
│   │   │   ├── consts/           #   跨层业务常量
│   │   │   ├── database/         #   MySQL 连接
│   │   │   ├── jwt/              #   JWT（会话版本化）
│   │   │   ├── loginlock/        #   登录失败锁定（含降级）
│   │   │   ├── migrate/          #   存量数据迁移
│   │   │   ├── passwd/           #   bcrypt 密码工具
│   │   │   ├── response/         #   统一响应 + 错误码
│   │   │   ├── snapshot/         #   用量每日快照任务
│   │   │   ├── snowId/           #   雪花 ID
│   │   │   ├── storage/          #   存储驱动（local/minio + 路径安全）
│   │   │   ├── thumb/            #   缩略图生成
│   │   │   ├── util/             #   通用工具
│   │   │   └── verification/     #   验证码（频控/日配额）
│   │   ├── repository/           # 数据访问层（Repository 模式）
│   │   ├── server/               # gRPC server 实现（纯委托，手工维护）
│   │   └── svc/                  # 服务上下文（依赖注入）
│
├── gateway/                      # HTTP 网关
│   ├── gateway.go                # 网关入口（Authorization 桥接/大消息上限）
│   ├── gateway.yaml              # 路由映射（与 deploy/ 下副本保持同步）
│   ├── protoset.pb               # proto 描述符集（改 proto 后须再生成）
│   └── type/                     # 自定义 Marshaler / 统一响应包装
│
├── web/                          # Web 演示前端
│   ├── server.go                 # 静态服务器 + API 反向代理（:8080）
│   ├── index.html                # 前端页面
│   ├── css/                      # 样式
│   └── js/                       # 脚本（Vue 3 无构建链，vendor 内置）
│
├── doc/                          # 项目文档（16 篇，索引见 doc/README.md）
│   └── 15_项目交接文档.md        # ⭐ 接手者第一入口
│
├── scripts/
│   └── smoke_e2e.py             # E2E 冒烟（35 项断言，纯标准库）
├── .github/workflows/ci.yml     # CI（三模块测试 + 三镜像构建）
├── docker-compose.yml            # Docker Compose 一键部署（含 MySQL/Redis）
└── deploy/                       # 容器化部署专用配置
```

## 快速开始

### 环境要求

- Go **1.25+**
- MySQL **8.x**
- Redis **7.x**
- protoc + protoc-gen-go + protoc-gen-go-grpc + protoc-gen-validate
- ent（代码生成器）
- goctl（go-zero 代码生成器）

### 1. 克隆项目

```bash
git clone <repo-url>
cd ONE_CLOUD
```

### 2. 配置数据库与缓存

编辑 `cloud/etc/storage.yaml`：

```yaml
MySQL:
  Host: 127.0.0.1:3306
  User: "root"
  Password: "12345"
  Database: "cloud"

Redis:
  Addr: 127.0.0.1:6379
```

### 3. 创建数据库

```sql
CREATE DATABASE `cloud` DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 4. 启动 RPC 服务

```bash
cd cloud
go run main.go -f etc/storage.yaml
```

服务启动后会：
- 自动初始化 ent 数据库表结构
- 自动执行存量数据迁移（dev 模式下）
- 监听 gRPC 端口 `8081`

### 5. 启动 HTTP 网关

```bash
cd ../gateway
go run gateway.go -f gateway.yaml
```

网关监听 HTTP 端口 `8888`，所有 REST 请求自动转发到 gRPC 服务。

### 6. 启动 Web 前端（可选）

```bash
cd ../web
go run server.go
```

Web 服务监听端口 `8080`（可用 `-port` 修改）：

- 前端页面：http://127.0.0.1:8080/index.html
- API 代理：`/api/*` 反向代理到网关 `http://localhost:8888`（可用 `-api` 修改），并自动把 `Authorization` 请求头转换为 `Grpc-Metadata-Authorization` 以通过 JWT 拦截器

### 7. 验证

```bash
# 注册
curl -X POST http://127.0.0.1:8888/user/register \
  -H "Content-Type: application/json" \
  -d '{"account":"test","password":"123456","nickname":"测试用户"}'

# 登录获取 token
curl -X POST http://127.0.0.1:8888/user/loginByAccount \
  -H "Content-Type: application/json" \
  -d '{"account":"test","password":"123456"}'

# 携带 token 创建桶
curl -X POST http://127.0.0.1:8888/bucket/create \
  -H "Authorization: <token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-bucket"}'
```

## API 总览

### 用户服务（`/storage.User`）

| 接口 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| Register | POST | - | 账号注册 |
| LoginByAccount | POST | - | 账号密码登录 |
| LoginByPhoneSms | POST | - | 手机验证码登录 |
| LoginByEmailSms | POST | - | 邮箱验证码登录 |
| SendVerificationCode | POST | - | 发送验证码 |
| ResetPassword | POST | - | 重置密码 |
| FindAccount | POST | - | 找回账号 |

### 桶服务（`/storage.Bucket`）

| 接口 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| CreateBucket | POST | ✅ | 创建桶 |
| GetBucketList | GET | ✅ | 获取桶列表 |
| GetBucketInfo | GET | ✅ | 获取桶详情 |
| UpdateBucket | POST | ✅ | 更新桶信息 |
| DeleteBucket | POST | ✅ | 删除桶 |

### 文件服务（`/storage.File`）

#### 基础操作

| 接口 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| GetFileInfo | GET | ✅ | 获取文件信息 |
| UploadFile | POST | ✅ | 上传文件（支持秒传） |
| GetFileList | GET | ✅ | 用户全部文件列表 |
| GetBucketFileList | GET | ✅ | 桶内文件列表 |
| RenameFile | POST | ✅ | 重命名文件 |
| DeleteFile | POST | ✅ | 删除文件 |
| DownloadFile | GET | ✅ | 下载文件 |
| CopyFile | POST | ✅ | 复制文件 |

#### 分片上传

| 接口 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| InitMultipartUpload | POST | ✅ | 初始化分片上传 |
| UploadPart | POST | ✅ | 上传单个分片 |
| CompleteMultipartUpload | POST | ✅ | 合并分片完成上传 |
| AbortMultipartUpload | POST | ✅ | 取消分片上传 |

#### 文件分享

| 接口 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| CreateShare | POST | ✅ | 创建分享链接 |
| AccessShare | GET | - | 匿名访问分享 |
| DownloadByShare | GET | - | 匿名分享下载（校验提取码/有效期/下载次数；permission=read 拒绝；>5MB 建议走流式） |
| DownloadByShareStream | POST | - | 匿名分享流式下载（64KB 分块 server-streaming，大文件不驻内存） |
| GetMyShares | GET | ✅ | 我的分享列表（分页，含文件名/已用次数） |
| RevokeShare | POST | ✅ | 撤销分享（仅限本人，立即失效） |

#### 扩展能力

| 接口 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| GetStorageStats | GET | ✅ | 用量统计（文件数/总容量/分桶明细） |
| GetStorageTrend | GET | ✅ | 用量趋势（每日快照，默认 30 天） |
| GetFileThumb | GET | ✅ | 图片缩略图（JPEG dataURL；缺失时按需自愈生成） |
| DownloadStream | POST | ✅ | 流式下载（64KB 分块 server-streaming，大文件不驻内存） |
| GetPresignedURL | GET | ✅ | 预签名下载 URL（本地驱动返回 40005，前端自动回落；有效期钳制 7 天） |

#### 回收站

| 接口 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| ListDeletedFiles | GET | ✅ | 回收站列表（已删文件） |
| RestoreFiles | POST | ✅ | 批量还原 |
| PurgeFiles | POST | ✅ | 批量彻底删除（级联分享/归还配额） |

#### 分享审计

| 接口 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| GetShareAccessLogs | GET | ✅ | 我的分享访问/下载审计（倒序分页） |

### 管理后台（`/storage.User`，需 admin 角色）

| 接口 | 方法 | 说明 |
|------|------|------|
| AdminListUsers | GET | 用户列表（关键词搜索 + 文件用量聚合） |
| AdminSetUserRole | POST | 设置角色（user/admin，禁改自己） |
| AdminSetUserDisabled | POST | 禁用/启用（禁用后登录与既有 token 均被拒） |
| AdminGetGlobalStats | GET | 全局统计（用户数/文件数/总容量） |
| AdminRecentAccessLogs | GET | 全局分享访问/下载审计（倒序） |
| AdminRecentOpLogs | GET | 管理操作审计（改角色/禁用/强制删除） |
| AdminPurgeFile | POST | 强制彻底删除任意文件（写审计） |

## 数据库设计

### 核心表

| 表名 | 说明 | 关键字段 |
|------|------|---------|
| `users` | 用户 | id, phone, email, nickname, password, avatar |
| `buckets` | 桶 | id, user_id, name, size_limit, used_size, visibility, version |
| `files` | 文件 | id, user_id, bucket_id, name, path, ext, content_type, size, md5 |
| `file_parts` | 分片上传记录 | id, upload_id, user_id, bucket_id, part_number, status |
| `file_shares` | 文件分享 | id, file_id, user_id, share_code, permission, expire_at, max_count, used_count |

### Redis 键

| Key 模式 | 类型 | 用途 | TTL |
|----------|------|------|-----|
| `md5:{user_id}:{md5}` | string | 秒传索引（file_id） | 30 天 |
| `upload:{upload_id}:meta` | string(JSON) | 分片上传元信息 | 24 小时 |
| `upload:{upload_id}:parts` | hash | 已上传分片（part_number → md5） | 24 小时 |
| `share:{code}:meta` | string(JSON) | 分享元信息 | 与过期时间对齐 |
| `share:{code}:counter` | string(int) | 分享已用次数（Lua 原子校验+递增） | 与过期时间对齐 |
| `sms_code_daily:{contact}:{type}` | string(int) | 验证码每日发送配额（10 条/天） | 24 小时 |
| `login_fail:{user_id}` / `login_lock:{user_id}` | string | 登录失败计数/锁定标记（5 次锁 15 分钟） | 15 分钟 |

## 开发与贡献

### 代码生成

```bash
# 生成 ent 代码（修改 schema 后执行）
cd cloud/ent
go generate ./...

# 生成 protobuf 代码（修改 proto 后执行）
cd cloud/proto
generate_all.cmd

# 更新 gateway protoset
protoc --descriptor_set_out=../gateway/protoset.pb \
  --include_imports base.proto file.proto user.proto bucket.proto service.proto
```

### 编码规范

- **命名**：类名大驼峰（PascalCase），函数/变量/文件名蛇形（snake_case），全局常量全大写下划线
- **注释**：中文，`# ` 后跟 1 个空格，与代码缩进对齐
- **错误处理**：关键路径使用 `response.ErrWithCode(code, msg)` 携带规范错误码
- **日志**：使用 `logx.WithContext(ctx)`，业务字段以 `key=value` 形式记录
- **代码修改后同步更新对应 md 文档**（workspace 规则）

### 项目约定

1. 所有实体 ID 使用**雪花 ID**（字符串形式），非自增主键
2. 乐观锁通过 `version` 字段实现（桶容量更新等并发场景）
3. 存储操作通过 `Storage Driver` 接口，不直接耦合具体后端
4. 鉴权通过 gRPC 拦截器统一处理，白名单在 `middleware/jwt_middleware.go` 配置
5. 响应统一使用 `response.Success` / `response.ErrWithCode`，不直接返回裸 error

## 文档导航

完整文档位于 [doc/](doc/) 目录：

| 文档 | 说明 |
|------|------|
| [01_现状分析.md](doc/01_现状分析.md) | 项目初始状态与已知问题 |
| [02_架构设计.md](doc/02_架构设计.md) | 整体架构与分层设计 |
| [03_数据库设计.md](doc/03_数据库设计.md) | 表结构、索引、Redis 键设计 |
| [04_存储引擎设计.md](doc/04_存储引擎设计.md) | 存储驱动抽象与后端实现 |
| [05_API接口设计.md](doc/05_API接口设计.md) | 完整 API 定义与 REST 映射 |
| [06_开发任务清单.md](doc/06_开发任务清单.md) | 分阶段任务与进度跟踪 |
| [07_P5_高级能力设计.md](doc/07_P5_高级能力设计.md) | 秒传 / 分片上传 / 文件分享详细设计 |
| [08_P6_健壮性改造设计.md](doc/08_P6_健壮性改造设计.md) | bcrypt / 数据迁移 / 错误码 / 日志详细设计 |
| [09_部署指南.md](doc/09_部署指南.md) | 本地部署、Docker Compose 部署、生产检查清单 |
| [10_配置说明.md](doc/10_配置说明.md) | 全量配置键位说明（服务/网关/Web） |
| [11_P7_安全与健壮性修复设计.md](doc/11_P7_安全与健壮性修复设计.md) | 验证码链路 / 路径安全 / 配额与事务修复设计 |
| [12_P8_前端完善设计.md](doc/12_P8_前端完善设计.md) | Web 前端 Vue 3 本地化改造与功能补齐设计 |
| [13_夜间自主开发路线图.md](doc/13_夜间自主开发路线图.md) | M1-M12 增量能力路线图（流式下载/缩略图/趋势/回收站/分享审计/限流等） |
| [14_管理后台设计.md](doc/14_管理后台设计.md) | 管理后台（角色模型/用户管理/全局统计/操作审计）设计 |
| [15_项目交接文档.md](doc/15_项目交接文档.md) | ⭐ **交接入口**：新人路线/代码导读/设计不变式/陷阱清单/技术债 |
| [17_项目亮点与面试指南.md](doc/17_项目亮点与面试指南.md) | 技术决策讲点化：8 条不变式「问题→方案→证据」、降级矩阵取舍、E2E 抓 5 真实缺陷故事、高频追问预演 |

## License

[MIT](LICENSE)
