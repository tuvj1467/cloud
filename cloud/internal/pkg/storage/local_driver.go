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
)

// LocalDriver 本地磁盘存储驱动实现
type LocalDriver struct {
	rootDir string // 存储根目录
}

// NewLocalDriver 创建本地磁盘驱动实例
func NewLocalDriver(rootDir string) *LocalDriver {
	return &LocalDriver{rootDir: rootDir}
}

// RootDir 返回存储根目录（缩略图等派生数据落盘定位用）
func (d *LocalDriver) RootDir() string { return d.rootDir }

// absPath 将相对路径转为绝对路径，越界（穿越）返回 ErrPathInvalid
func (d *LocalDriver) absPath(path string) (string, error) {
	return SafeJoin(d.rootDir, filepath.FromSlash(path))
}

// Put 写入本地文件
func (d *LocalDriver) Put(ctx context.Context, path string, reader io.Reader, size int64) error {
	fullPath, err := d.absPath(path)
	if err != nil {
		return err
	}
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
	fullPath, err := d.absPath(path)
	if err != nil {
		return nil, err
	}
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
	fullPath, err := d.absPath(path)
	if err != nil {
		return err
	}
	err = os.Remove(fullPath)
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
	fullPath, err := d.absPath(path)
	if err != nil {
		return nil, err
	}
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
	fullPath, err := d.absPath(path)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("检查文件存在失败: %w", err)
}

// InitPart 初始化分片上传：以业务 uploadID 命名创建临时目录 {root}/.tmp/{uploadID}/
func (d *LocalDriver) InitPart(ctx context.Context, uploadID string, totalParts int) error {
	if !ValidStorageID(uploadID) {
		return ErrPathInvalid
	}
	tmpDir, err := SafeJoin(d.rootDir, ".tmp", uploadID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("创建分片临时目录失败: %w", err)
	}
	return nil
}

// UploadPart 上传单个分片，写入 {root}/.tmp/{upload_id}/part_{N}
func (d *LocalDriver) UploadPart(ctx context.Context, uploadID string, partNumber int, reader io.Reader, size int64) (string, error) {
	if partNumber < 1 {
		return "", errors.New("分片序号必须大于 0")
	}
	if !ValidStorageID(uploadID) {
		return "", ErrPathInvalid
	}
	tmpDir, err := SafeJoin(d.rootDir, ".tmp", uploadID)
	if err != nil {
		return "", err
	}
	partPath := filepath.Join(tmpDir, fmt.Sprintf("part_%d", partNumber))
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

// CompletePart 合并所有分片为目标文件，校验数量与逐片 MD5，成功后清理临时目录
func (d *LocalDriver) CompletePart(ctx context.Context, uploadID string, targetPath string, expectedParts int, expectedMD5 map[int]string) error {
	if !ValidStorageID(uploadID) {
		return ErrPathInvalid
	}
	tmpDir, err := SafeJoin(d.rootDir, ".tmp", uploadID)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("读取分片目录失败: %w", err)
	}

	// 收集磁盘上的分片并按序号排序
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
	sort.Slice(parts, func(i, j int) bool { return parts[i].number < parts[j].number })
	if len(parts) == 0 {
		return errors.New("未找到任何分片")
	}
	// 分片数量校验
	if expectedParts > 0 && len(parts) != expectedParts {
		return fmt.Errorf("分片数量不符：磁盘 %d / 期望 %d", len(parts), expectedParts)
	}
	// 逐片 MD5 校验（磁盘内容 vs 上传时记录）
	for _, p := range parts {
		want, ok := expectedMD5[p.number]
		if !ok {
			continue
		}
		got, err := md5File(filepath.Join(tmpDir, p.name))
		if err != nil {
			return fmt.Errorf("计算分片 %d MD5 失败: %w", p.number, err)
		}
		if got != want {
			return fmt.Errorf("分片 %d MD5 校验失败", p.number)
		}
	}

	// 创建目标文件
	fullTarget, err := d.absPath(targetPath)
	if err != nil {
		return err
	}
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
		_, err = io.Copy(target, partFile)
		partFile.Close()
		if err != nil {
			return fmt.Errorf("合并分片 %d 失败: %w", p.number, err)
		}
	}

	// 合并成功，清理临时目录（原实现永久残留 .tmp/{uploadID}）
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("清理分片临时目录失败: %w", err)
	}
	return nil
}

// md5File 计算文件 MD5
func md5File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// AbortPart 取消分片上传，清理临时目录
func (d *LocalDriver) AbortPart(ctx context.Context, uploadID string) error {
	if !ValidStorageID(uploadID) {
		return ErrPathInvalid
	}
	tmpDir, err := SafeJoin(d.rootDir, ".tmp", uploadID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("清理分片临时目录失败: %w", err)
	}
	return nil
}

// PresignURL 本地驱动不支持预签名
func (d *LocalDriver) PresignURL(ctx context.Context, path string, expire int64, method string) (string, error) {
	return "", ErrNotImplemented
}
