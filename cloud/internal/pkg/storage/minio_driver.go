package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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
	// mu 保护 in flight 分片映射：业务 upload_id → 服务端 upload_id。
	// S3 multipart 的 upload_id 由服务端生成，而本驱动接口传入业务生成的 upload_id，
	// 需要映射；进程重启后未完成的映射丢失——由 reconcileOrphans 周期对账
	// （中止服务端超期任务），不依赖本进程记忆
	mu      sync.Mutex
	uploads map[string]*uploadEntry
}

// uploadEntry 服务端分片任务映射与其创建时间（对账与惰性过期用）
type uploadEntry struct {
	serverID string
	created  time.Time
}

// 编译期接口断言：签名漂移即编译失败
var _ Driver = (*MinioDriver)(nil)

const (
	// orphanReconcileInterval 服务端孤儿分片任务对账周期
	orphanReconcileInterval = 12 * time.Hour
	// orphanMaxAge 服务端 multipart 任务超期阈值（与 .tmp 物理清理对齐，2× Redis TTL）：
	// 超过该时长仍未完成的任务视为被放弃，对账时中止并清理
	orphanMaxAge = 48 * time.Hour
)

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
	d := &MinioDriver{
		client:  client,
		bucket:  cfg.Bucket,
		uploads: map[string]*uploadEntry{},
	}
	// 服务端孤儿 multipart 对账：客户端放弃且未调 Abort、或进程重启丢映射时，
	// 服务端任务会永久残留占存储；周期对账中止超期任务（进程生命周期内存活）
	go d.reconcileLoop()
	return d, nil
}

// reconcileLoop 周期对账服务端 multipart 任务
func (d *MinioDriver) reconcileLoop() {
	// 首扫延迟 30 分钟，避开启动高峰
	<-time.After(30 * time.Minute)
	_ = d.reconcileOrphans(time.Now())
	ticker := time.NewTicker(orphanReconcileInterval)
	defer ticker.Stop()
	for now := range ticker.C {
		_ = d.reconcileOrphans(now)
	}
}

// reconcileOrphans 中止服务端超期 multipart 任务并同步清理本地映射。
// 失败仅记日志（下轮重试）；不区分任务是否本进程发起——超期即视为放弃
func (d *MinioDriver) reconcileOrphans(now time.Time) error {
	deadline := now.Add(-orphanMaxAge)
	var aborted []string
	prefix := ".tmp/"
	keyMarker, idMarker := "", ""
	for {
		res, err := d.client.ListMultipartUploads(context.Background(), d.bucket,
			prefix, keyMarker, idMarker, "", 1000)
		if err != nil {
			return fmt.Errorf("列举服务端分片任务失败: %w", err)
		}
		for _, u := range res.Uploads {
			if u.Initiated.After(deadline) {
				continue
			}
			if err := d.client.AbortMultipartUpload(context.Background(), d.bucket, u.Key, u.UploadID); err != nil {
				if !isNotFound(err) {
					continue // 单个失败不阻塞其余
				}
			}
			aborted = append(aborted, uploadIDFromTmpKey(u.Key))
		}
		if !res.IsTruncated {
			break
		}
		keyMarker, idMarker = res.NextKeyMarker, res.NextUploadIDMarker
	}
	// 同步清理本地映射中被中止的任务
	if len(aborted) > 0 {
		d.mu.Lock()
		for _, id := range aborted {
			delete(d.uploads, id)
		}
		d.mu.Unlock()
	}
	return nil
}

// uploadIDFromTmpKey 从临时对象键 .tmp/{uploadID}/blob 反解业务 upload_id（解析失败返回空）
func uploadIDFromTmpKey(key string) string {
	segs := strings.Split(key, "/")
	if len(segs) != 3 || segs[0] != ".tmp" || segs[2] != "blob" || segs[1] == "" {
		return ""
	}
	return segs[1]
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
	d.uploads[uploadID] = &uploadEntry{serverID: serverID, created: time.Now()}
	d.mu.Unlock()
	return nil
}

