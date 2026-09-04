// Package util 通用工具
// share_code.go：分享码生成
package util

import (
	"crypto/rand"
	"math/big"
)

// shareAlphabet 分享码字符集（去除易混淆字符 0/O/I/1/l）
const shareAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789abcdefghijkmnpqrstuvwxyz"

// GenerateShareCode 生成指定长度的随机分享码
// 默认长度 8 位，碰撞概率极低（54^8 ≈ 7.2e13）
func GenerateShareCode(length int) (string, error) {
	if length <= 0 {
		length = 8
	}
	result := make([]byte, length)
	max := big.NewInt(int64(len(shareAlphabet)))
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		result[i] = shareAlphabet[n.Int64()]
	}
	return string(result), nil
}
