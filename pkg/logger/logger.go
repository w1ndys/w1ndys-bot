// 📌 影响范围：向标准输出写入结构化日志；不读取环境变量或敏感配置。
package logger

import (
	"fmt"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 隔离业务代码与具体日志库。
type Logger interface {
	Debug(string, ...any)
	Info(string, ...any)
	Warn(string, ...any)
	Error(string, ...any)
	With(...any) Logger
	Sync() error
}

type zapLogger struct {
	inner *zap.Logger
}

var (
	defaultMu     sync.RWMutex
	defaultLogger Logger = &zapLogger{inner: zap.NewExample()}
)

// New 创建 zap 结构化日志器。
// @param level：debug、info、warn 或 error；format：text 或 json。
// @returns 项目 Logger 接口，或配置错误。
// ⚠️副作用说明：构造 zap 内部缓冲与输出句柄，日志默认写入标准输出。
func New(level string, format string) (Logger, error) {
	parsedLevel, err := parseLevel(level)
	// [决策理由] 无效级别容易导致关键日志被意外过滤，必须拒绝启动。
	if err != nil {
		return nil, err
	}
	encoding := strings.ToLower(format)
	// [决策理由] zap 仅支持 console 与 json 编码，项目配置 text 需映射为 console。
	if encoding == "text" {
		encoding = "console"
	}
	// [决策理由] 不支持的格式不能静默回退，否则容器日志采集行为不一致。
	if encoding != "console" && encoding != "json" {
		return nil, fmt.Errorf("不支持的 LOG_FORMAT: %q", format)
	}
	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(parsedLevel),
		Development:      false,
		Encoding:         encoding,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey: "time", LevelKey: "level", NameKey: "logger", CallerKey: "caller",
			MessageKey: "message", StacktraceKey: "stacktrace",
			EncodeLevel: zapcore.LowercaseLevelEncoder, EncodeTime: zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.StringDurationEncoder, EncodeCaller: zapcore.ShortCallerEncoder,
		},
	}
	inner, err := config.Build()
	// [决策理由] 输出句柄或编码器构造失败时日志器不可安全使用。
	if err != nil {
		return nil, fmt.Errorf("构建 zap 日志器: %w", err)
	}
	result := &zapLogger{inner: inner.With(zap.String("service", "w1ndys-bot"))}

	// >>> 数据演变示例
	// 1. info+json -> zap JSON Logger{service=w1ndys-bot}。
	// 2. verbose+text -> 级别校验失败 -> 返回错误。
	return result, nil
}

// SetDefault 设置项目全局日志器。
// @param value：后续业务日志使用的 Logger。
// @returns 无。
// ⚠️副作用说明：并发安全地替换进程全局日志器。
func SetDefault(value Logger) {
	// [决策理由] nil 日志器会让所有业务日志调用崩溃，因此拒绝替换。
	if value == nil {
		return
	}
	defaultMu.Lock()
	defaultLogger = value
	defaultMu.Unlock()

	// >>> 数据演变示例
	// 1. bootstrap -> SetDefault(zap) -> 后续 Info 使用 zap。
	// 2. 当前 logger -> SetDefault(nil) -> 保持当前 logger。
}

// Default 返回当前全局日志器。
// @param 无。
// @returns 并发安全读取的 Logger。
// ⚠️副作用说明：无。
func Default() Logger {
	defaultMu.RLock()
	result := defaultLogger
	defaultMu.RUnlock()

	// >>> 数据演变示例
	// 1. 默认 zap -> Default -> zap Logger。
	// 2. SetDefault(custom) -> Default -> custom Logger。
	return result
}

// Debug 写入调试日志。
// @param message：日志消息；fields：交替排列的键和值。
// @returns 无。
// ⚠️副作用说明：可能向日志输出写入数据。
func Debug(message string, fields ...any) {
	Default().Debug(message, fields...)

	// >>> 数据演变示例
	// 1. Debug("收到心跳","interval",30000) -> DEBUG 结构化日志。
	// 2. info 级别 -> Debug 被过滤 -> 无输出。
}

// Info 写入信息日志。
// @param message：日志消息；fields：交替排列的键和值。
// @returns 无。
// ⚠️副作用说明：可能向日志输出写入数据。
func Info(message string, fields ...any) {
	Default().Info(message, fields...)

	// >>> 数据演变示例
	// 1. Info("启动","port",18800) -> INFO 结构化日志。
	// 2. error 级别 -> Info 被过滤 -> 无输出。
}

// Warn 写入警告日志。
// @param message：日志消息；fields：交替排列的键和值。
// @returns 无。
// ⚠️副作用说明：可能向日志输出写入数据。
func Warn(message string, fields ...any) {
	Default().Warn(message, fields...)

	// >>> 数据演变示例
	// 1. Warn("连接关闭","code",1005) -> WARN 结构化日志。
	// 2. error 级别 -> Warn 被过滤 -> 无输出。
}

// Error 写入错误日志。
// @param message：日志消息；fields：交替排列的键和值。
// @returns 无。
// ⚠️副作用说明：向错误日志输出写入数据。
func Error(message string, fields ...any) {
	Default().Error(message, fields...)

	// >>> 数据演变示例
	// 1. Error("连接失败","error",err) -> ERROR 结构化日志。
	// 2. Error("配置失败") -> 无字段 ERROR 日志。
}

