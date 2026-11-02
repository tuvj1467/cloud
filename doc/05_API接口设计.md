# 05 API接口设计

> **状态注记（2026-09-12 刷新）**：本文已刷新为**现行接口全集**（原设计期措辞保留于历史章节供追溯）。当前共 **46 个 REST 端点**（用户 7 + 桶 5 + 文件 10 + 分片 4 + 分享 6 + 回收站 3 + 统计/趋势 2 + 审计 1 + 缩略图 1 + 预签名 1 + 管理后台 7；与 service.proto 的 46 个 rpc、网关 46 条映射一致），路由唯一真源为 `gateway/gateway.yaml`（与 `deploy/gateway/gateway.yaml` 保持同步），汇总表见根 [README.md](../README.md) 的「API 总览」。**鉴权**：除白名单（注册/登录系列/发送验证码/重置密码/找回账号/分享查看/分享下载含流式）外全部需 JWT。**错误码**：200 成功；10003 用户名重复 / 10004-10007 验证码系列 / 20004 桶容量不足 / 20005 桶非空 / 30001-30022 文件与分享系列 / 30012 文件超限 / 40005 驱动不支持 / 40006 路径非法 / 42900 限流（HTTP 层为 429）。**通用约定**：请求/响应 JSON，字段 camelCase；int64 序列化为字符串；分页统一 page/page_size（≤50）。

## 一、服务总览

| gRPC 服务 | 说明 | 当前状态 |
| --------- | ---- | -------- |
| `storage.User` | 用户服务 | 已实现 |
| `storage.File` | 文件服务 | 部分实现，需补全 |
| `storage.Bucket` | 桶服务 | 新增 |

## 二、proto 文件规划

```
cloud/proto/
├── base.proto        # 通用消息（保持，补充错误码相关）
├── validate.proto    # 校验规则（保持）
├── user.proto        # 用户消息（保持）
├── file.proto        # 文件消息（扩展分片、秒传、分享）
├── bucket.proto      # 桶消息（新增）
└── service.proto     # 服务定义（聚合，新增 Bucket 服务）
```

## 三、Bucket 服务接口（新增 `bucket.proto`）

```proto
syntax = "proto3";
package storage;
option go_package = ".proto/service";
import "validate.proto";
import "base.proto";

// 桶元信息
message BucketMeta {
  string bucket_id = 1;
  string name = 2;
  string user_id = 3;
  string description = 4;
  int64 size_limit = 5;
  int64 used_size = 6;
  string visibility = 7;       // private / internal / public
  string created_at = 8;
  string updated_at = 9;
}

// 1. 创建桶
message CreateBucketRequest {
  string name = 1 [(validate.rules).string = {min_len:1, max_len:63}];
  optional string description = 2;
  int64 size_limit = 3;        // 0 表示使用默认值 1GB
  string visibility = 4 [(validate.rules).string = {in: ["private", "internal", "public"], ignore_empty: true}]; // 可空,空=默认 private
}
message CreateBucketResponse {
  BucketMeta bucket = 1;
}

// 2. 获取桶列表
message BucketListRequest {
  int32 page = 1 [(validate.rules).int32 = {gte: 1}];
  int32 page_size = 2 [(validate.rules).int32 = {gte:1, lte:50}];
}
message BucketListResponse {
  repeated BucketMeta buckets = 1;
  int32 total = 2;
}

// 3. 获取桶详情
message BucketInfoRequest {
  string bucket_id = 1 [(validate.rules).string.min_len = 1];
}
message BucketInfoResponse {
  BucketMeta bucket = 1;
}

// 4. 更新桶
message UpdateBucketRequest {
  string bucket_id = 1 [(validate.rules).string.min_len = 1];
  optional string description = 2;
  optional int64 size_limit = 3;
  optional string visibility = 4;
}
message UpdateBucketResponse {
  bool success = 1;
}

// 5. 删除桶（需桶为空）
message DeleteBucketRequest {
  string bucket_id = 1 [(validate.rules).string.min_len = 1];
  bool force = 2;  // 强制删除（含文件）
}
message DeleteBucketResponse {
  bool success = 1;
}
```

### Bucket 服务定义（在 `service.proto` 追加）

```proto
service Bucket {
  rpc CreateBucket(CreateBucketRequest) returns (Response);
  rpc GetBucketList(BucketListRequest) returns (Response);
  rpc GetBucketInfo(BucketInfoRequest) returns (Response);
  rpc UpdateBucket(UpdateBucketRequest) returns (Response);
  rpc DeleteBucket(DeleteBucketRequest) returns (Response);
}
```

## 四、File 服务接口扩展（`file.proto`）

### 4.1 修正 `UploadFileRequest` 增加桶归属

