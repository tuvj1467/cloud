package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioDriver MinIO / S3 兼容对象存储驱动
// 通过 minio-go 访问 MinIO 或任何 S3 兼容端点
type MinioDriver struct {
	client *minio.Core
	bucket string
	// mu 保护 in flight 分片映射：业务 upload_id → S3 服务端 upload_id
	// S3 multipart 的 upload_id 由服务端生成，而本驱动接口传入业务生成的 upload_id，
	// 需要映射；进程重启后未完成的映射丢失（未完成分片由 MinIO 生命周期策略或手工清理）
	mu      sync.Mutex
	uploads map[string]string
}

// 编译期接口断言：签名漂移即编译失败
var _ Driver = (*MinioDriver)(nil)

// NewMinioDriver 创建 MinIO / S3 兼容驱动实例（仅本地校验配置，不做网络请求）
func NewMinioDriver(cfg MinioConfig) (*MinioDriver, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("minio 配置不完整：endpoint 必填")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("minio 配置不完整：accessKey / secretKey 必填")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("minio 配置不完整：bucket 必填")
	}
	client, err := minio.NewCore(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 minio 客户端失败: %w", err)
	}
	return &MinioDriver{
		client:  client,
		bucket:  cfg.Bucket,
		uploads: map[string]string{},
	}, nil
}

// objectKey 校验并规范化对象键：拒绝 .. 段（与本地驱动的路径安全姿态一致）
func objectKey(path string) (string, error) {
	key := strings.TrimPrefix(filepath2Unix(path), "/")
	if key == "" {
		return "", ErrPathInvalid
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrPathInvalid
		}
	}
	return key, nil
}

// filepath2Unix 统一 Windows 反斜杠为 S3 对象键分隔符
func filepath2Unix(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

// isNotFound 判断 S3 错误是否为对象/上传不存在
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.Code == "NoSuchUpload" || resp.Code == "NotFound" || resp.StatusCode == 404
}

// Put 上传完整对象
func (d *MinioDriver) Put(ctx context.Context, path string, reader io.Reader, size int64) error {
	key, err := objectKey(path)
	if err != nil {
		return err
	}
	// 空串表示不预计算 md5/sha256（由 SDK 分片计算）
	_, err = d.client.PutObject(ctx, d.bucket, key, reader, size, "", "", minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("对象写入失败: %w", err)
	}
	return nil
}

// Get 读取对象，返回可读流（调用方负责 Close）
func (d *MinioDriver) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	key, err := objectKey(path)
	if err != nil {
		return nil, err
	}
	obj, _, _, err := d.client.GetObject(ctx, d.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("对象读取失败: %w", err)
	}
	// Core.GetObject 内部已先 StatObject，404 在此已转换为错误返回
	return obj, nil
}

// Delete 删除对象（不存在返回 ErrFileNotFound，与本地驱动语义一致）
func (d *MinioDriver) Delete(ctx context.Context, path string) error {
	key, err := objectKey(path)
	if err != nil {
		return err
	}
	if _, err := d.client.StatObject(ctx, d.bucket, key, minio.StatObjectOptions{}); err != nil {
		if isNotFound(err) {
			return ErrFileNotFound
		}
		return fmt.Errorf("对象状态查询失败: %w", err)
	}
	return d.client.RemoveObject(ctx, d.bucket, key, minio.RemoveObjectOptions{})
}

// Stat 获取对象信息
func (d *MinioDriver) Stat(ctx context.Context, path string) (*FileInfo, error) {
	key, err := objectKey(path)
	if err != nil {
		return nil, err
	}
	info, err := d.client.StatObject(ctx, d.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("对象状态查询失败: %w", err)
	}
	return &FileInfo{
		Size:         info.Size,
		LastModified: info.LastModified.Unix(),
		ContentType:  info.ContentType,
	}, nil
}

// Exists 判断对象是否存在
func (d *MinioDriver) Exists(ctx context.Context, path string) (bool, error) {
	_, err := d.Stat(ctx, path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrFileNotFound) {
		return false, nil
	}
	return false, err
}

// tmpObject 分片临时对象键：{root}/.tmp/{uploadID}/blob 形态
func tmpObject(uploadID string) (string, error) {
	if !ValidStorageID(uploadID) {
		return "", ErrPathInvalid
	}
	return ".tmp/" + uploadID + "/blob", nil
}

// InitPart 初始化 S3 multipart 上传
// 业务 uploadID 映射到服务端 upload_id（内存映射，进程重启丢失未完成映射）
func (d *MinioDriver) InitPart(ctx context.Context, uploadID string, totalParts int) error {
	tmpKey, err := tmpObject(uploadID)
	if err != nil {
		return err
	}
	serverID, err := d.client.NewMultipartUpload(ctx, d.bucket, tmpKey, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("初始化分片上传失败: %w", err)
	}
	d.mu.Lock()
	d.uploads[uploadID] = serverID
	d.mu.Unlock()
	return nil
}

// UploadPart 上传单个分片，返回分片 MD5（S3 对非 multipart 分片的 ETag 即内容 MD5）
func (d *MinioDriver) UploadPart(ctx context.Context, uploadID string, partNumber int, reader io.Reader, size int64) (string, error) {
	tmpKey, err := tmpObject(uploadID)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	serverID, ok := d.uploads[uploadID]
	d.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("分片上传任务不存在或已失效: %s", uploadID)
	}
	part, err := d.client.PutObjectPart(ctx, d.bucket, tmpKey, serverID, partNumber, reader, size, minio.PutObjectPartOptions{})
	if err != nil {
		return "", fmt.Errorf("分片写入失败: %w", err)
	}
	return strings.Trim(part.ETag, "\""), nil
}

