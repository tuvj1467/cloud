package snowId

import (
	"fmt"
	"sync"
	"time"
)

// 雪花算法位运算常量定义
const (
	workerBits uint8 = 10                      // 机器 ID 位数（10 位，最多支持 1024 个节点）
	numberBits uint8 = 12                      // 序列号位数（12 位，每毫秒每个节点可生成 4096 个 ID）
	workerMax  int64 = -1 ^ (-1 << workerBits) // 机器 ID 最大值（1023）
	numberMax  int64 = -1 ^ (-1 << numberBits) // 序列号最大值（4095）
	timeLeft   uint8 = 63 - timeBits           // 时间戳在 64 位中的左移位数
	workerLeft uint8 = timeLeft - workerBits   // 机器 ID 在 64 位中的左移位数
	numberLeft uint8 = workerLeft - numberBits // 序列号在 64 位中的左移位数
	timeBits   uint8 = 41                      // 时间戳位数（41 位，可使用约 69 年）
	epoch      int64 = 1704067200000           // 起始时间戳（2024-01-01 00:00:00 UTC 的毫秒数）
)

// Snowflake 雪花 ID 生成器结构体
type Snowflake struct {
	mu       sync.Mutex // 互斥锁，保证并发安全
	workerId int64      // 机器节点 ID（0-1023）
	number   int64      // 当前毫秒内的序列号（0-4095）
	lastTime int64      // 上次生成 ID 的时间戳（毫秒）
}

var (
	Instance *Snowflake // 单例模式的雪花 ID 生成器实例
	once     sync.Once  // sync.Once 确保只初始化一次
)

// Initialize 初始化雪花 ID 生成器（全局单例）
// workerId: 机器节点 ID，范围必须在 0-1023 之间
// 返回值：初始化失败时返回错误
func Initialize(workerId int64) error {
	var err error
	once.Do(func() {
		if workerId < 0 || workerId > workerMax {
			err = fmt.Errorf("workerId 必须在 0-%d 之间", workerMax)
			return
		}
		Instance = &Snowflake{
			workerId: workerId,
			lastTime: -1, // 初始值设为 -1，表示还未生成过 ID
		}
	})
	return err
}

// Generate 生成全局唯一的雪花 ID
// 返回值：生成的 64 位整数 ID，如果生成失败则返回错误
func Generate() (int64, error) {
	if Instance == nil {
		return 0, fmt.Errorf("雪花 ID 生成器未初始化")
	}
	return Instance.NextID()
}

// NextID 生成下一个雪花 ID（线程安全）
// ID 结构：[1bit 符号位][41bit 时间戳][10bit 机器 ID][12bit 序列号]
// 返回值：生成的 64 位雪花 ID，如果时钟回拨则返回错误
func (s *Snowflake) NextID() (int64, error) {
	s.mu.Lock()         // 加锁，保证并发安全
	defer s.mu.Unlock() // 确保函数退出时释放锁

	now := time.Now().UnixMilli() // 获取当前时间的毫秒级时间戳

	// 检测时钟回拨问题：如果当前时间小于上次记录的时间，说明系统时钟发生了回拨
	if now < s.lastTime {
		return 0, fmt.Errorf("时钟回拨，等待时间同步")
	}

	// 如果在同一毫秒内，递增序列号
	if now == s.lastTime {
		s.number = (s.number + 1) & numberMax // 与操作确保序列号不溢出 12 位
		if s.number == 0 {
			// 如果序列号溢出（超过 4095），则等待下一毫秒
			now = s.waitNextMillis(now)
		}
	} else {
		// 如果是新的毫秒，重置序列号为 0
		s.number = 0
	}

	s.lastTime = now // 更新上次生成 ID 的时间戳

	// 拼接生成最终的 64 位雪花 ID
	// 格式：[时间戳部分][机器 ID 部分][序列号部分]
	id := (now-epoch)<<timeLeft |
		(s.workerId << numberLeft) |
		s.number

	return id, nil
}

// MustId 生成 ID，失败（含时钟回拨）直接 panic
// 雪花 ID 用作全局主键，静默返回错误值比崩溃更危险
func (s *Snowflake) MustId() int64 {
	id, err := s.NextID()
	if err != nil {
		panic(fmt.Errorf("雪花 ID 生成失败: %w", err))
	}
	return id
}

func (s *Snowflake) MustIdS() string {
	res := fmt.Sprintf("%d", s.MustId())
	return res
}

// waitNextMillis 等待直到下一毫秒（用于处理序列号溢出或时钟回拨）
// lastTime: 上次记录的时间戳
// 返回值：下一个毫秒的时间戳
func (s *Snowflake) waitNextMillis(lastTime int64) int64 {
	for {
		now := time.Now().UnixMilli()
		if now > lastTime {
			return now // 等到时间超过上次记录的时间后返回
		}
		time.Sleep(time.Millisecond) // 短暂休眠，避免 CPU 空转
	}
}
