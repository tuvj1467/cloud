package jwt

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const testSecret = "unit-test-secret-0123456789abcdef-XYZ"

func init() {
	// 测试统一初始化（空 Secret 会 panic——那本身就是被测行为之一）
	Init(testSecret, 3600, "unit-test")
}

func TestGenerateAndParseRoundTrip(t *testing.T) {
	token, err := GenerateToken("user-123", 7)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	parsed, claims, err := ParseToken(token)
	if err != nil || !parsed.Valid {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.UserId != "user-123" || claims.Ver != 7 {
		t.Fatalf("claims 不匹配: id=%s ver=%d", claims.UserId, claims.Ver)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Before(time.Now()) {
		t.Fatalf("过期时间缺失或已过期")
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	// 用另一密钥签发的 token 必须被拒
	other := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{UserId: "evil"})
	signed, err := other.SignedString([]byte("attacker-secret-0123456789abcdef"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, _, err := ParseToken(signed); err == nil {
		t.Fatal("错误密钥签发的 token 应解析失败")
	}
}

func TestParseRejectsNoneAlgorithm(t *testing.T) {
	// alg=none（未签名）token 必须被 keyfunc 的 HMAC 白名单拒绝——算法混淆攻击防护
	token := jwt.NewWithClaims(jwt.SigningMethodNone, &Claims{UserId: "evil"})
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if _, _, err := ParseToken(signed); err == nil {
		t.Fatal("alg=none token 应被拒绝")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "not-a-token", "a.b.c", strings.Repeat("x", 4096)} {
		if _, _, err := ParseToken(s); err == nil {
			t.Fatalf("垃圾输入 %q 应解析失败", s)
		}
	}
}

func TestInitEmptySecretPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("空 Secret 的 Init 应 panic（拒绝静默回退默认密钥）")
		}
		// 恢复测试密钥，避免污染其他用例
		Init(testSecret, 3600, "unit-test")
	}()
	Init("", 0, "")
}

func TestGenerateWithoutInitFails(t *testing.T) {
	// 模拟未初始化：清空 secret
	cfgMu.Lock()
	saved := cfgSecret
	cfgSecret = nil
	cfgMu.Unlock()
	defer func() {
		cfgMu.Lock()
		cfgSecret = saved
		cfgMu.Unlock()
	}()
	if _, err := GenerateToken("x", 1); err == nil {
		t.Fatal("未初始化（空 Secret）时签发应报错而非产出可伪造 token")
	}
}