// UploadPart 上传单个分片，返回分片内容 MD5
// 双轨校验：驱动内 TeeReader 自算 MD5（与本地驱动语义一致，SSE-KMS 等
// ETag≠MD5 的场景也返回真实内容摘要），CompletePart 侧仅对「形似 MD5」的
// 服务端 ETag 做逐片比对（见 isMD5Like）
func (d *MinioDriver) UploadPart(ctx context.Context, uploadID string, partNumber int, reader io.Reader, size int64) (string, error) {
	tmpKey, err := tmpObject(uploadID)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	entry, ok := d.uploads[uploadID]
	d.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("分片上传任务不存在或已失效: %s", uploadID)
	}
	hash := md5.New()
	part, err := d.client.PutObjectPart(ctx, d.bucket, tmpKey, entry.serverID, partNumber,
		io.TeeReader(reader, hash), size, minio.PutObjectPartOptions{})
	if err != nil {
		return "", fmt.Errorf("分片写入失败: %w", err)
	}
	_ = part.ETag
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// isMD5Like 判断 ETag 是否形似 MD5（32 位十六进制）：SSE-KMS 等场景 ETag 不是
// 内容 MD5，此时跳过逐片比对而非误报校验失败
func isMD5Like(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
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
// 失败时保留业务 upload_id → 服务端 upload_id 映射（与本地驱动语义一致），客户端可重试；
// 全部步骤成功后才消费映射
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
	entry, ok := d.uploads[uploadID]
	d.mu.Unlock()
	if !ok {
		return fmt.Errorf("分片上传任务不存在或已失效: %s", uploadID)
	}

	etags, err := d.listPartETags(ctx, tmpKey, entry.serverID)
	if err != nil {
		return err
	}
	if expectedParts > 0 && len(etags) != expectedParts {
		return fmt.Errorf("分片数量不符：磁盘 %d / 期望 %d", len(etags), expectedParts)
	}
	for num, want := range expectedMD5 {
		got, ok := etags[num]
		if !ok || !isMD5Like(got) {
			// 分片缺失或 ETag 非 MD5 形态（SSE）：跳过逐片比对，完整性由数量校验
			// 与业务层大小对账兜底
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
	if _, err := d.client.CompleteMultipartUpload(ctx, d.bucket, tmpKey, entry.serverID, parts, minio.PutObjectOptions{}); err != nil {
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
	// 全部成功，消费映射
	d.mu.Lock()
	delete(d.uploads, uploadID)
	d.mu.Unlock()
	return nil
}

// AbortPart 取消分片上传并中止服务端任务
// 服务端任务不存在（已被清理/中止过）视为成功并清理映射；
// 其他失败保留映射供客户端重试中止
func (d *MinioDriver) AbortPart(ctx context.Context, uploadID string) error {
	tmpKey, err := tmpObject(uploadID)
	if err != nil {
		return err
	}
	d.mu.Lock()
	entry, ok := d.uploads[uploadID]
	d.mu.Unlock()
	if !ok {
		return ErrFileNotFound
	}
	consume := func() {
		d.mu.Lock()
		delete(d.uploads, uploadID)
		d.mu.Unlock()
	}
	if err := d.client.AbortMultipartUpload(ctx, d.bucket, tmpKey, entry.serverID); err != nil {
		if isNotFound(err) {
			consume()
			return ErrFileNotFound
		}
		return fmt.Errorf("中止分片上传失败: %w", err)
	}
	consume()
	return nil
}

// PresignURL 生成预签名 URL（download→GET，upload→PUT；expire 秒）
// 下载预签名强制 response-content-disposition=attachment：html/svg 等可内联类型
// 若允许浏览器直接渲染，预签名 URL 会成为存储型 XSS 载体
func (d *MinioDriver) PresignURL(ctx context.Context, path string, expire int64, method string, filename string) (string, error) {
	key, err := objectKey(path)
	if err != nil {
		return "", err
	}
	if expire <= 0 {
		expire = 3600
	}
	switch strings.ToLower(method) {
	case "upload", "put":
		u, err := d.client.PresignHeader(ctx, "PUT", d.bucket, key, time.Duration(expire)*time.Second, nil, nil)
		if err != nil {
			return "", fmt.Errorf("生成预签名上传 URL 失败: %w", err)
		}
		return u.String(), nil
	default:
		reqParams := make(url.Values)
		reqParams.Set("response-content-disposition", contentDisposition(filename))
		u, err := d.client.PresignHeader(ctx, "GET", d.bucket, key, time.Duration(expire)*time.Second, reqParams, nil)
		if err != nil {
			return "", fmt.Errorf("生成预签名下载 URL 失败: %w", err)
		}
		return u.String(), nil
	}
}

// contentDisposition 构造 attachment 头：filename 走 RFC 5987（UTF-8 百分号编码），
// 同时提供 ASCII 回退（不识别 filename* 的旧客户端）；控制字符/引号剥离
func contentDisposition(filename string) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, filename)
	if name == "" {
		return "attachment"
	}
	fallback := name
	for i := len(fallback) - 1; i >= 0; i-- {
		if fallback[i] > 0x7f { // 截断首个非 ASCII（粗略回退，仅保扩展名可读）
			fallback = fallback[:i]
		} else {
			break
		}
	}
	if fallback == "" {
		fallback = "download"
	}
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + url.PathEscape(name)
}
