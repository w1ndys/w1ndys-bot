// 📌 影响范围：读取 PostgreSQL 关键词规则并通过 Messenger 引用回复群消息；注册 keyword_reply 插件。
package keywordreply

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
	"github.com/w1ndys/w1ndys-bot/internal/ws"
)

var manifest = plugin.Manifest{
	Name: pluginName, DisplayName: pluginDisplayName, Description: pluginDescription,
	Priority: 0, GroupControllable: true,
}

type ruleLoader interface {
	LoadEnabled(context.Context) (map[string]string, error)
}

type implementation struct {
	messenger  plugin.Messenger
	repository ruleLoader
	resources  []plugin.AdminResourceRegistration
	rules      atomic.Pointer[map[string]string]
	snapshotMu sync.Mutex
}

// Name 返回插件稳定名称。
// @param 无。
// @returns keyword_reply。
// ⚠️副作用说明：无。
func (p *implementation) Name() string {
	// >>> 数据演变示例
	// 1. 新实例 -> Name -> keyword_reply。
	// 2. 生命周期切换后 -> Name -> keyword_reply。
	return pluginName
}

// Handle 对群消息执行不修剪、不折叠大小写的关键词完全匹配。
// @param ctx：事件处理上下文；event：OneBot事件。
// @returns 非目标事件或未命中返回nil，回复成功返回ErrStopPropagation，发送失败返回错误。
// ⚠️副作用说明：命中时通过NapCat引用回复原消息。
func (p *implementation) Handle(ctx context.Context, event ws.Event) error {
	message, ok := event.(*ws.MessageEvent)
	// [决策理由] 关键词规则只面向消息事件，其他通知和元事件必须忽略。
	if !ok {
		return nil
	}
	// [决策理由] OneBot 的 message_sent 可能包含机器人自身消息，必须只处理入站 message 以避免自动回复循环。
	if message.PostType != "message" {
		return nil
	}
	// [决策理由] 产品范围明确为群聊，私聊和其他消息类型不得触发。
	if message.MessageType != "group" {
		return nil
	}
	snapshot := p.rules.Load()
	// [决策理由] OnEnable加载前或禁用清空后的零快照不包含可执行规则。
	if snapshot == nil {
		return nil
	}
	reply, matched := (*snapshot)[message.RawMessage]
	// [决策理由] Map键直接查询保持原始文本完全相等语义，不做trim或大小写折叠。
	if !matched {
		return nil
	}
	_, err := p.messenger.ReplyToMessage(ctx, message, message.MessageID, reply)
	// [决策理由] 发送故障必须交由统一事件日志链路处理，不能误报为已消费。
	if err != nil {
		return fmt.Errorf("发送关键词回复: %w", err)
	}

	// >>> 数据演变示例
	// 1. 入站group RawMessage="你好"+快照{"你好":"你好呀"} -> 引用回复 -> 停止传播。
	// 2. message_sent或RawMessage=" 你好" -> 忽略或精确查询未命中 -> nil。
	return plugin.ErrStopPropagation
}

// OnEnable 从数据库发布全部已启用规则的不可变快照。
// @param ctx：控制数据库加载的生命周期上下文。
// @returns 数据库读取错误或nil。
// ⚠️副作用说明：查询关键词表并原子替换事件处理快照。
func (p *implementation) OnEnable(ctx context.Context) error {
	// >>> 数据演变示例
	// 1. DB含启用规则a->b -> LoadEnabled -> 快照{a:b}。
	// 2. DB无启用规则 -> LoadEnabled -> 空快照。
	return p.reload(ctx)
}

// OnDisable 清空运行时规则快照。
// @param context.Context：生命周期上下文。
// @returns nil。
// ⚠️副作用说明：原子发布空规则快照；不修改数据库。
func (p *implementation) OnDisable(context.Context) error {
	p.snapshotMu.Lock()
	defer p.snapshotMu.Unlock()
	empty := map[string]string{}
	p.rules.Store(&empty)

	// >>> 数据演变示例
	// 1. 快照{a:b} -> OnDisable -> 空快照。
	// 2. 空快照 -> OnDisable -> 仍为空。
	return nil
}

// AdminResources 声明关键词规则的通用 CRUD 管理资源。
// @param 无。
// @returns rules资源声明与绑定处理器的副本。
// ⚠️副作用说明：无。
func (p *implementation) AdminResources() []plugin.AdminResourceRegistration {
	result := append([]plugin.AdminResourceRegistration(nil), p.resources...)

	// >>> 数据演变示例
	// 1. 新插件 -> AdminResources -> [rules]。
	// 2. 调用方修改返回切片 -> 内部绑定保持不变。
	return result
}

// reload 加载并原子发布规则快照。
// @param ctx：数据库读取上下文。
// @returns 加载错误或nil。
// ⚠️副作用说明：查询数据库并替换后续Handle读取的快照。
func (p *implementation) reload(ctx context.Context) error {
	p.snapshotMu.Lock()
	defer p.snapshotMu.Unlock()
	rules, err := p.repository.LoadEnabled(ctx)
	// [决策理由] 查询失败时必须保留旧快照，避免部分或空数据覆盖可用运行态。
	if err != nil {
		return fmt.Errorf("加载关键词回复规则: %w", err)
	}
	cloned := make(map[string]string, len(rules))
	for keyword, reply := range rules {
		cloned[keyword] = reply
	}
	p.rules.Store(&cloned)

	// >>> 数据演变示例
	// 1. 仓库{a:b} -> clone -> 原子发布{a:b}。
	// 2. 仓库错误+旧快照{x:y} -> 不Store -> 保持{x:y}。
	return nil
}

