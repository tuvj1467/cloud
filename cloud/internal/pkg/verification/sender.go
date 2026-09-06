package verification

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"
)

// Sender 验证码发送通道抽象
// 当前实现 LogSender（开发环境日志输出）；将来接 SMTP/短信服务商时新增实现并在配置中选择
type Sender interface {
	Send(ctx context.Context, contact string, typ int, code string) error
}

// LogSender 开发环境实现：仅写日志，不真实发送
type LogSender struct{}

func (LogSender) Send(ctx context.Context, contact string, typ int, code string) error {
	logx.WithContext(ctx).Infof("[verification] 验证码已生成：%s (联系方式：%s, 类型：%d)", code, contact, typ)
	return nil
}
