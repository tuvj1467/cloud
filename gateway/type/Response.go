package types

// 自定义 HTTP 响应结构
type Response struct {
	Code    int         `json:"code"`    // 自定义业务码（如 0 成功，404 不存在）
	Message string      `json:"message"` // 提示信息
	Data    interface{} `json:"data"`    // 业务数据
}
