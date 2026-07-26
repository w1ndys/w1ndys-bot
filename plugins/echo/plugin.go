// 📌 影响范围：通过注入的 Messenger 引用回复 QQ 消息；向目标插件架构提供 Echo 规格。
package echo

import (
	"fmt"

	"github.com/w1ndys/w1ndys-bot/internal/plugin"
)

type echoPlugin struct {
	messenger plugin.Messenger
}

// Spec 返回编译期 Echo 插件规格，由 cmd/bot 装入 SpecCatalog。
// 触发词与允许身份由代码持有，数据库只保存管理员的开关意图。
func Spec(messenger plugin.Messenger) (plugin.PluginSpec, error) {
	// Echo 的唯一行为是引用回复，缺少 Messenger 时不能构建可运行规格。
	if messenger == nil {
		return plugin.PluginSpec{}, fmt.Errorf("%s 缺少 Messenger", pluginKey)
	}
	implementation := &echoPlugin{messenger: messenger}
	return plugin.PluginSpec{
		Key:         pluginKey,
		DisplayName: pluginDisplayName,
		Description: pluginDescription,
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

// handle 将命令参数原样作为引用回复发送；参数为空时回复当前触发词的用法。
func (p *echoPlugin) handle(command plugin.CommandContext) error {
	response := command.Arguments
	if response == "" {
		response = fmt.Sprintf(usageTemplate, command.Trigger)
	}
	_, err := p.messenger.ReplyToMessage(command.Context, command.Message, command.Message.MessageID, response)
	if err != nil {
		return fmt.Errorf("发送 echo 回复: %w", err)
	}
	return nil
}