// With 创建包含公共字段的子日志器。
// @param fields：交替排列的键和值。
// @returns 派生 Logger。
// ⚠️副作用说明：无；仅创建附带字段的日志器。
func With(fields ...any) Logger {
	result := Default().With(fields...)

	// >>> 数据演变示例
	// 1. With("plugin","ping") -> 子日志器携带 plugin=ping。
	// 2. With() -> 行为等价的派生日志器。
	return result
}

// parseLevel 将配置文本转换为 zap 级别。
// @param value：日志级别文本。
// @returns zapcore.Level 或不支持错误。
// ⚠️副作用说明：无。
func parseLevel(value string) (zapcore.Level, error) {
	var level zapcore.Level
	// [决策理由] zap 原生解析能保持其支持级别语义，warning 作为项目兼容别名处理。
	if strings.EqualFold(value, "warning") {
		value = "warn"
	}
	// [决策理由] 无效文本必须向配置层返回清晰错误。
	if err := level.Set(strings.ToLower(value)); err != nil {
		return 0, fmt.Errorf("不支持的 LOG_LEVEL: %q", value)
	}

	// >>> 数据演变示例
	// 1. debug -> zapcore.DebugLevel,nil。
	// 2. verbose -> 返回不支持错误。
	return level, nil
}

// Debug 写入 zap 调试日志。
// @param message：日志消息；fields：键值字段。
// @returns 无。
// ⚠️副作用说明：可能写入 zap 输出。
func (l *zapLogger) Debug(message string, fields ...any) {
	l.inner.Debug(message, toFields(fields)...)

	// >>> 数据演变示例
	// 1. message+x=1 -> zap.Debug(message,x=1)。
	// 2. 无字段 -> zap.Debug(message)。
}

// Info 写入 zap 信息日志。
// @param message：日志消息；fields：键值字段。
// @returns 无。
// ⚠️副作用说明：可能写入 zap 输出。
func (l *zapLogger) Info(message string, fields ...any) {
	l.inner.Info(message, toFields(fields)...)

	// >>> 数据演变示例
	// 1. message+x=1 -> zap.Info(message,x=1)。
	// 2. 无字段 -> zap.Info(message)。
}

// Warn 写入 zap 警告日志。
// @param message：日志消息；fields：键值字段。
// @returns 无。
// ⚠️副作用说明：可能写入 zap 输出。
func (l *zapLogger) Warn(message string, fields ...any) {
	l.inner.Warn(message, toFields(fields)...)

	// >>> 数据演变示例
	// 1. message+x=1 -> zap.Warn(message,x=1)。
	// 2. 无字段 -> zap.Warn(message)。
}

// Error 写入 zap 错误日志。
// @param message：日志消息；fields：键值字段。
// @returns 无。
// ⚠️副作用说明：写入 zap 输出。
func (l *zapLogger) Error(message string, fields ...any) {
	l.inner.Error(message, toFields(fields)...)

	// >>> 数据演变示例
	// 1. message+error=err -> zap.Error(message,error=err)。
	// 2. 无字段 -> zap.Error(message)。
}

// With 创建 zap 子日志器。
// @param fields：键值字段。
// @returns 实现项目接口的 zap 子日志器。
// ⚠️副作用说明：无。
func (l *zapLogger) With(fields ...any) Logger {
	result := &zapLogger{inner: l.inner.With(toFields(fields)...)}

	// >>> 数据演变示例
	// 1. With(plugin=ping) -> 子日志器携带 plugin 字段。
	// 2. With() -> 新包装器共享同一 zap core。
	return result
}

// Sync 刷新 zap 缓冲。
// @param 无。
// @returns 输出同步错误。
// ⚠️副作用说明：刷新日志输出句柄。
func (l *zapLogger) Sync() error {
	err := l.inner.Sync()

	// >>> 数据演变示例
	// 1. 有缓冲日志 -> Sync -> 写入输出。
	// 2. stdout 不支持同步 -> 返回对应系统错误。
	return err
}

// toFields 将键值参数转换为 zap 字段。
// @param values：交替排列的键和值。
// @returns zap.Field 切片；非字符串键和缺失值被明确标记。
// ⚠️副作用说明：无。
func toFields(values []any) []zap.Field {
	fields := make([]zap.Field, 0, (len(values)+1)/2)
	for index := 0; index < len(values); index += 2 {
		key, ok := values[index].(string)
		// [决策理由] 结构化日志键必须为字符串，错误输入仍需可观察而不能 panic。
		if !ok {
			key = fmt.Sprintf("invalid_key_%d", index)
		}
		// [决策理由] 奇数参数缺少值时记录显式错误文本，避免数组越界。
		if index+1 >= len(values) {
			fields = append(fields, zap.String(key, "<missing>"))
			continue
		}
		fields = append(fields, zap.Any(key, values[index+1]))
	}

	// >>> 数据演变示例
	// 1. [group_id,1] -> [zap.Any(group_id,1)]。
	// 2. [orphan] -> [zap.String(orphan,<missing>)]。
	return fields
}
