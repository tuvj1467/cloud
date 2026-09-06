package storage

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrPathInvalid 路径非法（疑似路径穿越或白名单外的形态）
var ErrPathInvalid = errors.New("存储路径非法")

var (
	// 后缀白名单：字母数字 1-8 段，兼容 tar.gz 形态
	suffixRe = regexp.MustCompile(`^[A-Za-z0-9]{1,8}(\.[A-Za-z0-9]{1,8})?$`)
	// 存储标识白名单：纯数字（雪花 ID 形态），限制 upload_id / storage_id
	storageIDRe = regexp.MustCompile(`^[0-9]{1,32}$`)
)

// ValidSuffix 校验文件后缀白名单
func ValidSuffix(suffix string) bool { return suffixRe.MatchString(suffix) }

// ValidStorageID 校验 upload_id / storage_id 形态
func ValidStorageID(id string) bool { return storageIDRe.MatchString(id) }

// SafeJoin 在 root 内拼接路径元素，结果越界（穿越）即拒绝
func SafeJoin(root string, elems ...string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pAbs, err := filepath.Abs(filepath.Join(append([]string{root}, elems...)...))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, pAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathInvalid
	}
	return pAbs, nil
}
