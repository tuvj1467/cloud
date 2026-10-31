// Package cache 提供 Redis 缓存辅助，是全服务唯一的 Redis 访问层。
//
// 两条硬约定（违反会破坏降级语义，见 doc/15 交接文档 §5.7）：
//  1. nil 安全：client 为 nil（服务未配置 Redis）时，读方法返回零值/未命中、
//     写方法返回错误或静默跳过——调用方不得因此 panic 或中断主流程；
//     logic 层禁止直接持有 *redis.Client，必须经由本包。
//  2. 计数类操作必须原子：涉及「校验+递增」的组合一律用 Lua（见 share_cache.go /
//     kv.go 的脚本），杜绝 INCR+EXPIRE 两步写入留下的永久键。
//
// 键设计总表见 doc/03_数据库设计.md「Redis 键设计」。
package cache
