package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本地驱动纯文件系统测试（无网络依赖）

func newTestLocalDriver(t *testing.T) (*LocalDriver, string) {
	t.Helper()
	root := t.TempDir()
	return NewLocalDriver(root), root
}

func TestPutSizeMismatchRejected(t *testing.T) {
	d, root := newTestLocalDriver(t)
	ctx := context.Background()

	// 实际 5 字节 vs 声明 10 字节：必须报错且不留半成品文件
	err := d.Put(ctx, "u1/f.bin", bytes.NewReader([]byte("hello")), 10)
	if err == nil {
		t.Fatal("写入字节数与声明不符应报错")
	}
	if _, statErr := os.Stat(filepath.Join(root, "u1", "f.bin")); !os.IsNotExist(statErr) {
		t.Fatal("失败的 Put 不应留下半成品文件")
	}

	// 声明与实际一致：成功
	if err := d.Put(ctx, "u1/f.bin", bytes.NewReader([]byte("hello")), 5); err != nil {
		t.Fatalf("正常 Put: %v", err)
	}
	// size<=0 表示不校验：透传写入
	if err := d.Put(ctx, "u1/g.bin", bytes.NewReader([]byte("hello")), 0); err != nil {
		t.Fatalf("size=0 Put: %v", err)
	}
}

func TestCompletePartMergesInOrder(t *testing.T) {
	d, root := newTestLocalDriver(t)
	ctx := context.Background()

	if err := d.InitPart(ctx, "111", 2); err != nil {
		t.Fatalf("InitPart: %v", err)
	}
	md5Of := func(b byte) string {
		h := md5.Sum([]byte{b})
		return hex.EncodeToString(h[:])
	}
	if _, err := d.UploadPart(ctx, "111", 1, bytes.NewReader([]byte{1}), 1); err != nil {
		t.Fatalf("UploadPart 1: %v", err)
	}
	if _, err := d.UploadPart(ctx, "111", 2, bytes.NewReader([]byte{2}), 1); err != nil {
		t.Fatalf("UploadPart 2: %v", err)
	}

	expected := map[int]string{1: md5Of(1), 2: md5Of(2)}
	if err := d.CompletePart(ctx, "111", "u1/merged.bin", 2, expected); err != nil {
		t.Fatalf("CompletePart: %v", err)
	}

	// 合并顺序与内容校验
	f, err := os.Open(filepath.Join(root, "u1", "merged.bin"))
	if err != nil {
		t.Fatalf("打开合并产物: %v", err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if !bytes.Equal(got, []byte{1, 2}) {
		t.Fatalf("合并内容应为 [1 2]，实际 %v", got)
	}
	// 临时目录已清理
	if _, err := os.Stat(filepath.Join(root, ".tmp", "111")); !os.IsNotExist(err) {
		t.Fatal("合并成功后临时目录应被清理")
	}
	// 无 .merging 残留
	entries, _ := os.ReadDir(filepath.Join(root, "u1"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".merging") {
			t.Fatalf("存在 .merging 残留: %s", e.Name())
		}
	}
}

func TestCompletePartRejectsMD5Mismatch(t *testing.T) {
	d, root := newTestLocalDriver(t)
	ctx := context.Background()

	_ = d.InitPart(ctx, "222", 1)
	if _, err := d.UploadPart(ctx, "222", 1, bytes.NewReader([]byte{9}), 1); err != nil {
		t.Fatalf("UploadPart: %v", err)
	}
	// 期望 MD5 与实际不符：拒绝且不留目标文件
	bad := map[int]string{1: strings.Repeat("0", 32)}
	if err := d.CompletePart(ctx, "222", "u2/never.bin", 1, bad); err == nil {
		t.Fatal("MD5 不符应报错")
	}
	if _, err := os.Stat(filepath.Join(root, "u2", "never.bin")); !os.IsNotExist(err) {
		t.Fatal("失败合并不应产生目标文件")
	}
	if _, err := os.Stat(filepath.Join(root, "u2", "never.bin.merging")); !os.IsNotExist(err) {
		t.Fatal("失败合并不应留下 .merging 临时文件")
	}
}

func TestContentDisposition(t *testing.T) {
	cases := []struct {
		in      string
		wantSub []string
	}{
		{"report.pdf", []string{"attachment", `filename="report.pdf"`}},
		{"报告.pdf", []string{"attachment", `filename*=UTF-8''%E6%8A%A5%E5%91%8A.pdf`}}, // 非 ASCII 走 RFC 5987
		{`evil"name`, []string{"attachment", `filename="evilname"`}},                  // 引号剥离
		{"", []string{"attachment"}},                                                  // 空文件名仅 attachment
	}
	for _, c := range cases {
		got := contentDisposition(c.in)
		for _, sub := range c.wantSub {
			if !strings.Contains(got, sub) {
				t.Fatalf("contentDisposition(%q) = %q，缺少 %q", c.in, got, sub)
			}
		}
	}
	if got := contentDisposition(""); got != "attachment" {
		t.Fatalf("空文件名应恰好为 attachment，实际 %q", got)
	}
}

func TestIsMD5Like(t *testing.T) {
	yes := []string{"d41d8cd98f00b204e9800998ecf8427e", "D41D8CD98F00B204E9800998ECF8427E"}
	no := []string{"", "short", strings.Repeat("g", 32), strings.Repeat("a", 33), "\"d41d8cd98f00b204e9800998ecf8427e\""}
	for _, s := range yes {
		if !isMD5Like(s) {
			t.Fatalf("%q 应判定为 MD5 形态", s)
		}
	}
	for _, s := range no {
		if isMD5Like(s) {
			t.Fatalf("%q 不应判定为 MD5 形态", s)
		}
	}
}

func TestUploadIDFromTmpKey(t *testing.T) {
	if got := uploadIDFromTmpKey(".tmp/123456/blob"); got != "123456" {
		t.Fatalf("正常键反解失败: %q", got)
	}
	for _, bad := range []string{"user/x/y.bin", ".tmp/123/other", ".tmp//blob", "tmp/123/blob"} {
		if got := uploadIDFromTmpKey(bad); got != "" {
			t.Fatalf("非法键 %q 应返回空，实际 %q", bad, got)
		}
	}
}
