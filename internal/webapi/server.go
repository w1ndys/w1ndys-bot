// 📌 影响范围：读取系统时间和 crypto/rand；校验管理员快照；签发及验证 HMAC-SHA256 JWT；写入 HTTP 响应与访问日志。
package webapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/w1ndys/w1ndys-bot/internal/admin"
	projectauth "github.com/w1ndys/w1ndys-bot/internal/auth"
	projectlogger "github.com/w1ndys/w1ndys-bot/pkg/logger"
)

const tokenLifetime = 12 * time.Hour

// ApplicationName 是 WebUI 固定展示名称，不开放运行时修改。
const ApplicationName = "w1ndys-bot-webui"

type contextKey string

const actorContextKey contextKey = "webui_actor"

// AdminResolver 定义 WebUI 登录及请求期间所需的管理员快照能力。
type AdminResolver interface {
	Resolve(string) (admin.SystemAdmin, bool)
}

// Server 提供 WebUI 认证 HTTP 接口。
type Server struct {
	passwordHash string
	jwtSecret    []byte
	admins       AdminResolver
	now          func() time.Time
}

type loginRequest struct {
	QQ       string `json:"qq"`
	Password string `json:"password"`
}

type tokenClaims struct {
	Subject   string `json:"sub"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

type responseEnvelope struct {
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// New 创建 WebUI API 服务并在内存中准备环境密码哈希。
// @param password：环境变量中的共享密码；jwtSecret：JWT 签名密钥；admins：管理员快照解析器。
// @returns 可注册到 HTTP 路由的 Server，或配置强度错误。
// ⚠️副作用说明：读取系统加密随机源并执行 Argon2id 哈希。
func New(password string, jwtSecret string, admins AdminResolver) (*Server, error) {
	// [决策理由] JWT 密钥过短会降低离线伪造成本，必须在监听端口前拒绝启动。
	if len([]byte(jwtSecret)) < 32 {
		return nil, errors.New("JWT_SECRET 不能少于32字节")
	}
	// [决策理由] 管理员解析器缺失时无法完成服务端授权确认。
	if admins == nil {
		return nil, errors.New("管理员解析器不能为空")
	}
	passwordHash, err := projectauth.HashPassword(password)
	// [决策理由] 环境密码不符合强度要求或随机源失败时禁止开放登录接口。
	if err != nil {
		return nil, fmt.Errorf("准备 WebUI 密码: %w", err)
	}
	server := &Server{passwordHash: passwordHash, jwtSecret: []byte(jwtSecret), admins: admins, now: time.Now}

	// >>> 数据演变示例
	// 1. 强密码+32字节密钥+Resolver -> Argon2id哈希 -> Server,nil。
	// 2. 短JWT密钥 -> 强度检查 -> nil,error。
	return server, nil
}

// Handler 返回包含登录、当前身份和通用中间件的 HTTP 路由。
// @param 无。
// @returns 可挂载在 /api/ 路径下的 http.Handler。
// ⚠️副作用说明：创建内存路由；请求到达后会写响应和访问日志。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.Handle("GET /api/auth/me", s.authenticate(http.HandlerFunc(s.me)))
	handler := s.middleware(mux)

	// >>> 数据演变示例
	// 1. POST /api/auth/login -> login处理器。
	// 2. GET /api/auth/me无Token -> authenticate -> 401。
	return handler
}

// login 校验管理员 QQ 与共享密码并签发 JWT。
// @param writer：响应写入器；request：JSON 登录请求。
// @returns 无。
// ⚠️副作用说明：读取请求体、执行 Argon2id 校验并写入 JSON 响应。
func (s *Server) login(writer http.ResponseWriter, request *http.Request) {
	var input loginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	// [决策理由] 登录载荷必须是单个、字段明确的 JSON 对象，避免歧义输入。
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "登录参数格式无效")
		return
	}
	account, exists := s.admins.Resolve(input.QQ)
	matched, verifyErr := projectauth.VerifyPassword(input.Password, s.passwordHash)
	// [决策理由] QQ 与密码使用同一模糊错误，避免暴露管理员账号是否存在。
	if verifyErr != nil || !matched || !exists {
		writeError(writer, http.StatusUnauthorized, "invalid_credentials", "QQ号或密码错误")
		return
	}
	token, err := s.sign(input.QQ)
	// [决策理由] 签名失败表示服务器无法建立可信会话，不能返回部分登录结果。
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal_error", "签发登录凭证失败")
		return
	}
	writeJSON(writer, http.StatusOK, responseEnvelope{Data: map[string]any{"token": token, "expires_in": int64(tokenLifetime.Seconds()), "admin": account}})

	// >>> 数据演变示例
	// 1. 启用管理员+正确密码 -> JWT -> 200及管理员信息。
	// 2. 非管理员+正确密码 -> 固定认证失败响应 -> 401。
}

// me 返回 JWT 对应的当前启用管理员身份。
// @param writer：响应写入器；request：已通过鉴权且携带 Actor 的请求。
// @returns 无。
// ⚠️副作用说明：读取请求上下文并写入 JSON 响应。
func (s *Server) me(writer http.ResponseWriter, request *http.Request) {
	actor, exists := request.Context().Value(actorContextKey).(admin.SystemAdmin)
	// [决策理由] 鉴权中间件未注入身份表示路由装配错误，按未授权安全失败。
	if !exists {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "登录凭证无效")
		return
	}
	writeJSON(writer, http.StatusOK, responseEnvelope{Data: actor})

	// >>> 数据演变示例
	// 1. context含管理员100 -> 200 data{user_id:100}。
	// 2. context无身份 -> 401 unauthorized。
}

// authenticate 验证 Bearer JWT 并再次确认管理员仍处于启用状态。
// @param next：鉴权成功后调用的处理器。
// @returns 包装后的 HTTP 处理器。
// ⚠️副作用说明：读取请求头和管理员快照；失败时写入401响应。
func (s *Server) authenticate(next http.Handler) http.Handler {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := request.Header.Get("Authorization")
		// [决策理由] 仅接受标准 Bearer 方案，避免把其他认证头误作 JWT。
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(writer, http.StatusUnauthorized, "unauthorized", "缺少登录凭证")
			return
		}
		claims, err := s.verify(strings.TrimPrefix(header, "Bearer "))
		// [决策理由] 签名、结构或有效期任一失败都不能建立可信身份。
		if err != nil {
			writeError(writer, http.StatusUnauthorized, "unauthorized", "登录凭证无效或已过期")
			return
		}
		account, exists := s.admins.Resolve(claims.Subject)
		// [决策理由] 每次请求查询内存快照，使管理员禁用后已签发 Token 立即失效。
		if !exists {
			writeError(writer, http.StatusUnauthorized, "unauthorized", "管理员账号已停用")
			return
		}
		ctx := context.WithValue(request.Context(), actorContextKey, account)
		next.ServeHTTP(writer, request.WithContext(ctx))

		// >>> 数据演变示例
		// 1. 有效Token+启用管理员 -> 注入account -> next。
		// 2. 有效Token+管理员已禁用 -> 401 -> 不调用next。
	})

	// >>> 数据演变示例
	// 1. me处理器 -> authenticate包装 -> 受保护处理器。
	// 2. nil身份请求 -> 包装器校验失败 -> 401。
	return handler
}

// sign 为管理员签发固定12小时有效的 HMAC JWT。
// @param subject：管理员 QQ 号。
// @returns JWT 字符串或 JSON 编码错误。
// ⚠️副作用说明：读取当前时间。
func (s *Server) sign(subject string) (string, error) {
	now := s.now()
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	// [决策理由] 虽然静态头通常不会失败，仍需保持签名链路错误完整。
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(tokenClaims{Subject: subject, IssuedAt: now.Unix(), ExpiresAt: now.Add(tokenLifetime).Unix()})
	// [决策理由] Claims 编码失败时不存在可安全签发的载荷。
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature := s.signature(unsigned)

	// >>> 数据演变示例
	// 1. QQ100+时间0 -> header.payload+HMAC -> 三段JWT。
	// 2. QQ200+时间1 -> 不同payload和签名 -> 独立JWT。
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// verify 校验 JWT 结构、HMAC 签名和时间范围。
// @param token：Bearer Token 原文。
// @returns 可信 Claims 或校验错误。
// ⚠️副作用说明：读取当前时间。
func (s *Server) verify(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	// [决策理由] JWT 必须严格包含 header、payload、signature 三段。
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("JWT结构无效")
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	// [决策理由] 无法解码的签名不能进行可信比较。
	if err != nil || !hmac.Equal(provided, s.signature(parts[0]+"."+parts[1])) {
		return tokenClaims{}, errors.New("JWT签名无效")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	// [决策理由] 必须固定算法为HS256，防止算法替换攻击。
	if err != nil || string(headerBytes) != `{"alg":"HS256","typ":"JWT"}` {
		return tokenClaims{}, errors.New("JWT头无效")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	// [决策理由] Claims 必须可解析并拒绝未知字段。
	if err != nil {
		return tokenClaims{}, errors.New("JWT载荷无效")
	}
	var claims tokenClaims
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	// [决策理由] 身份和时间字段缺失时 Token 不能用于授权。
	if err := decoder.Decode(&claims); err != nil || claims.Subject == "" || claims.ExpiresAt == 0 || claims.IssuedAt == 0 {
		return tokenClaims{}, errors.New("JWT声明无效")
	}
	now := s.now().Unix()
	// [决策理由] 已过期 Token 或签发时间明显位于未来都不可信。
	if claims.ExpiresAt <= now || claims.IssuedAt > now+60 {
		return tokenClaims{}, errors.New("JWT已过期或时间无效")
	}

	// >>> 数据演变示例
	// 1. 三段Token+正确HMAC+未过期 -> Claims,nil。
	// 2. payload被修改 -> HMAC不匹配 -> error。
	return claims, nil
}

// signature 计算 JWT 未签名内容的 HMAC-SHA256。
// @param unsigned：JWT 的 header.payload 部分。
// @returns 32字节消息认证码。
// ⚠️副作用说明：无。
func (s *Server) signature(unsigned string) []byte {
	hash := hmac.New(sha256.New, s.jwtSecret)
	_, _ = hash.Write([]byte(unsigned))
	result := hash.Sum(nil)

	// >>> 数据演变示例
	// 1. secretA+payloadA -> HMAC A。
	// 2. secretA+payloadB -> HMAC B。
	return result
}

// middleware 注入请求ID、安全响应头及访问日志。
// @param next：业务路由处理器。
// @returns 包装后的 HTTP 处理器。
// ⚠️副作用说明：读取随机源、修改响应头并写入访问日志。
func (s *Server) middleware(next http.Handler) http.Handler {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		requestID := request.Header.Get("X-Request-ID")
		// [决策理由] 客户端未提供追踪标识时生成本地随机ID，便于关联审计和故障日志。
		if requestID == "" {
			buffer := make([]byte, 12)
			// [决策理由] 随机源异常时仍使用明确占位符，不影响 API 可用性。
			if _, err := rand.Read(buffer); err != nil {
				requestID = "unavailable"
			} else {
				requestID = hex.EncodeToString(buffer)
			}
		}
		writer.Header().Set("X-Request-ID", requestID)
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(writer, request)
		projectlogger.Info("WebAPI请求", "method", request.Method, "path", request.URL.Path, "request_id", requestID, "duration", time.Since(started))

		// >>> 数据演变示例
		// 1. 无X-Request-ID请求 -> 随机ID+安全头 -> 业务响应+访问日志。
		// 2. X-Request-ID=abc -> 保留abc -> 业务响应+关联日志。
	})

	// >>> 数据演变示例
	// 1. mux -> middleware包装 -> 带安全头处理器。
	// 2. login请求 -> 包装器 -> mux -> login -> 日志。
	return handler
}

// writeJSON 写入统一 JSON 响应。
// @param writer：响应写入器；status：HTTP 状态码；value：响应对象。
// @returns 无。
// ⚠️副作用说明：设置响应头并写入响应体。
func writeJSON(writer http.ResponseWriter, status int, value responseEnvelope) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	// [决策理由] 响应头已发送后编码错误无法改写状态，仅记录故障供排查。
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		projectlogger.Error("编码WebAPI响应失败", "error", err)
	}

	// >>> 数据演变示例
	// 1. status=200+data -> application/json -> 200 JSON。
	// 2. status=401+error -> application/json -> 401 JSON。
}

// writeError 写入统一 API 错误结构。
// @param writer：响应写入器；status：HTTP 状态码；code：稳定错误码；message：用户消息。
// @returns 无。
// ⚠️副作用说明：写入 JSON HTTP 响应。
func writeError(writer http.ResponseWriter, status int, code string, message string) {
	writeJSON(writer, status, responseEnvelope{Error: &apiError{Code: code, Message: message}})

	// >>> 数据演变示例
	// 1. 401+unauthorized -> error对象 -> JSON响应。
	// 2. 400+invalid_request -> error对象 -> JSON响应。
}
