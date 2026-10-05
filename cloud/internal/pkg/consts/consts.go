// Package consts 全局业务常量（跨层共用，杜绝魔法数字多处漂移）
package consts

// DefaultBucketSizeLimit 新桶默认容量上限 1GB
// （ent schema 默认值与 repository 创建兜底共用同一来源）
const DefaultBucketSizeLimit = int64(1024 * 1024 * 1024)
