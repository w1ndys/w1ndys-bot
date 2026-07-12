// 📌 影响范围：通过当前 NapCat WebSocket 连接发送 OneBot Action；维护进程内 pending 请求和 echo 序列。
package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

var ErrNotConnected = errors.New("NapCat WebSocket 未连接")
var ErrConnectionClosed = errors.New("NapCat WebSocket 连接已关闭")

// ActionRequest 表示发送给 NapCat 的 OneBot Action 请求。
type ActionRequest struct {
	Action string `json:"action"`
	Params any    `json:"params,omitempty"`
	Echo   string `json:"echo"`
}

// ActionResponse 表示 NapCat 返回的 OneBot Action 响应。
type ActionResponse struct {
	Status  string          `json:"status"`
	RetCode int             `json:"retcode"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Wording string          `json:"wording"`
	Echo    string          `json:"echo"`
}

// OK 判断 Action 是否成功。
// @param 无。
// @returns status=ok 且 retcode=0 时返回 true。
// ⚠️副作用说明：无。
func (r ActionResponse) OK() bool {
	// >>> 数据演变示例
	// 1. status=ok,retcode=0 -> true。
	// 2. status=failed,retcode=100 -> false。
	return r.Status == "ok" && r.RetCode == 0
}

type actionResult struct {
	response ActionResponse
	err      error
}

// ActionClient 将 WebSocket 上的 echo 响应包装成同步请求调用。
type ActionClient struct {
	connectionMu sync.RWMutex
	connection   *websocket.Conn
	writeMu      sync.Mutex
	pendingMu    sync.Mutex
	pending      map[string]chan actionResult
	instanceID   string
	sequence     atomic.Uint64
}

// NewActionClient 创建 Action Client 和本次进程的 echo 随机前缀。
// @param 无。
// @returns ActionClient；系统随机源不可用时返回错误。
// ⚠️副作用说明：读取系统加密随机源。
func NewActionClient() (*ActionClient, error) {
	random := make([]byte, 8)
	// [决策理由] 随机实例前缀避免进程重启后的迟到响应误匹配新请求。
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("生成 echo 实例标识: %w", err)
	}
	client := &ActionClient{
		pending:    make(map[string]chan actionResult),
		instanceID: hex.EncodeToString(random),
	}

	// >>> 数据演变示例
	// 1. 随机字节 8 个 -> 16 位十六进制 instanceID -> 空 pending Client。
	// 2. 随机源失败 -> 返回错误 -> 不创建 Client。
	return client, nil
}

// Call 发送 Action 并同步等待相同 echo 的响应。
// @param ctx：控制等待超时或取消；action：OneBot 操作名；params：操作参数。
// @returns 匹配的 ActionResponse，或连接、发送、取消错误。
// ⚠️副作用说明：写入 WebSocket，并临时修改 pending 请求表。
func (c *ActionClient) Call(ctx context.Context, action string, params any) (ActionResponse, error) {
	// [决策理由] 空 Action 无法由 NapCat 路由，应在占用 pending 资源前拒绝。
	if action == "" {
		return ActionResponse{}, errors.New("Action 名称不能为空")
	}
	echo := c.nextEcho()
	resultChannel := make(chan actionResult, 1)
	c.pendingMu.Lock()
	c.pending[echo] = resultChannel
	c.pendingMu.Unlock()
	defer c.removePending(echo)

	request := ActionRequest{Action: action, Params: params, Echo: echo}
	// [决策理由] 必须先登记 pending 再发送，避免极速响应先于等待项注册。
	if err := c.writeJSON(request); err != nil {
		return ActionResponse{}, err
	}

	select {
	case result := <-resultChannel:
		// [决策理由] 断线清理通过同一通道返回错误，调用方无需感知内部连接生命周期。
		if result.err != nil {
			return ActionResponse{}, result.err
		}
		return result.response, nil
	case <-ctx.Done():
		return ActionResponse{}, ctx.Err()
	}

	// >>> 数据演变示例
	// 1. Call(get_group_list) -> 注册 echo -> 发送 -> 匹配响应 -> 返回 Response。
	// 2. 发送后 ctx 超时 -> 删除 pending -> 返回 context deadline exceeded。
}

// attach 绑定当前 NapCat 连接。
// @param connection：已鉴权并升级成功的 WebSocket。
// @returns 无。
// ⚠️副作用说明：替换当前连接；存在旧连接时关闭旧连接并唤醒 pending 请求。
func (c *ActionClient) attach(connection *websocket.Conn) {
	c.connectionMu.Lock()
	previous := c.connection
	c.connection = connection
	c.connectionMu.Unlock()
	// [决策理由] 单账号 Client 只允许一个活动连接，新连接接管时旧连接必须退出。
	if previous != nil && previous != connection {
		previous.Close()
		c.failAll(ErrConnectionClosed)
	}

	// >>> 数据演变示例
	// 1. nil -> attach(connA) -> 当前连接 connA。
	// 2. connA -> attach(connB) -> 关闭 connA -> pending 收到断线错误。
}

// detach 仅在目标仍是当前连接时解除绑定。
// @param connection：正在结束读取循环的连接。
// @returns 无。
// ⚠️副作用说明：可能清空当前连接并唤醒所有 pending 请求。
func (c *ActionClient) detach(connection *websocket.Conn) {
	c.connectionMu.Lock()
	// [决策理由] 旧连接晚到的清理不能误删已接管的新连接。
	if c.connection != connection {
		c.connectionMu.Unlock()
		return
	}
	c.connection = nil
	c.connectionMu.Unlock()
	c.failAll(ErrConnectionClosed)

	// >>> 数据演变示例
	// 1. current=connA,detach(connA) -> current=nil -> pending 失败。
	// 2. current=connB,detach(connA) -> 忽略 -> connB 保持活动。
}

// handleResponse 识别并投递 Action 响应。
// @param payload：WebSocket JSON 数据。
// @returns JSON 含 echo 时返回 true，否则返回 false 交给事件解析。
// ⚠️副作用说明：可能从 pending 表取出等待项并写入响应通道。
func (c *ActionClient) handleResponse(payload []byte) bool {
	var envelope struct {
		Echo json.RawMessage `json:"echo"`
	}
	// [决策理由] 非 JSON 或不含 echo 的载荷不是可匹配响应，应交给事件解析处理。
	if err := json.Unmarshal(payload, &envelope); err != nil || len(envelope.Echo) == 0 {
		return false
	}
	var response ActionResponse
	// [决策理由] 含 echo 的载荷属于响应，字段解析失败时不能误当成事件。
	if err := json.Unmarshal(payload, &response); err != nil {
		return true
	}
	c.pendingMu.Lock()
	channel, exists := c.pending[response.Echo]
	// [决策理由] 原子取出并删除保证同一 echo 最多消费一次。
	if exists {
		delete(c.pending, response.Echo)
	}
	c.pendingMu.Unlock()
	// [决策理由] 超时后的迟到响应没有等待者，直接忽略避免阻塞读取循环。
	if !exists {
		return true
	}
	channel <- actionResult{response: response}

	// >>> 数据演变示例
	// 1. echo 存在 -> pending 原子删除 -> 通道收到响应 -> true。
	// 2. echo 已超时 -> 无 pending -> 忽略迟到响应 -> true。
	return true
}

// writeJSON 向当前连接串行写入 Action。
// @param value：可 JSON 编码的请求。
// @returns 未连接或 WebSocket 写入错误。
// ⚠️副作用说明：向 NapCat WebSocket 写入一帧 JSON。
func (c *ActionClient) writeJSON(value any) error {
	c.connectionMu.RLock()
	connection := c.connection
	c.connectionMu.RUnlock()
	// [决策理由] 无活动连接时请求不可能到达 NapCat，应立即失败。
	if connection == nil {
		return ErrNotConnected
	}
	c.writeMu.Lock()
	err := connection.WriteJSON(value)
	c.writeMu.Unlock()
	// [决策理由] 写入错误需要保留上下文供插件判断网络故障。
	if err != nil {
		return fmt.Errorf("发送 OneBot Action: %w", err)
	}

	// >>> 数据演变示例
	// 1. 活动连接 + Request -> 串行 WriteJSON -> nil。
	// 2. connection=nil -> ErrNotConnected。
	return nil
}

// nextEcho 生成进程内唯一且跨重启低碰撞的关联 ID。
// @param 无。
// @returns w1ndys-bot:<instance>:<sequence> 格式字符串。
// ⚠️副作用说明：原子递增请求序号。
func (c *ActionClient) nextEcho() string {
	sequence := c.sequence.Add(1)
	echo := fmt.Sprintf("w1ndys-bot:%s:%d", c.instanceID, sequence)

	// >>> 数据演变示例
	// 1. instance=abc,sequence=0 -> w1ndys-bot:abc:1。
	// 2. 再次调用 -> w1ndys-bot:abc:2。
	return echo
}

// removePending 删除指定等待项。
// @param echo：请求关联 ID。
// @returns 无。
// ⚠️副作用说明：修改 pending 表。
func (c *ActionClient) removePending(echo string) {
	c.pendingMu.Lock()
	delete(c.pending, echo)
	c.pendingMu.Unlock()

	// >>> 数据演变示例
	// 1. pending[echo] 存在 -> delete -> 不存在。
	// 2. pending[echo] 不存在 -> delete -> 保持不变。
}

// failAll 用同一错误唤醒并清空所有等待请求。
// @param err：连接级失败原因。
// @returns 无。
// ⚠️副作用说明：清空 pending 表并写入所有等待通道。
func (c *ActionClient) failAll(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan actionResult)
	c.pendingMu.Unlock()
	for _, channel := range pending {
		channel <- actionResult{err: err}
	}

	// >>> 数据演变示例
	// 1. pending=[A,B] -> 清空 Map -> A、B 通道收到断线错误。
	// 2. pending=[] -> 替换空 Map -> 无通道写入。
}
