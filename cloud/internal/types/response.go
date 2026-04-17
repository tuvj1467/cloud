package types

// 统一响应结构
type Response struct {
	Code    int         `json:"code"`           // 状态码
	Message string      `json:"message"`        // 响应消息
	Data    interface{} `json:"data,omitempty"` // 响应数据
}

// 成功响应
func Success(data interface{}) *Response {
	return &Response{
		Code:    0,
		Message: "success",
		Data:    data,
	}
}

// 错误响应
func Error(code int, message string) *Response {
	return &Response{
		Code:    code,
		Message: message,
		Data:    nil,
	}
}
