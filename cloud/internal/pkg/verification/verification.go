package verification

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// 语义化错误，logic 层据此映射 response 错误码
var (
	ErrCodeWrong   = errors.New("验证码错误")
	ErrCodeExpired = errors.New("验证码已过期")
	ErrTooFrequent = errors.New("验证码发送过于频繁，请稍后再试")
	ErrQuotaExceeded = errors.New("今日发送次数已达上限，请明日再试")
)

const (
	codeTTL      = 5 * time.Minute  // 验证码有效期
	limitTTL     = 60 * time.Second // 发送频率限制窗口
	maxFailCount = 5                // 最大验证失败次数，达到即作废验证码
	// maxSendsPerDay 单联系方式+类型每日发送上限（60s 窗口只防短时连击，
	// 不防 3 类型×全天轰炸；日级配额封堵持续轰炸与短信费用攻击面）
	maxSendsPerDay = 10
	dayTTL         = 24 * time.Hour
)

// Service 验证码服务
type Service struct {
	kv     KV
	sender Sender
}

// NewService 创建验证码服务
func NewService(kv KV, sender Sender) *Service {
	return &Service{kv: kv, sender: sender}
}

// key 规范唯一定义处：sms_code:{contact}:{type}，type 与 proto 的 SmsType 枚举一致
func codeKey(contact string, typ int) string {
	return fmt.Sprintf("sms_code:%s:%d", contact, typ)
}

func limitKey(contact string, typ int) string {
	return fmt.Sprintf("sms_code_limit:%s:%d", contact, typ)
}

func failKey(contact string, typ int) string {
	return fmt.Sprintf("sms_code_fail:%s:%d", contact, typ)
}

func dailyKey(contact string, typ int) string {
	return fmt.Sprintf("sms_code_daily:%s:%d", contact, typ)
}

// Send 生成并“发送”验证码（60 秒频率限制 + 每日 10 条配额，5 分钟有效期）
func (s *Service) Send(ctx context.Context, contact string, typ int) error {
	ok, err := s.kv.SetNX(ctx, limitKey(contact, typ), "1", limitTTL)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return ErrUnavailable
		}
		return fmt.Errorf("频率限制检查失败: %w", err)
	}
	if !ok {
		return ErrTooFrequent
	}

	// 日级配额（原子自增 + TTL；键为永久历史残留时由 Incr 补 TTL 自愈）
	n, err := s.kv.Incr(ctx, dailyKey(contact, typ), dayTTL)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return ErrUnavailable
		}
		return fmt.Errorf("日配额检查失败: %w", err)
	}
	if n > maxSendsPerDay {
		return ErrQuotaExceeded
	}

	code, err := randomCode()
	if err != nil {
		return fmt.Errorf("生成验证码失败: %w", err)
	}
	if err := s.kv.Set(ctx, codeKey(contact, typ), code, codeTTL); err != nil {
		return fmt.Errorf("存储验证码失败: %w", err)
	}
	return s.sender.Send(ctx, contact, typ, code)
}

// Verify 校验验证码：不匹配则失败计数（达 5 次作废）；匹配即一次性消费，防重放
func (s *Service) Verify(ctx context.Context, contact string, typ int, code string) error {
	stored, err := s.kv.Get(ctx, codeKey(contact, typ))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrCodeExpired
		}
		if errors.Is(err, ErrUnavailable) {
			return ErrUnavailable
		}
		return fmt.Errorf("查询验证码失败: %w", err)
	}
	if stored != code {
		n, _ := s.kv.Incr(ctx, failKey(contact, typ), codeTTL)
		if n >= maxFailCount {
			_ = s.kv.Del(ctx, codeKey(contact, typ))
			_ = s.kv.Del(ctx, failKey(contact, typ))
		}
		return ErrCodeWrong
	}
	// 一次性消费：验证成功立即作废
	_ = s.kv.Del(ctx, codeKey(contact, typ))
	_ = s.kv.Del(ctx, failKey(contact, typ))
	return nil
}

// randomCode 生成 6 位数字验证码（crypto/rand）
func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
