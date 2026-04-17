package util

import (
	"crypto/md5"
	"encoding/hex"
)

// CalculateFileMD5 计算字节数组的MD5值
func CalculateFileMD5(data []byte) string {
	hash := md5.Sum(data)
	// 转为32位小写字符串
	return hex.EncodeToString(hash[:])
}
