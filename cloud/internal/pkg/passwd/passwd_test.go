package passwd

import "testing"

func TestHashCompareRoundTrip(t *testing.T) {
	h, err := Hash("s3cret-Pass!")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !IsHashed(h) {
		t.Fatalf("哈希应被识别为已加密: %q", h)
	}
	if err := Compare(h, "s3cret-Pass!"); err != nil {
		t.Fatalf("正确密码校验应通过: %v", err)
	}
	if err := Compare(h, "wrong"); err == nil {
		t.Fatal("错误密码校验应失败")
	}
	// bcrypt 盐值随机：同一明文两次哈希不同
	h2, _ := Hash("s3cret-Pass!")
	if h == h2 {
		t.Fatal("bcrypt 应每次产生不同盐值/哈希")
	}
}

func TestIsHashedLegacyPlaintext(t *testing.T) {
	// 存量明文密码（渐进式迁移场景）不得被误判为哈希
	for _, s := range []string{"", "123456", "$2a$short", "$2b$" + string(make([]byte, 55)), "$2c$" + string(make([]byte, 56)), "$2a$" + string(make([]byte, 59))} {
		if IsHashed(s) {
			t.Fatalf("明文/非法格式不应判为哈希: %q", s)
		}
	}
}
