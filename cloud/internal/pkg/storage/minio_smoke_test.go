package storage

// MinIO 驱动运行验证冒烟测试（默认跳过，需真实 MinIO 服务）
//
// 运行方式（需可达的 MinIO 服务，本地未装时的安装步骤见 doc/04）：
//
//	启动服务：MINIO_ROOT_USER=minioadmin MINIO_ROOT_PASSWORD=minioadmin minio.exe server D:\data --address :9000
//	运行验证：MINIO_SMOKE=1 MINIO_SMOKE_ADDR=localhost:9000 go test ./internal/pkg/storage/ -run TestMinioSmoke -v
//
// 覆盖：Put/Get/Stat/Exists/Delete、分片全流程（数量+逐片 MD5 校验）、AbortPart、PresignURL（GET）。

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestMinioSmoke(t *testing.T) {
	if os.Getenv("MINIO_SMOKE") != "1" {
		t.Skip("需要真实 MinIO：设 MINIO_SMOKE=1 开启（见文件头注释）")
	}
	addr := os.Getenv("MINIO_SMOKE_ADDR")
	if addr == "" {
		addr = "localhost:9000"
	}
	cfg := MinioConfig{Endpoint: addr, AccessKey: "minioadmin", SecretKey: "minioadmin", Bucket: "onecloud-smoke"}
	d, err := NewMinioDriver(cfg)
	if err != nil {
		t.Fatalf("构造驱动: %v", err)
	}
	ctx := context.Background()

	// 确保桶存在（smoke 客户端自建，避免依赖 mc）
	if err := d.client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
		exists, e2 := d.client.BucketExists(ctx, cfg.Bucket)
		if e2 != nil || !exists {
			t.Fatalf("创建/确认桶失败: %v / %v", err, e2)
		}
	}

	// ===== 1. Put / Stat / Exists / Get / Delete =====
	content := bytes.Repeat([]byte("onecloud-smoke-"), 1000) // 15000 字节
	path := "smoke/p13/object-a.bin"
	if err := d.Put(ctx, path, bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := d.Stat(ctx, path)
	if err != nil || info.Size != int64(len(content)) {
		t.Fatalf("Stat: info=%v err=%v", info, err)
	}
	if ok, _ := d.Exists(ctx, path); !ok {
		t.Fatal("Exists: want true")
	}
	rc, err := d.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, content) {
		t.Fatal("Get: 内容不一致")
	}

	// ===== 2. 分片全流程：Init → 2 片上传（记录 ETag）→ Complete（数量+MD5 校验）=====
	uploadID := "900000000000000001"
	if err := d.InitPart(ctx, uploadID, 2); err != nil {
		t.Fatalf("InitPart: %v", err)
	}
	part1, part2 := content[:10000], content[10000:]
	md5Of := func(b []byte) string {
		h := md5.Sum(b)
		return hex.EncodeToString(h[:])
	}
	etag1, err := d.UploadPart(ctx, uploadID, 1, bytes.NewReader(part1), int64(len(part1)))
	if err != nil {
		t.Fatalf("UploadPart 1: %v", err)
	}
	etag2, err := d.UploadPart(ctx, uploadID, 2, bytes.NewReader(part2), int64(len(part2)))
	if err != nil {
		t.Fatalf("UploadPart 2: %v", err)
	}
	if etag1 != md5Of(part1) || etag2 != md5Of(part2) {
		t.Fatalf("分片 ETag 与本地 MD5 不一致: %s/%s", etag1, etag2)
	}
	target := "smoke/p13/multipart-done.bin"
	// 故意传入错误的 expectedMD5 应被拒绝
	if err := d.CompletePart(ctx, uploadID, target, 2, map[int]string{1: "deadbeef", 2: etag2}); err == nil {
		t.Fatal("CompletePart: 错误 MD5 未被拒绝")
	}
	// 重新走一遍（上一次失败后映射已被消费，需重新 init+上传）
	if err := d.InitPart(ctx, uploadID, 2); err != nil {
		t.Fatalf("InitPart retry: %v", err)
	}
	etag1, _ = d.UploadPart(ctx, uploadID, 1, bytes.NewReader(part1), int64(len(part1)))
	etag2, _ = d.UploadPart(ctx, uploadID, 2, bytes.NewReader(part2), int64(len(part2)))
	if err := d.CompletePart(ctx, uploadID, target, 2, map[int]string{1: etag1, 2: etag2}); err != nil {
		t.Fatalf("CompletePart: %v", err)
	}
	info2, err := d.Stat(ctx, target)
	if err != nil || info2.Size != int64(len(content)) {
		t.Fatalf("分片合并后 Stat: info=%v err=%v", info2, err)
	}

	// ===== 3. AbortPart =====
	abortID := "900000000000000002"
	if err := d.InitPart(ctx, abortID, 1); err != nil {
		t.Fatalf("InitPart(abort): %v", err)
	}
	if err := d.AbortPart(ctx, abortID); err != nil {
		t.Fatalf("AbortPart: %v", err)
	}

	// ===== 4. PresignURL（GET 预签名可匿名取回内容）=====
	u, err := d.PresignURL(ctx, path, 300, "download")
	if err != nil {
		t.Fatalf("PresignURL: %v", err)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(u)
	if err != nil {
		t.Fatalf("预签名下载请求: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !bytes.Equal(body, content) {
		t.Fatalf("预签名下载: code=%d len=%d", resp.StatusCode, len(body))
	}

	// ===== 5. 清理 =====
	if err := d.Delete(ctx, path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := d.Delete(ctx, target); err != nil {
		t.Fatalf("Delete(target): %v", err)
	}
	if ok, _ := d.Exists(ctx, path); ok {
		t.Fatal("Delete 后 Exists 仍为 true")
	}
	t.Log("MinIO 驱动运行验证全部通过")
}
