package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("密码哈希包含明文")
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("正确密码未通过验证")
	}
	if VerifyPassword(hash, "wrong password value") {
		t.Fatal("错误密码通过验证")
	}
}

func TestPasswordPolicy(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("弱密码应被拒绝")
	}
}
