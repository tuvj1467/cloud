# ONE_CLOUD OSS 对象存储系统

> 基于 go-zero + ent + MySQL + Redis 的对象存储服务，提供桶管理、文件上传下载、秒传、分片上传、文件分享等能力。

## 技术栈

| 分类 | 技术选型 |
| ---- | -------- |
| 微服务框架 | go-zero (zrpc + gateway) |
| ORM | ent |
| 数据库 | MySQL |
| 缓存 | Redis |
| 通信协议 | gRPC + Protobuf |
| API 网关 | go-zero gateway (REST → gRPC) |
| ID 生成 | 雪花算法 (自研) |
| 鉴权 | JWT |

## 项目结构

```
ONE_CLOUD/
├── cloud/                     # 存储 RPC 服务（核心）
│   ├── main.go                # 服务入口（含存量数据迁移）
│   ├── etc/storage.yaml       # 服务配置
│   ├── proto/                 # protobuf 定义与生成脚本（base/file/user/bucket/service）
│   ├── ent/                   # ent 模型与生成代码（9 张表，含审计/快照）
│   └── internal/
│       ├── config/            # 配置定义（MySQL/Redis/Storage/JWT）
│       ├── logic/             # 业务逻辑（file / user / bucket）
│       ├── middleware/        # 中间件（JWT + 限流拦截器，含用户状态缓存）
│       ├── pkg/
│       │   ├── cache/         # Redis 缓存（md5/multipart/share）
│       │   ├── database/      # MySQL 连接
│       │   ├── jwt/           # JWT 工具
│       │   ├── migrate/       # 存量数据迁移
│       │   ├── passwd/        # bcrypt 密码工具
│       │   ├── response/      # 统一响应与错误码
│       │   ├── snowId/        # 雪花 ID 生成器
│       │   ├── storage/       # 存储驱动抽象（local/minio/s3）
│       │   └── util/          # 通用工具（MD5/share_code）
│       ├── repository/        # 数据访问层（user/bucket/file/file_part/file_share）
│       ├── server/            # gRPC server 实现（file/user/bucket）
│       └── svc/               # 服务上下文（依赖注入）
├── gateway/                   # HTTP 网关（REST → gRPC）
├── web/                       # Web 演示前端（静态页面 + API 反向代理 :8080）
├── doc/                       # 项目文档（本目录）
├── deploy/                    # 容器化部署专用配置
├── scripts/smoke_e2e.py       # E2E 冒烟脚本（35 项断言）
└── docker-compose.yml         # Docker Compose 一键部署
```

## 文档导航

| 文档 | 说明 |
| ---- | ---- |
| [01_现状分析.md](01_现状分析.md) | 当前实现盘点、已发现问题与风险清单 |
| [02_架构设计.md](02_架构设计.md) | 整体架构、分层职责、调用链路 |
| [03_数据库设计.md](03_数据库设计.md) | User / Bucket / File / 分片 / 分享 模型设计 |
| [04_存储引擎设计.md](04_存储引擎设计.md) | 存储驱动抽象层（本地 / MinIO / S3） |
| [05_API接口设计.md](05_API接口设计.md) | proto 接口定义、gRPC 与 REST 映射 |
| [06_开发任务清单.md](06_开发任务清单.md) | 分阶段任务分解与执行顺序（P1-P6） |
| [07_P5_高级能力设计.md](07_P5_高级能力设计.md) | 秒传、分片上传、文件分享详细设计 |
| [08_P6_健壮性改造设计.md](08_P6_健壮性改造设计.md) | bcrypt、数据迁移、错误码、日志详细设计 |
| [09_部署指南.md](09_部署指南.md) | 本地部署、Docker Compose 部署、生产检查清单 |
| [10_配置说明.md](10_配置说明.md) | 全量配置键位说明（服务/网关/Web） |
| [11_P7_安全与健壮性修复设计.md](11_P7_安全与健壮性修复设计.md) | 验证码链路 / 路径安全 / 配额与事务修复设计 |
| [12_P8_前端完善设计.md](12_P8_前端完善设计.md) | Web 前端 Vue 3 本地化改造与功能补齐设计 |
| [13_夜间自主开发路线图.md](13_夜间自主开发路线图.md) | M1-M12 增量能力路线图（流式下载/缩略图/趋势/回收站/审计/限流等） |
| [14_管理后台设计.md](14_管理后台设计.md) | 管理后台（角色模型/用户管理/全局统计/操作审计）设计 |
| [15_项目交接文档.md](15_项目交接文档.md) | ⭐ **交接入口**：新人路线/代码导读/设计不变式/陷阱清单/技术债 |

## 完善目标与完成状态

1. ✅ **补全文件服务**：实现下载、删除、重命名、桶内文件列表、复制
2. ✅ **补全桶服务**：Bucket CRUD、容量统计、乐观锁
3. ✅ **存储抽象**：抽离存储驱动层，支持本地磁盘（MinIO/S3 预留接口）
4. ✅ **高级能力**：MD5 秒传（Redis 索引）、分片上传与断点续传、文件分享、文件复制
5. ✅ **修复缺陷**：编译错误、JWT 白名单、proto 消息类型错配、Redis 启用
6. ✅ **可观测与健壮性**：bcrypt 密码加密、存量数据迁移、错误码 10xxx-50xxx 段、关键路径日志补全

## 执行原则

- 文档先行：代码变更同步更新对应 md 文件（遵循 workspace 规则）
- 命名规范：遵循 `.zcode/rules` 中的命名规则（PascalCase / snake_case）
- 注释规范：全部中文注释，`#` 后留 1 空格，缩进与代码一致
- 小步提交：按任务清单逐项实现，每阶段可独立验证
