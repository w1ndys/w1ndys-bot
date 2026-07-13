// 📌 影响范围：无；使用内存仓库验证插件功能元数据授权，不访问数据库。
package admin

import (
	"context"
	"errors"
	"testing"
)

// TestListPluginFeaturesRequiresSuperAdmin 验证功能元数据只允许最高管理员读取。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存授权替身并可能终止当前测试。
func TestListPluginFeaturesRequiresSuperAdmin(t *testing.T) {
	service := NewService(&fakeRepository{}, nil, nil, nil, nil, &fakeAuthorizer{})
	_, err := service.ListPluginFeatures(context.Background(), Actor{ID: "200", Channel: ChannelWebUI}, "ping")
	// [决策理由] 未授权用户不能读取命令与默认权限元数据。
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPluginFeatures() error = %v, want ErrForbidden", err)
	}

	// >>> 数据演变示例
	// 1. 非管理员200+ping -> ErrForbidden。
	// 2. 管理员100+ping -> 可进入Repository。
}

// TestListPluginFeaturesRejectsEmptyPlugin 验证空插件名在仓库前被拒绝。
// @param t：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：执行内存替身并可能终止当前测试。
func TestListPluginFeaturesRejectsEmptyPlugin(t *testing.T) {
	service := NewService(&fakeRepository{}, nil, nil, nil, nil, &fakeAuthorizer{allowed: map[string]bool{"100": true}})
	_, err := service.ListPluginFeatures(context.Background(), Actor{ID: "100", Channel: ChannelWebUI}, "")
	// [决策理由] 空插件名必须映射为稳定未找到错误。
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("ListPluginFeatures(empty) error = %v", err)
	}

	// >>> 数据演变示例
	// 1. 管理员100+空插件 -> ErrPluginNotFound。
	// 2. 管理员100+ping -> Repository查询。
}
