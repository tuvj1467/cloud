package logic

import (
	"context"

	"{{.GoPackage}}/internal/svc"
	"{{.ProtoImport}}"

	"github.com/zeromicro/go-zero/core/logx"
)


type {{.Name}}_Logic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}


func New{{.Name}}_Logic(ctx context.Context, svcCtx *svc.ServiceContext) *{{.Name}}_Logic {
	return &{{.Name}}_Logic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}


func (l *{{.Name}}_Logic) {{.Name}}(req *{{.ProtoPackage}}.{{.Request}}) (resp *{{.ProtoPackage}}.{{.Response}}, err error) {
	// todo: add your logic here and delete this line
	return &{{.ProtoPackage}}.{{.Response}}{}, nil
}