// applyRuleChange 基于已提交事务的权威前后像串行发布新快照。
// @param before：更新或删除前规则，可为空；after：创建或更新后规则，可为空。
// @returns 无。
// ⚠️副作用说明：复制当前规则map并原子发布；与其他CRUD快照变更串行。
func (p *implementation) applyRuleChange(before *ruleData, after *ruleData) {
	p.snapshotMu.Lock()
	defer p.snapshotMu.Unlock()
	current := p.rules.Load()
	next := make(map[string]string)
	// [决策理由] 防御零值实例，同时避免修改任何已被Handle读取的旧map。
	if current != nil {
		for keyword, reply := range *current {
			next[keyword] = reply
		}
	}
	// [决策理由] 更新更名、禁用和删除都必须先移除旧关键词。
	if before != nil {
		delete(next, before.Keyword)
	}
	// [决策理由] 只有事务提交后的启用规则才能进入消息热路径。
	if after != nil && after.Enabled {
		next[after.Keyword] = after.ReplyContent
	}
	p.rules.Store(&next)

	// >>> 数据演变示例
	// 1. before{old,true}+after{new,true} -> 删除old、加入new -> 原子发布。
	// 2. before{a,true}+after{a,false}或after=nil -> 删除a -> 原子发布。
}

// newPlugin 使用数据库和消息依赖构建关键词回复插件。
// @param runtime：主程序注入的运行时依赖。
// @returns 可运行插件或依赖缺失错误。
// ⚠️副作用说明：创建PostgreSQL仓库，不立即查询数据库。
func newPlugin(runtime plugin.Runtime) (plugin.Plugin, error) {
	// [决策理由] 命中规则后必须具备引用回复能力。
	if runtime.Messenger == nil {
		return nil, fmt.Errorf("%s 缺少 Messenger", pluginName)
	}
	// [决策理由] 生命周期和CRUD均依赖持久化规则表，不能以空数据库降级启动。
	if runtime.Database == nil {
		return nil, fmt.Errorf("%s 缺少 Database", pluginName)
	}
	repository := newPostgresRepository(runtime.Database)
	result := newImplementation(runtime.Messenger, repository)

	// >>> 数据演变示例
	// 1. Runtime{Messenger,Database} -> repository+implementation -> nil错误。
	// 2. Runtime缺Database -> nil插件 -> 依赖错误。
	return result, nil
}

// newImplementation 组装可替换仓库的插件实例。
// @param messenger：引用回复客户端；repository：规则读写仓库。
// @returns 默认空快照且声明rules资源的实例。
// ⚠️副作用说明：仅分配内存，不查询数据库。
func newImplementation(messenger plugin.Messenger, repository repository) *implementation {
	result := &implementation{messenger: messenger, repository: repository}
	empty := map[string]string{}
	result.rules.Store(&empty)
	result.resources = []plugin.AdminResourceRegistration{{Descriptor: rulesDescriptor(), Handler: &resourceHandler{repository: repository, applyChange: result.applyRuleChange}}}

	// >>> 数据演变示例
	// 1. fake仓库+messenger -> 空快照实例+rules处理器。
	// 2. PostgreSQL仓库+messenger -> 待OnEnable加载的实例。
	return result
}

// rulesDescriptor 构建通用管理端可渲染的规则字段声明。
// @param 无。
// @returns rules资源描述符。
// ⚠️副作用说明：分配字段切片。
func rulesDescriptor() plugin.AdminResource {
	descriptor := plugin.AdminResource{Key: resourceKey, DisplayName: "关键词规则", Description: "群消息完全等于关键词时引用回复指定内容", CanCreate: true, CanUpdate: true, CanDelete: true, MaxPageSize: 100, Fields: []plugin.ResourceField{
		{Key: "keyword", DisplayName: "关键词", Description: "完全匹配原始群消息，最多200字符", Type: plugin.ResourceFieldString, Required: true},
		{Key: "reply_content", DisplayName: "回复内容", Description: "命中后引用回复的文本，最多2000字符", Type: plugin.ResourceFieldMultiline, Required: true},
		{Key: "enabled", DisplayName: "启用", Type: plugin.ResourceFieldBoolean, Default: json.RawMessage(`true`)},
	}}

	// >>> 数据演变示例
	// 1. WebUI读取rules -> 三字段+CRUD能力 -> 渲染表格表单。
	// 2. enabled省略 -> Schema默认true。
	return descriptor
}

// init 注册内置关键词回复插件。
// @param 无。
// @returns 无。
// ⚠️副作用说明：修改全局Plugin Catalog；注册错误会panic。
func init() {
	plugin.MustRegister(plugin.Registration{Manifest: manifest, Factory: newPlugin})

	// >>> 数据演变示例
	// 1. cmd/bot空导入 -> 注册keyword_reply。
	// 2. 同名重复注册 -> panic暴露构建错误。
}
