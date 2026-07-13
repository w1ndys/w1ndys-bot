// 📌 影响范围：读取 crypto/rand 系统随机源；计算 Argon2id 密码哈希。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const argonMemory uint32 = 64 * 1024
const argonIterations uint32 = 3
const argonParallelism uint8 = 2
const saltLength uint32 = 16
const keyLength uint32 = 32

// HashPassword 使用 Argon2id 和随机盐生成不可逆密码编码。
// @param password：待哈希原始密码。
// @returns PHC 格式 Argon2id 编码或密码强度、随机源错误。
// ⚠️副作用说明：读取系统加密随机源并执行高成本内存哈希。
func HashPassword(password string) (string, error) {
	// [决策理由] 初始和新密码至少12字符，降低弱密码离线破解风险。
	if len([]rune(password)) < 12 {
		return "", errors.New("密码长度不能少于12个字符")
	}
	salt := make([]byte, saltLength)
	// [决策理由] 每个密码必须使用不可预测独立盐，随机源失败时禁止降级。
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, keyLength)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonIterations, argonParallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))

	// >>> 数据演变示例
	// 1. 12字符强密码 + 随机saltA -> PHC编码A。
	// 2. 同密码 + 随机saltB -> 不同PHC编码B。
	return encoded, nil
}

// VerifyPassword 使用编码参数验证原始密码。
// @param password：用户输入密码；encoded：数据库 PHC 格式哈希。
// @returns 密码匹配时 true；编码无效时返回错误。
// ⚠️副作用说明：执行高成本 Argon2id 内存哈希。
func VerifyPassword(password string, encoded string) (bool, error) {
	var version int
	var memory uint32
	var iterations uint32
	var parallelism uint8
	parts := strings.Split(encoded, "$")
	// [决策理由] PHC 编码必须具有固定算法、版本、参数、盐和哈希六段结构。
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("密码哈希格式无效")
	}
	// [决策理由] 参数解析失败表示数据库编码损坏，不能使用默认值猜测。
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, errors.New("密码哈希版本无效")
	}
	// [决策理由] 只接受当前 Argon2 版本，避免未知算法语义。
	if version != argon2.Version {
		return false, errors.New("密码哈希版本不受支持")
	}
	// [决策理由] 内存、迭代和并行参数必须完整解析。
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, errors.New("密码哈希参数无效")
	}
	// [决策理由] 限制编码参数，防止损坏或恶意数据库值触发过量内存和CPU消耗。
	if memory < 8*1024 || memory > 1024*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 16 {
		return false, errors.New("密码哈希参数超出安全范围")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	// [决策理由] 盐编码损坏时无法可靠复算哈希。
	if err != nil {
		return false, errors.New("密码哈希盐无效")
	}
	// [决策理由] 异常盐长度可能表示编码损坏，不应继续执行高成本哈希。
	if len(salt) < 8 || len(salt) > 64 {
		return false, errors.New("密码哈希盐长度无效")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	// [决策理由] 目标哈希编码损坏时必须拒绝验证。
	if err != nil || len(expected) == 0 {
		return false, errors.New("密码哈希值无效")
	}
	// [决策理由] 限制目标哈希长度，避免异常编码造成不必要计算或弱比较。
	if len(expected) < 16 || len(expected) > 64 {
		return false, errors.New("密码哈希长度无效")
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	matched := subtle.ConstantTimeCompare(actual, expected) == 1

	// >>> 数据演变示例
	// 1. 正确密码 -> 同salt和参数 -> 恒定时间比较true。
	// 2. 错误密码 -> 不同hash -> false,nil。
	return matched, nil
}
