// 📌 影响范围：读取 crypto/rand；执行 Argon2id 哈希测试，不访问数据库。
package auth

import "testing"

// TestHashAndVerifyPassword 验证密码哈希可匹配正确密码并拒绝错误密码。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：读取系统随机源并执行三次 Argon2id 计算。
func TestHashAndVerifyPassword(t *testing.T) {
	encoded, err := HashPassword("correct-horse-battery-staple")
	// [决策理由] 合法强密码必须成功生成编码。
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	matched, err := VerifyPassword("correct-horse-battery-staple", encoded)
	// [决策理由] 正确密码必须匹配且无解析错误。
	if err != nil || !matched {
		t.Fatalf("VerifyPassword(correct) = %v,%v", matched, err)
	}
	matched, err = VerifyPassword("wrong-password-value", encoded)
	// [决策理由] 错误密码必须安全返回不匹配。
	if err != nil || matched {
		t.Fatalf("VerifyPassword(wrong) = %v,%v", matched, err)
	}

	// >>> 数据演变示例
	// 1. correct + encoded -> true,nil。
	// 2. wrong + encoded -> false,nil。
}

// TestHashPasswordRejectsWeakPassword 验证短密码在随机源前被拒绝。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：可能终止当前测试。
func TestHashPasswordRejectsWeakPassword(t *testing.T) {
	_, err := HashPassword("short")
	// [决策理由] 少于12字符必须返回强度错误。
	if err == nil {
		t.Fatal("HashPassword(short) error = nil")
	}

	// >>> 数据演变示例
	// 1. short -> 长度5 -> error。
	// 2. 12字符以上 -> 进入随机盐和Argon2id流程。
}
