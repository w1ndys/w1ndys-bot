// 📌 影响范围：通过注入的 Messenger 引用回复 QQ 消息；向目标插件架构提供 Echo 规格。
package echo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
)

type echoPlugin struct {
	messenger plugin.Messenger
	config    atomic.Pointer[configSnapshot]
}

type configSnapshot struct {
	ResponsePrefix string `json:"response_prefix"`
}

// Spec 返回编译期 Echo 插件规格，由 cmd/bot 装入 SpecCatalog。
// 触发词与允许身份由代码持有，数据库只保存管理员的开关意图。
func Spec(messenger plugin.Messenger) (plugin.PluginSpec, error) {
	// Echo 的唯一行为是引用回复，缺少 Messenger 时不能构建可运行规格。
	if messenger == nil {
		return plugin.PluginSpec{}, fmt.Errorf("%s 缺少 Messenger", pluginKey)
	}
	implementation := &echoPlugin{messenger: messenger}
	// 启动恢复会在插件进入 Ready 前发布持久化配置；此处先放入零值快照，防御未接线的测试实例。
	implementation.config.Store(&configSnapshot{})
	return plugin.PluginSpec{
		Key:         pluginKey,
		DisplayName: pluginDisplayName,
		Description: pluginDescription,
		Config: &plugin.ConfigSpec{
			Schema: plugin.ConfigSchema{Fields: []plugin.ConfigField{{
				Key:         configKeyResponsePrefix,
				DisplayName: "回复前缀",
				Description: "添加到每条 Echo 回复之前的文本",
				Type:        plugin.FieldString,
				Default:     json.RawMessage(`""`),
			}}},
			Validate: implementation.validateConfig,
			Apply:    implementation.applyConfig,
		},
		Commands: []plugin.CommandSpec{{
			Key:          commandKey,
			DisplayName:  commandDisplayName,
			Description:  commandDescription,
			Triggers:     []string{triggerEcho, triggerEchoCN},
			Scope:        plugin.CommandScopeGroup,
			AllowedRoles: plugin.Roles(plugin.RoleSuperAdmin, plugin.RoleGroupOwner, plugin.RoleGroupAdmin, plugin.RoleGroupMember),
			Handler:      implementation.handle,
		}},
	}, nil
}

// validateConfig 校验 Echo 配置的领域约束。
// 平台已按 Schema 规范化字段类型，此处只判断插件自身的业务边界。
func (p *echoPlugin) validateConfig(ctx context.Context, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var candidate configSnapshot
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return fmt.Errorf("解析 echo 配置: %w", err)
	}
	// 限制前缀长度可避免单条命令无界放大回复负载。
	if len([]rune(candidate.ResponsePrefix)) > maxResponsePrefixRunes {
		return fmt.Errorf("response_prefix 不能超过 %d 个字符", maxResponsePrefixRunes)
	}
	return nil
}

// applyConfig 原子发布 Echo 的不可变配置快照；解码失败时保留旧快照。
func (p *echoPlugin) applyConfig(ctx context.Context, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var candidate configSnapshot
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return fmt.Errorf("解析 echo 配置: %w", err)
	}
	p.config.Store(&candidate)
	return nil
}

// handle 将命令参数原样作为引用回复发送；参数为空时回复当前触发词的用法。
func (p *echoPlugin) handle(command plugin.CommandContext) error {
	response := command.Arguments
	if response == "" {
		response = fmt.Sprintf(usageTemplate, command.Trigger)
	}
	// 快照由 applyConfig 原子替换，热路径只读当前指针，不访问数据库。
	if current := p.config.Load(); current != nil {
		response = current.ResponsePrefix + response
	}
	_, err := p.messenger.ReplyToMessage(command.Context, command.Message, command.Message.MessageID, response)
	if err != nil {
		return fmt.Errorf("发送 echo 回复: %w", err)
	}
	return nil
}
