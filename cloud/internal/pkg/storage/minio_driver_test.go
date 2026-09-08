package storage

import (
	"errors"
	"testing"
)

// 测试均不依赖真实 MinIO 服务：只覆盖构造校验、工厂分发与对象键安全校验。
// 需要真实服务的读写/分片/预签名运行验证另行执行（见 doc/04 状态注记）。

func TestNewMinioDriverValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     MinioConfig
		wantErr bool
	}{
		{"缺少endpoint", MinioConfig{AccessKey: "ak", SecretKey: "sk", Bucket: "bkt"}, true},
		{"缺少accessKey", MinioConfig{Endpoint: "localhost:9000", SecretKey: "sk", Bucket: "bkt"}, true},
		{"缺少secretKey", MinioConfig{Endpoint: "localhost:9000", AccessKey: "ak", Bucket: "bkt"}, true},
		{"缺少bucket", MinioConfig{Endpoint: "localhost:9000", AccessKey: "ak", SecretKey: "sk"}, true},
		{"合法配置", MinioConfig{Endpoint: "localhost:9000", AccessKey: "ak", SecretKey: "sk", Bucket: "bkt"}, false},
	}
	for _, c := range cases {
		d, err := NewMinioDriver(c.cfg)
		if c.wantErr && err == nil {
			t.Errorf("%s: want error, got nil", c.name)
		}
		if !c.wantErr {
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
			}
			if d.bucket != "bkt" {
				t.Errorf("%s: bucket not wired", c.name)
			}
		}
	}
}

func TestFactoryMinio(t *testing.T) {
	d, err := NewDriver(Config{
		Driver: "minio",
		Minio:  MinioConfig{Endpoint: "localhost:9000", AccessKey: "ak", SecretKey: "sk", Bucket: "bkt"},
	})
	if err != nil {
		t.Fatalf("factory minio: %v", err)
	}
	if _, ok := d.(*MinioDriver); !ok {
		t.Fatalf("factory returned %T, want *MinioDriver", d)
	}
	if _, err := NewDriver(Config{Driver: "unknown-x"}); err == nil {
		t.Fatal("unknown driver should fail")
	}
}

func TestMinioObjectKeyValidation(t *testing.T) {
	for _, p := range []string{"", "../escape", "a/../b", "u1//f", ".", ".."} {
		if _, err := objectKey(p); !errors.Is(err, ErrPathInvalid) {
			t.Errorf("objectKey(%q) want ErrPathInvalid, got %v", p, err)
		}
	}
	for _, p := range []string{"u1/20260909/f1.jpg", "u1\\20260909\\f1.jpg"} {
		if _, err := objectKey(p); err != nil {
			t.Errorf("objectKey(%q) unexpected error %v", p, err)
		}
	}
}

func TestMinioTmpObjectValidation(t *testing.T) {
	if _, err := tmpObject("../evil"); !errors.Is(err, ErrPathInvalid) {
		t.Fatalf("tmpObject traversal: want ErrPathInvalid, got %v", err)
	}
	if k, err := tmpObject("123456"); err != nil || k != ".tmp/123456/blob" {
		t.Fatalf("tmpObject ok case: key=%s err=%v", k, err)
	}
}
