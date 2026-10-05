package userlogic

import (
	"context"
	"errors"

	"onecloud/ent/user"
	"onecloud/internal/pkg/response"
	"onecloud/internal/pkg/verification"
	"onecloud/internal/svc"
	"onecloud/proto/.proto/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type SendVerificationCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSendVerificationCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendVerificationCodeLogic {
	return &SendVerificationCodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// SendVerificationCode 发送验证码
// 防护：60 秒频率限制 + 单联系方式/类型每日 10 条配额（组件内实现）；
// LOGIN / RESET_PASSWORD 类型仅对已注册联系方式发送（REGISTER 由注册流程自行校验重复），
// 未注册按「发送成功」静默处理——不向探测者泄露手机号/邮箱是否注册
func (l *SendVerificationCodeLogic) SendVerificationCode(in *service.SendVerificationCodeReq) (*service.Response, error) {
	// 1. 确定联系方式（oneof：手机号或邮箱二选一）
	contact := in.GetPhone()
	if contact == "" {
		contact = in.GetEmail()
	}
	if contact == "" {
		return response.Err(errors.New("手机号或邮箱不能为空"))
	}

	// 2. 校验验证码类型（0 为未知类型，拒绝发送）
	if in.Type == service.SmsType_SMS_TYPE_UNKNOWN {
		return response.Err(errors.New("无效的验证码类型"))
	}

	// 3. 登录/重置密码：仅向已注册联系方式发送；未注册静默成功（防账号枚举）
	if in.Type == service.SmsType_SMS_TYPE_LOGIN || in.Type == service.SmsType_SMS_TYPE_RESET_PASSWORD {
		q := l.svcCtx.EntClient.User.Query()
		if contact == in.GetPhone() {
			q = q.Where(user.PhoneEQ(contact))
		} else {
			q = q.Where(user.EmailEQ(contact))
		}
		exist, err := q.Exist(l.ctx)
		if err != nil {
			l.Errorf("查询注册状态失败 contact=%s type=%d: %v", contact, in.Type, err)
			return response.ErrWithCode(response.CodeUserSmsCodeService, "验证码发送失败")
		}
		if !exist {
			l.Infof("验证码发送目标未注册（静默）contact=%s type=%d", contact, in.Type)
			return response.Success(&service.SendVerificationCodeRes{Success: true})
		}
	}

	// 4. 生成 + 存储 + 发送
	if err := l.svcCtx.Verifier.Send(l.ctx, contact, int(in.Type)); err != nil {
		switch {
		case errors.Is(err, verification.ErrTooFrequent):
			return response.ErrWithCode(response.CodeUserSmsCodeFrequent, err.Error())
		case errors.Is(err, verification.ErrQuotaExceeded):
			return response.ErrWithCode(response.CodeUserSmsCodeFrequent, err.Error())
		case errors.Is(err, verification.ErrUnavailable):
			return response.ErrWithCode(response.CodeUserSmsCodeService, "验证码服务不可用")
		default:
			l.Logger.Errorf("发送验证码失败 contact=%s type=%d: %v", contact, in.Type, err)
			return response.ErrWithCode(response.CodeUserSmsCodeService, "验证码发送失败")
		}
	}

	return response.Success(&service.SendVerificationCodeRes{Success: true})
}