```proto
message UploadFileRequest {
  string bucket_id = 1 [(validate.rules).string.min_len = 1]; // 新增：所属桶
  string name = 2 [(validate.rules).string = {min_len:1, max_len:100}];
  string suffix = 3 [(validate.rules).string = {min_len:1}];
  int64 size = 4 [(validate.rules).int64 = {gt: 0}];
  bytes content = 5;
  optional string md5 = 6;     // 新增：客户端可传 MD5 触发秒传
}
```

### 4.2 新增秒传响应

```proto
message UploadFileResponse {
  FileMeta file = 1;
  bool instant = 2;  // true 表示秒传命中
}
```

### 4.3 修正 `GetBucketFileList` 入参

```proto
// service.proto 中修正
rpc GetBucketFileList(BucketFileListRequest) returns (Response);
```

### 4.4 新增分片上传接口

```proto
// 1. 初始化分片上传
message InitMultipartUploadRequest {
  string bucket_id = 1 [(validate.rules).string.min_len = 1];
  string name = 2 [(validate.rules).string = {min_len:1, max_len:100}];
  string suffix = 3 [(validate.rules).string = {min_len:1}];
  int64 size = 4 [(validate.rules).int64 = {gt: 0}];
  int32 total_parts = 5 [(validate.rules).int32 = {gte:1, lte:10000}];
}
message InitMultipartUploadResponse {
  string upload_id = 1;
  int32 part_size = 2;  // 建议每片大小
}

// 2. 上传分片
message UploadPartRequest {
  string upload_id = 1 [(validate.rules).string.min_len = 1];
  int32 part_number = 2 [(validate.rules).int32 = {gte:1}];
  bytes content = 3;
  int64 size = 4 [(validate.rules).int64 = {gt: 0}];
}
message UploadPartResponse {
  bool success = 1;
  string part_md5 = 2;
}

// 3. 完成分片上传（合并）
message CompleteMultipartUploadRequest {
  string upload_id = 1 [(validate.rules).string.min_len = 1];
}
message CompleteMultipartUploadResponse {
  FileMeta file = 1;
}

// 4. 取消分片上传
message AbortMultipartUploadRequest {
  string upload_id = 1 [(validate.rules).string.min_len = 1];
}
message AbortMultipartUploadResponse {
  bool success = 1;
}
```

### 4.4.1 列表排序参数（2026-10-31 语义刷新）

`FileListRequest` / `BucketFileListRequest` 的排序字段均为**可选**（`ignore_empty: true`）：

| 字段 | 取值 | 缺省行为 |
| ---- | ---- | -------- |
| `sort_by` | `name` / `size` / `create_time` / `update_time` | 空 = 按 `update_time` 倒序（服务端 `applyFileSort` 默认分支） |
| `sort_order` | `asc` / `desc` | 空 = 倒序 |

> 历史注记：早期版本空值会触发 validate `in` 校验失败（「参数校验失败」），经 E2E 冒烟
> 发现后补 `ignore_empty` 修复——新增同类枚举参数务必带 `ignore_empty: true`。

### 4.5.1 分享流式下载（2026-10-05 新增）

```
POST /file/share/download_stream          # 匿名（白名单）
Body: { "share_code": "xxxxxxxx" }
```

server-streaming（串联 JSON 线格式，与 `POST /file/download_stream` 一致）：
每个 64KB 分块一个 `{data(base64), sent, total}` 对象。校验/计数语义与一元
`GET /file/share/download` 完全一致（过期 / 次数上限 / permission=read 拒绝），
但物理文件按块推送，大文件不再整文件驻内存。前端分享页流式优先、一元回落。

同批行为收敛：
- `permission=read` 的分享两个下载端点均返回 30002「仅允许查看」（原实现可下载）
- 文件进入回收站后：不可分享、不可被分享下载、一元/流式下载与复制/缩略图均按不存在处理
- 一元 `GET /file/download` 增加 5MB 上限，超限返回 30012 并提示走流式端点
- 一元直传 `POST /file/upload` 服务端限幅 `Storage.DirectUploadSize`（默认 5MB），超限返回 30012 引导分片通道
- 预签名 `GET /file/presign` 有效期钳制 [1min, 7d]，下载 URL 强制 attachment 头

### 4.5 新增文件分享接口

```proto
message CreateShareRequest {
  string file_id = 1 [(validate.rules).string.min_len = 1];
  string permission = 2 [(validate.rules).string = {in: ["read", "download"]}];
  optional int64 expire_seconds = 3;  // 空=永久
  optional int32 max_count = 4;       // 空=不限
}
message CreateShareResponse {
  string share_code = 1;
  string share_url = 2;
}

message AccessShareRequest {
  string share_code = 1 [(validate.rules).string.min_len = 1];
}
message AccessShareResponse {
  FileMeta file = 1;
  string permission = 2;
}
```

### 4.6 新增文件复制/移动

