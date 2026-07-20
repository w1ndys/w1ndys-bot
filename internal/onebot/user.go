// 📌 影响范围：通过注入的 ActionCaller 调用 NapCat 用户接口；不直接管理 WebSocket 或 echo。
package onebot

import (
	"context"
	"fmt"
)

// FriendInfo 是好友列表中的完整 OB11User 资料。
type FriendInfo = UserInfo

// GetFriendListParams 是获取好友列表的参数。
type GetFriendListParams struct {
	NoCache bool `json:"no_cache"`
}

// GetFriendList 获取当前账号好友列表。
// @param ctx：请求上下文；noCache：是否跳过 NapCat 缓存。
// @returns 好友列表，或 Action、业务与解析错误。
// ⚠️副作用说明：可能在 noCache 为 true 时触发 NapCat 刷新好友数据。
func (a *API) GetFriendList(ctx context.Context, noCache bool) ([]FriendInfo, error) {
	var result []FriendInfo
	err := a.Call(ctx, ActionGetFriendList, GetFriendListParams{NoCache: noCache}, &result)

	// >>> 数据演变示例
	// 1. noCache=false -> get_friend_list -> [{UserID:1}]。
	// 2. noCache=true -> 刷新数据 -> 返回最新好友列表。
	return result, err
}

// SetFriendRemarkParams 是设置好友备注的参数。
type SetFriendRemarkParams struct {
	UserID string `json:"user_id"`
	Remark string `json:"remark"`
}

// SetFriendRemark 修改好友备注。
// @param ctx：请求上下文；userID：好友 QQ 号；remark：新备注。
// @returns 参数、Action、业务与解析错误。
// ⚠️副作用说明：修改 QQ 好友资料中的备注。
func (a *API) SetFriendRemark(ctx context.Context, userID, remark string) error {
	// [决策理由] 空 QQ 号无法定位待修改的好友。
	if userID == "" {
		return fmt.Errorf("好友 QQ 号不能为空")
	}
	err := a.Call(ctx, ActionSetFriendRemark, SetFriendRemarkParams{UserID: userID, Remark: remark}, nil)

	// >>> 数据演变示例
	// 1. userID="123",remark="同事" -> set_friend_remark -> 备注更新。
	// 2. userID="" -> 参数校验失败 -> 备注不变。
	return err
}

// SendLikeParams 是资料点赞参数。
type SendLikeParams struct {
	UserID string `json:"user_id"`
	Times  int    `json:"times"`
}

// SendLike 为指定用户资料点赞。
// @param ctx：请求上下文；userID：目标 QQ 号；times：点赞次数。
// @returns 参数、Action、业务与解析错误。
// ⚠️副作用说明：通过 QQ 向指定用户发送资料点赞。
func (a *API) SendLike(ctx context.Context, userID string, times int) error {
	// [决策理由] 目标 QQ 和正数次数是执行点赞的必要条件。
	if userID == "" || times <= 0 {
		return fmt.Errorf("用户 QQ 不能为空且点赞次数必须大于 0")
	}
	err := a.Call(ctx, ActionSendLike, SendLikeParams{UserID: userID, Times: times}, nil)

	// >>> 数据演变示例
	// 1. userID="123",times=3 -> send_like -> 点赞 3 次。
	// 2. userID="123",times=0 -> 参数校验失败 -> 不点赞。
	return err
}
