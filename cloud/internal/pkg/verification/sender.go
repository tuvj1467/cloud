package verification

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"
)

// Sender 验证码发送通道抽象
// 当前实现 LogSender（日志输出）；将来接 SMTP/短信服务商时新增实现并在配置中选择
type Sender interface {
	Send(ctx context.Context, contact string, typ int, code string) error
}

// LogSender 日志通道实现：不真实发送，仅写日志
// dev 模式输出完整验证码便于联调；其他模式仅输出脱码——
// 生产日志若明文记录验证码，任何能读日志的人都可对已知手机号/邮箱
// 发起重置密码并从日志取码接管账号
type LogSender struct {
	DevMode bool
}

// NewLogSender 按服务运行模式构造（mode=="dev" 时输出完整验证码）
func NewLogSender(mode string) LogSender {
	return LogSender{DevMode: mode == "dev"}
}

func (s LogSender) Send(ctx context.Context, contact string, typ int, code string) error {
	shown := "******"
	if s.DevMode {
		shown = code
	}
	logx.WithContext(ctx).Infof("[verification] 验证码已生成：%s (联系方式：%s, 类型：%d)", shown, contact, typ)
	return nil
}
