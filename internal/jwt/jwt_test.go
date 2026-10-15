package jwt

import (
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	secret := []byte("dev-secret")
	token, err := Sign(map[string]any{"id": 1, "username": "admin"}, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Verify(token, secret)
	if err != nil {
		t.Fatal(err)
	}
	if claims["username"] != "admin" {
		t.Fatalf("username 声明丢失: %v", claims["username"])
	}
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	secret := []byte("dev-secret")
	token, _ := Sign(map[string]any{"preview": "abc"}, secret, time.Hour)
	if _, err := Verify(token[:len(token)-1]+"x", secret); err == nil {
		t.Fatal("篡改签名应校验失败")
	}
	if _, err := Verify(token, []byte("other-secret")); err == nil {
		t.Fatal("错误密钥应校验失败")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	token, _ := Sign(map[string]any{"preview": "abc"}, []byte("s"), -time.Minute)
	if _, err := Verify(token, []byte("s")); err == nil {
		t.Fatal("过期令牌应校验失败")
	}
}
