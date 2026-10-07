package filelogic

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"onecloud/ent"
	"onecloud/proto/.proto/service"
)

// logic 层纯函数测试（无 DB/Redis 依赖）

func TestToFileMetaFieldMapping(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 30, 5, 0, time.Local)
	f := &ent.File{
		ID:          "f-1",
		Name:        "report.pdf",
		Path:        "u1/20261007/f-1.pdf",
		Size:        12345,
		Ext:         "pdf",
		UserID:      "u-1",
		BucketID:    "b-1",
		Md5:         "d41d8cd98f00b204e9800998ecf8427e",
		ContentType: "application/pdf",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m := toFileMeta(f)
	if m.FileId != f.ID || m.Name != f.Name || m.Path != f.Path || m.Size != f.Size {
		t.Fatalf("基础字段映射不符: %+v", m)
	}
	if m.Suffix != "pdf" || m.UserId != "u-1" || m.BucketId != "b-1" || m.Md5 != f.Md5 {
		t.Fatalf("扩展字段映射不符: %+v", m)
	}
	if m.ContentType != "application/pdf" {
		t.Fatalf("content_type 映射不符: %q", m.ContentType)
	}
	if m.CreatedAt != "2026-10-07 12:30:05" {
		t.Fatalf("时间格式不符: %q", m.CreatedAt)
	}
}

func TestAnonymizeFileMetaStripsSensitiveFields(t *testing.T) {
	m := &service.FileMeta{
		FileId:   "f-1",
		Name:     "a.txt",
		Path:     "u1/20261007/f-1.txt",
		UserId:   "u-1",
		BucketId: "b-1",
		Md5:      "abc",
	}
	out := anonymizeFileMeta(m)
	// 原地脱敏：路径与归属信息不下发给匿名端点
	if out.Path != "" || out.UserId != "" || out.BucketId != "" || out.Md5 != "" {
		t.Fatalf("脱敏不完整: %+v", out)
	}
	if out.FileId != "f-1" || out.Name != "a.txt" {
		t.Fatalf("非敏感字段不应被清除: %+v", out)
	}
}

func TestClientInfoPrefersForwardedHeaders(t *testing.T) {
	md := metadata.Pairs(
		"gateway-x-forwarded-for", "203.0.113.9, 10.0.0.1",
		"gateway-user-agent", "unit-agent/1.0",
	)
	ctx := peer.NewContext(metadata.NewIncomingContext(context.Background(), md), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("10.1.1.1"), Port: 7000}})
	ip, ua := clientInfo(ctx)
	if ip != "203.0.113.9, 10.0.0.1" {
		t.Fatalf("XFF 应整体透传（首跳解析由消费方负责），实际 %q", ip)
	}
	if ua != "unit-agent/1.0" {
		t.Fatalf("UA 应取网关转发值，实际 %q", ua)
	}
}

func TestClientInfoFallsBackToPeer(t *testing.T) {
	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("192.168.0.9"), Port: 8123}})
	ip, ua := clientInfo(ctx)
	if ip != "192.168.0.9:8123" {
		t.Fatalf("无 XFF 时应回退对端地址，实际 %q", ip)
	}
	if ua != "" {
		t.Fatalf("无 UA 时应为空，实际 %q", ua)
	}
}

func TestFreshPathOf(t *testing.T) {
	if got := freshPathOf(nil, "u1/x.bin"); got != "u1/x.bin" {
		t.Fatalf("完整上传的物理文件应可回滚删除，实际 %q", got)
	}
	if got := freshPathOf(&ent.File{Path: "u1/shared.bin"}, "u1/new.bin"); got != "" {
		t.Fatalf("秒传共享的既有物理文件不可删除，实际 %q", got)
	}
}
