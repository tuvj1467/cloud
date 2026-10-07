package util

import (
	"strings"
	"testing"
)

func TestCalculateFileMD5KnownVector(t *testing.T) {
	// RFC 1321 标准向量："abc" → 900150983cd24fb0d6963f7d28e17f72
	if got := CalculateFileMD5([]byte("abc")); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("MD5 标准向量不符: %q", got)
	}
	if got := CalculateFileMD5(nil); got != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("空输入 MD5 不符: %q", got)
	}
}

func TestGenerateShareCodeCharsetAndLength(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := GenerateShareCode(8)
		if err != nil {
			t.Fatalf("GenerateShareCode: %v", err)
		}
		if len(code) != 8 {
			t.Fatalf("长度应为 8: %q", code)
		}
		for _, c := range code {
			if !strings.ContainsRune(shareAlphabet, c) {
				t.Fatalf("字符集外字符 %q in %q", c, code)
			}
		}
		// 去除易混淆字符 0/O/I/1/l
		if strings.ContainsAny(code, "0Oo1lI") {
			t.Fatalf("含易混淆字符: %q", code)
		}
		seen[code] = true
	}
	// 200 次生成应有极低重复率（54^8 空间，重复概率可忽略）
	if len(seen) < 195 {
		t.Fatalf("随机性异常：200 次仅 %d 个唯一值", len(seen))
	}
}

func TestGenerateShareCodeDefaultLength(t *testing.T) {
	code, err := GenerateShareCode(0)
	if err != nil || len(code) != 8 {
		t.Fatalf("非正长度应默认 8 位: %q %v", code, err)
	}
}
