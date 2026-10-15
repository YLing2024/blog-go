package bcrypt

import "testing"

// 固定向量由旧实现所用的 bcryptjs 生成（见 docs/DB.md 的兼容说明），用于锁定字节级一致。
var vectors = []struct {
	password string
	hash     string
}{
	{"admin", "$2a$10$q97ndFLpUJQng/W.v3bOYehXcLRa92pURsczrYb.4L3gV4YCj7z7u"},
	{"hello world", "$2a$10$5OjUTZdt/rAKn7FONqZf9OR9y9yf.h0zJDirY4t.U3TmMd447kiAO"},
	{"中文口令", "$2a$10$onttxHIsDenAPxbfOpRPx.MLmqJRD.YNBKcpyX9lOLmkRG0IVFdcS"},
	{"", "$2a$10$9w95BI.UA71wH084g/TL3upfHjSgA4s1LJt2FZaQJrs3kS.ycvuEq"},
	{"p@ssw0rd-1234567890", "$2a$10$FpbOQNr/1.ppFhqDTLmvTO0W02Ra4Cmeke0UmtLkLez75y3tXvXUm"},
}

func TestCompareMatchesBcryptjs(t *testing.T) {
	for _, v := range vectors {
		if !Compare(v.password, v.hash) {
			t.Errorf("Compare(%q) 应为 true", v.password)
		}
		if Compare(v.password+"x", v.hash) {
			t.Errorf("Compare(%q 加错误后缀) 应为 false", v.password)
		}
	}
}

func TestCompareSupportsRevisions(t *testing.T) {
	// 同一算法下 $2b$ / $2y$ 前缀必须同样可校验（与 bcryptjs 的修订号处理一致）。
	const h = "$2b$10$q97ndFLpUJQng/W.v3bOYehXcLRa92pURsczrYb.4L3gV4YCj7z7u"
	if !Compare("admin", h) {
		t.Fatal("$2b$ 前缀应可校验")
	}
	const y = "$2y$10$q97ndFLpUJQng/W.v3bOYehXcLRa92pURsczrYb.4L3gV4YCj7z7u"
	if !Compare("admin", y) {
		t.Fatal("$2y$ 前缀应可校验")
	}
}

func TestHashRoundTrip(t *testing.T) {
	h, err := Hash("admin", DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != hashLen || h[:7] != "$2a$10$" {
		t.Fatalf("哈希形态非法: %s", h)
	}
	if !Compare("admin", h) {
		t.Fatal("自产哈希应可校验")
	}
	if Compare("nope", h) {
		t.Fatal("错误口令不应通过")
	}
}

func TestHashRejectsBadCost(t *testing.T) {
	if _, err := Hash("x", 3); err == nil {
		t.Fatal("成本 3 应报错")
	}
	if _, err := Hash("x", 32); err == nil {
		t.Fatal("成本 32 应报错")
	}
}
