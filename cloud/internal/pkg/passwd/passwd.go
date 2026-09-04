// Package passwd 密码加密与校验工具
// 使用 bcrypt 算法，单向不可逆
package passwd

import "golang.org/x/crypto/bcrypt"

// Hash 使用 bcrypt 加密明文密码
// cost=bcrypt.DefaultCost（10），单次约 60ms，安全性与性能平衡
func Hash(plain string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// Compare 校验明文密码与 bcrypt 哈希是否匹配
// 匹配返回 nil，不匹配返回非 nil error
func Compare(hashed, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
}

// IsHashed 判断字符串是否为 bcrypt 哈希格式（$2a$/$2b$/$2y$ 开头，长度 60）
// 用于存量数据迁移时区分明文与已加密密码
func IsHashed(s string) bool {
	if len(s) != 60 {
		return false
	}
	return s[:4] == "$2a$" || s[:4] == "$2b$" || s[:4] == "$2y$"
}
