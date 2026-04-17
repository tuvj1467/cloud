package types

const (
	// 成功
	OK = 0

	// 通用错误
	ErrInternal     = 10001
	ErrInvalidParam = 10002
	ErrNotFound     = 10003

	// 业务相关错误
	ErrFileNotFound = 20001
	ErrUserNotFound = 20002
)

var errorMsg = map[int]string{
	OK:              "success",
	ErrInternal:     "内部服务器错误",
	ErrInvalidParam: "参数错误",
	ErrNotFound:     "资源不存在",
	ErrFileNotFound: "文件不存在",
	ErrUserNotFound: "用户不存在",
}

func GetErrorMsg(code int) string {
	if msg, ok := errorMsg[code]; ok {
		return msg
	}
	return "未知错误"
}
