package response

// 错误码常量定义
// 段位规划：
//
//	10000-10999  用户与认证
//	20000-20999  桶服务
//	30000-30999  文件服务
//	40000-40999  存储驱动
//	50000-50999  系统级（DB/Redis/未知）
const (
	// ====== 通用成功 ======
	CodeSuccess = 200

	// ====== 用户与认证 10000-10999 ======
	CodeUserNotFound        = 10001 // 用户不存在
	CodeUserPasswordWrong   = 10002 // 密码错误
	CodeUserDuplicate       = 10003 // 用户名/手机/邮箱重复
	CodeUserSmsCodeWrong    = 10004 // 验证码错误
	CodeUserSmsCodeExpired  = 10005 // 验证码已过期
	CodeTokenInvalid        = 10101 // token 无效
	CodeTokenExpired        = 10102 // token 已过期
	CodeTokenMissing        = 10103 // 缺少 token
	CodeUserSmsCodeFrequent = 10006 // 验证码发送过于频繁
	CodeUserSmsCodeService  = 10007 // 验证码服务不可用
	CodeUserDisabled        = 10008 // 账号已被禁用

	// ====== 桶服务 20000-20999 ======
	CodeBucketNotFound     = 20001 // 桶不存在
	CodeBucketDuplicate    = 20002 // 桶名重复
	CodeBucketNoPermission = 20003 // 无权操作该桶
	CodeBucketCapacity     = 20004 // 桶容量不足
	CodeBucketNotEmpty     = 20005 // 桶非空（删除时）

	// ====== 文件服务 30000-30999 ======
	CodeFileNotFound       = 30001 // 文件不存在
	CodeFileNoPermission   = 30002 // 无权操作该文件
	CodeFileDuplicateName  = 30003 // 同桶内文件名重复
	CodeFilePartIncomplete = 30010 // 分片不完整
	CodeFilePartNotFound   = 30011 // 分片记录不存在
	CodeFileShareExpired   = 30020 // 分享已过期
	CodeFileShareExhausted = 30021 // 分享访问次数已用尽
	CodeFileShareNotFound  = 30022 // 分享不存在
	CodeFileTooLarge       = 30012 // 文件体积超过上限

	// ====== 存储驱动 40000-40999 ======
	CodeStorageInit        = 40001 // 存储驱动初始化失败
	CodeStorageWrite       = 40002 // 文件写入失败
	CodeStorageRead        = 40003 // 文件读取失败
	CodeStorageDelete      = 40004 // 文件删除失败
	CodeStorageNotImpl     = 40005 // 不支持的操作（如本地驱动预签名）
	CodeStoragePathInvalid = 40006 // 存储路径非法（防穿越）

	// ====== 管理后台 40300-40399 ======
	CodeAdminDenied = 40300 // 需要管理员权限

	// ====== 限流 42000-42999 ======
	CodeTooManyRequests = 42900 // 请求过于频繁（业务层限流；gRPC 层为 HTTP 429）

	// ====== 系统级 50000-50999 ======
	CodeSystemDB       = 50001 // 数据库错误
	CodeSystemRedis    = 50002 // Redis 错误
	CodeSystemInternal = 50000 // 内部错误（兜底）
)