```proto
message CopyFileRequest {
  string file_id = 1 [(validate.rules).string.min_len = 1];
  string target_bucket_id = 2 [(validate.rules).string.min_len = 1];
  string new_name = 3 [(validate.rules).string = {min_len:1, max_len:100}];
}
message CopyFileResponse {
  FileMeta file = 1;
}
```

### 4.7 File 服务完整定义（`service.proto`）

```proto
service File {
  // 基础操作
  rpc GetFileInfo(FileInfoRequest) returns (Response);
  rpc UploadFile(UploadFileRequest) returns (Response);
  rpc GetFileList(FileListRequest) returns (Response);
  rpc GetBucketFileList(BucketFileListRequest) returns (Response);
  rpc RenameFile(RenameFileRequest) returns (Response);
  rpc DeleteFile(DeleteFileRequest) returns (Response);
  rpc DownloadFile(DownloadFileRequest) returns (Response);
  rpc CopyFile(CopyFileRequest) returns (Response);

  // 分片上传
  rpc InitMultipartUpload(InitMultipartUploadRequest) returns (Response);
  rpc UploadPart(UploadPartRequest) returns (Response);
  rpc CompleteMultipartUpload(CompleteMultipartUploadRequest) returns (Response);
  rpc AbortMultipartUpload(AbortMultipartUploadRequest) returns (Response);

  // 分享
  rpc CreateShare(CreateShareRequest) returns (Response);
  rpc AccessShare(AccessShareRequest) returns (Response);
}
```

## 五、REST 路由映射（`gateway.yaml` 扩展）

```yaml
upstreams:
  - Name: storage.rpc
    Grpc:
      target: 127.0.0.1:8081
      schema: ""
    ProtoSet: protoset.pb
    Mappings:
      # 文件
      - Method: POST
        Path: /file/info
        RpcPath: storage.File/GetFileInfo
      - Method: POST
        Path: /file/upload
        RpcPath: storage.File/UploadFile
      - Method: GET
        Path: /file/list
        RpcPath: storage.File/GetFileList
      - Method: GET
        Path: /file/bucket_list
        RpcPath: storage.File/GetBucketFileList
      - Method: POST
        Path: /file/rename
        RpcPath: storage.File/RenameFile
      - Method: POST
        Path: /file/delete
        RpcPath: storage.File/DeleteFile
      - Method: GET
        Path: /file/download
        RpcPath: storage.File/DownloadFile
      - Method: POST
        Path: /file/copy
        RpcPath: storage.File/CopyFile
      # 分片上传
      - Method: POST
        Path: /file/multipart/init
        RpcPath: storage.File/InitMultipartUpload
      - Method: POST
        Path: /file/multipart/upload
        RpcPath: storage.File/UploadPart
      - Method: POST
        Path: /file/multipart/complete
        RpcPath: storage.File/CompleteMultipartUpload
      - Method: POST
        Path: /file/multipart/abort
        RpcPath: storage.File/AbortMultipartUpload
      # 分享
      - Method: POST
        Path: /file/share/create
        RpcPath: storage.File/CreateShare
      - Method: GET
        Path: /file/share/access
        RpcPath: storage.File/AccessShare
      # 桶
      - Method: POST
        Path: /bucket/create
        RpcPath: storage.Bucket/CreateBucket
      - Method: GET
        Path: /bucket/list
        RpcPath: storage.Bucket/GetBucketList
      - Method: GET
        Path: /bucket/info
        RpcPath: storage.Bucket/GetBucketInfo
      - Method: POST
        Path: /bucket/update
        RpcPath: storage.Bucket/UpdateBucket
      - Method: POST
        Path: /bucket/delete
        RpcPath: storage.Bucket/DeleteBucket
      # 用户（保持现有）
      - Method: POST
        Path: /user/register
        RpcPath: storage.User/Register
```

> proto 变更后必须重新生成：
> 1. `cd cloud/proto && generate_all.cmd` 生成 pb 代码
> 2. `protoc --descriptor_set_out=gateway/protoset.pb --include_imports *.proto` 刷新网关 protoset

## 六、JWT 白名单修正

公开接口（无需鉴权）：

```go
var publicMethods = []string{
    // 用户认证相关
    "/storage.User/Register",
    "/storage.User/LoginByAccount",
    "/storage.User/LoginByPhoneSms",
    "/storage.User/LoginByEmailSms",
    "/storage.User/SendVerificationCode",
    "/storage.User/ResetPassword",
    "/storage.User/FindAccount",
    // 文件分享访问（匿名可访问）
    "/storage.File/AccessShare",
    // 分享下载（匿名可访问）
    "/storage.File/DownloadByShare",
    // 分享流式下载（匿名可访问）
    "/storage.File/DownloadByShareStream",
}
```

其余所有 `File` / `Bucket` 接口均需鉴权；流式方法另由 `JwtStreamInterceptor` 鉴权（同样引用此白名单）。

实现位置：[middleware/jwt_middleware.go](../cloud/internal/middleware/jwt_middleware.go)