// listPartETags 拉取已上传分片的 序号→MD5（ETag 去引号）
func (d *MinioDriver) listPartETags(ctx context.Context, tmpKey, serverID string) (map[int]string, error) {
	res := map[int]string{}
	marker := 0
	for {
		p, err := d.client.ListObjectParts(ctx, d.bucket, tmpKey, serverID, marker, 1000)
		if err != nil {
			if isNotFound(err) {
				return res, nil
			}
			return nil, fmt.Errorf("查询分片列表失败: %w", err)
		}
		for _, pp := range p.ObjectParts {
			res[pp.PartNumber] = strings.Trim(pp.ETag, "\"")
		}
		if !p.IsTruncated {
			break
		}
		marker = p.NextPartNumberMarker
	}
	return res, nil
}

// CompletePart 校验分片数量与逐片 MD5 后合并为目标对象
// S3 语义：Complete 作用于 init 时的临时对象名，完成后服务端拷贝到目标键并清理临时对象
// ⚠ S3 强制约束：除最后一片外每片必须 ≥5MiB——调用方（业务层/前端）需保证切片大小合规
func (d *MinioDriver) CompletePart(ctx context.Context, uploadID string, targetPath string, expectedParts int, expectedMD5 map[int]string) error {
	tmpKey, err := tmpObject(uploadID)
	if err != nil {
		return err
	}
	targetKey, err := objectKey(targetPath)
	if err != nil {
		return err
	}
	d.mu.Lock()
	serverID, ok := d.uploads[uploadID]
	delete(d.uploads, uploadID)
	d.mu.Unlock()
	if !ok {
		return fmt.Errorf("分片上传任务不存在或已失效: %s", uploadID)
	}

	etags, err := d.listPartETags(ctx, tmpKey, serverID)
	if err != nil {
		return err
	}
	if expectedParts > 0 && len(etags) != expectedParts {
		return fmt.Errorf("分片数量不符：磁盘 %d / 期望 %d", len(etags), expectedParts)
	}
	for num, want := range expectedMD5 {
		got, ok := etags[num]
		if !ok {
			continue
		}
		if !strings.EqualFold(got, want) {
			return fmt.Errorf("分片 %d MD5 校验失败", num)
		}
	}

	nums := make([]int, 0, len(etags))
	for num := range etags {
		nums = append(nums, num)
	}
	sort.Ints(nums)
	if len(nums) == 0 {
		return errors.New("未找到任何分片")
	}
	parts := make([]minio.CompletePart, 0, len(nums))
	for _, num := range nums {
		parts = append(parts, minio.CompletePart{PartNumber: num, ETag: etags[num]})
	}
	if _, err := d.client.CompleteMultipartUpload(ctx, d.bucket, tmpKey, serverID, parts, minio.PutObjectOptions{}); err != nil {
		return fmt.Errorf("合并分片失败: %w", err)
	}
	// 合并产物落在临时键上，服务端拷贝到目标键后删除临时对象
	if _, err := d.client.CopyObject(ctx,
		d.bucket, tmpKey, d.bucket, targetKey,
		nil, minio.CopySrcOptions{}, minio.PutObjectOptions{}); err != nil {
		return fmt.Errorf("合并对象就位失败: %w", err)
	}
	if err := d.client.RemoveObject(ctx, d.bucket, tmpKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("清理分片临时对象失败: %w", err)
	}
	return nil
}

// AbortPart 取消分片上传并中止服务端任务
func (d *MinioDriver) AbortPart(ctx context.Context, uploadID string) error {
	tmpKey, err := tmpObject(uploadID)
	if err != nil {
		return err
	}
	d.mu.Lock()
	serverID, ok := d.uploads[uploadID]
	delete(d.uploads, uploadID)
	d.mu.Unlock()
	if !ok {
		return ErrFileNotFound
	}
	if err := d.client.AbortMultipartUpload(ctx, d.bucket, tmpKey, serverID); err != nil {
		if isNotFound(err) {
			return ErrFileNotFound
		}
		return fmt.Errorf("中止分片上传失败: %w", err)
	}
	return nil
}

// PresignURL 生成预签名 URL（download→GET，upload→PUT；expire 秒）
func (d *MinioDriver) PresignURL(ctx context.Context, path string, expire int64, method string) (string, error) {
	key, err := objectKey(path)
	if err != nil {
		return "", err
	}
	if expire <= 0 {
		expire = 3600
	}
	reqParams := make(url.Values)
	switch strings.ToLower(method) {
	case "upload", "put":
		u, err := d.client.PresignHeader(ctx, "PUT", d.bucket, key, time.Duration(expire)*time.Second, nil, nil)
		if err != nil {
			return "", fmt.Errorf("生成预签名上传 URL 失败: %w", err)
		}
		return u.String(), nil
	default:
		u, err := d.client.PresignHeader(ctx, "GET", d.bucket, key, time.Duration(expire)*time.Second, reqParams, nil)
		if err != nil {
			return "", fmt.Errorf("生成预签名下载 URL 失败: %w", err)
		}
		return u.String(), nil
	}
}
