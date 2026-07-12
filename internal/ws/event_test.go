// 📌 影响范围：仅解析内存中的 NapCat OneBot 事件 JSON，不访问外部状态。
package ws

import "testing"

// TestParseEventReturnsConcreteTypes 验证 NapCat 事件被解析为精确 Go 类型。
// @param testing.T：Go 测试上下文。
// @returns 无。
// ⚠️副作用说明：无。
func TestParseEventReturnsConcreteTypes(t *testing.T) {
	tests := []struct {
		payload string
		check   func(Event) bool
	}{
		{`{"post_type":"meta_event","meta_event_type":"heartbeat","interval":30000,"status":{"online":true,"good":true}}`, func(event Event) bool {
			heartbeat, ok := event.(*HeartbeatEvent)
			// [决策理由] 心跳必须拥有专用类型和状态载荷。
			if !ok || heartbeat.Status.Online == nil {
				return false
			}

			// >>> 数据演变示例
			// 1. heartbeat JSON -> *HeartbeatEvent -> true。
			// 2. 其他类型 -> 类型断言失败 -> false。
			return *heartbeat.Status.Online && heartbeat.Status.Good
		}},
		{`{"post_type":"notice","notice_type":"group_upload","file":{"id":"f1","name":"a.txt","size":12,"busid":7}}`, func(event Event) bool {
			upload, ok := event.(*GroupUploadNotice)

			// >>> 数据演变示例
			// 1. group_upload JSON -> *GroupUploadNotice -> file.id=f1。
			// 2. friend_add JSON -> 类型断言失败 -> false。
			return ok && upload.File.ID == "f1" && upload.File.BusID == 7
		}},
		{`{"post_type":"notice","notice_type":"group_ban","sub_type":"ban","duration":60}`, func(event Event) bool {
			ban, ok := event.(*GroupBanNotice)

			// >>> 数据演变示例
			// 1. group_ban JSON -> *GroupBanNotice -> duration=60。
			// 2. group_card JSON -> 类型断言失败 -> false。
			return ok && ban.Duration == 60 && ban.Name() == "notice.group_ban.ban"
		}},
	}
	for _, test := range tests {
		event, err := ParseEvent([]byte(test.payload))
		// [决策理由] 解析失败时无法验证具体类型，应立即失败。
		if err != nil {
			t.Fatal(err)
		}
		// [决策理由] 每个源码样例都必须落到指定强类型。
		if !test.check(event) {
			t.Errorf("事件映射错误: %T %+v", event, event)
		}
	}

	// >>> 数据演变示例
	// 1. heartbeat -> discriminator -> *HeartbeatEvent。
	// 2. group_upload -> discriminator -> *GroupUploadNotice。
}
