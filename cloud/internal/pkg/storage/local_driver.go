package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// LocalDriver 本地磁盘存储驱动实现
type LocalDriver struct {
	rootDir string // 存储根目录
}

// NewLocalDriver 创建本地磁盘驱动实例
func NewLocalDriver(rootDir string) *LocalDriver {
	return &LocalDriver{rootDir: rootDir}
}

// absPath 将相对路径转为绝对路径，防止路径穿越
func (d *LocalDriver) absPath(path string) string {
	return filepath.Join(d.rootDir, path)
}

// Put 写入本地文件
func (d *LocalDriver) Put(ctx context.Context, path string, reader io.Reader, size int64) error {
	fullPath := d.absPath(path)
	// 确保父目录存在
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()
	// 拷贝流到文件
	if _, err := io.Copy(f, reader); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// Get 读取本地文件，返回可读流
func (d *LocalDriver) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	fullPath := d.absPath(path)
	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	return f, nil
}

// Delete 删除本地文件
func (d *LocalDriver) Delete(ctx context.Context, path string) error {
	fullPath := d.absPath(path)
	err := os.Remove(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrFileNotFound
		}
		return fmt.Errorf("删除文件失败: %w", err)
	}
	return nil
}

// Stat 获取文件信息
func (d *LocalDriver) Stat(ctx context.Context, path string) (*FileInfo, error) {
	fullPath := d.absPath(path)
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}
	return &FileInfo{
		Size:         info.Size(),
		LastModified: info.ModTime().Unix(),
	}, nil
}

// Exists 判断文件是否存在
func (d *LocalDriver) Exists(ctx context.Context, path string) (bool, error) {
	fullPath := d.absPath(path)
	_, err := os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("检查文件存在失败: %w", err)
}

// InitPart 初始化分片上传：创建临时目录，返回 upload_id
// 本地实现用时间戳+随机后缀作为 upload_id，临时分片存于 {root}/.tmp/{upload_id}/
func (d *LocalDriver) InitPart(ctx context.Context, path string, totalParts int) (string, error) {
	uploadID := strconv.FormatInt(generateUploadID(), 10)
	tmpDir := filepath.Join(d.rootDir, ".tmp", uploadID)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("创建分片临时目录失败: %w", err)
	}
	return uploadID, nil
}

// UploadPart 上传单个分片，写入 {root}/.tmp/{upload_id}/part_{N}
func (d *LocalDriver) UploadPart(ctx context.Context, uploadID string, partNumber int, reader io.Reader, size int64) (string, error) {
	if partNumber < 1 {
		return "", errors.New("分片序号必须大于 0")
	}
	partPath := filepath.Join(d.rootDir, ".tmp", uploadID, fmt.Sprintf("part_%d", partNumber))
	f, err := os.Create(partPath)
	if err != nil {
		return "", fmt.Errorf("创建分片文件失败: %w", err)
	}
	defer f.Close()

	hash := md5.New()
	// 同时写入文件与计算 MD5
	if _, err := io.Copy(io.MultiWriter(f, hash), reader); err != nil {
		return "", fmt.Errorf("写入分片失败: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// CompletePart 合并所有分片为目标文件
func (d *LocalDriver) CompletePart(ctx context.Context, uploadID string, targetPath string) error {
	tmpDir := filepath.Join(d.rootDir, ".tmp", uploadID)
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("读取分片目录失败: %w", err)
	}

	// 按分片序号排序
	type partEntry struct {
		number int
		name   string
	}
	parts := make([]partEntry, 0, len(entries))
	for _, e := range entries {
		var num int
		_, err := fmt.Sscanf(e.Name(), "part_%d", &num)
		if err != nil {
			continue
		}
		parts = append(parts, partEntry{number: num, name: e.Name()})
	}
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].number < parts[j].number
	})
	if len(parts) == 0 {
		return errors.New("未找到任何分片")
	}

	// 创建目标文件
	fullTarget := d.absPath(targetPath)
	if err := os.MkdirAll(filepath.Dir(fullTarget), 0755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}
	target, err := os.Create(fullTarget)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer target.Close()

	// 依次拼接分片
	for _, p := range parts {
		partFile, err := os.Open(filepath.Join(tmpDir, p.name))
		if err != nil {
			return fmt.Errorf("打开分片 %d 失败: %w", p.number, err)
		}
		if _, err := io.Copy(target, partFile); err != nil {
			partFile.Close()
			return fmt.Errorf("合并分片 %d 失败: %w", p.number, err)
		}
		partFile.Close()
	}
	return nil
}

// AbortPart 取消分片上传，清理临时目录
func (d *LocalDriver) AbortPart(ctx context.Context, uploadID string) error {
	tmpDir := filepath.Join(d.rootDir, ".tmp", uploadID)
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("清理分片临时目录失败: %w", err)
	}
	return nil
}

// PresignURL 本地驱动不支持预签名
func (d *LocalDriver) PresignURL(ctx context.Context, path string, expire int64, method string) (string, error) {
	return "", ErrNotImplemented
}
