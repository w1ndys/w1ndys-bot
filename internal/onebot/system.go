// 📌 影响范围：读取 NapCat 登录、版本与运行状态；不修改机器人或外部系统状态。
package onebot

import "context"

// LoginInfo 表示当前登录 QQ 的完整 OB11User 资料。
type LoginInfo = UserInfo

// VersionInfo 表示 NapCat 暴露的实现与协议版本。
type VersionInfo struct {
	AppName         string `json:"app_name"`
	ProtocolVersion string `json:"protocol_version"`
	AppVersion      string `json:"app_version"`
}

// Status 表示 NapCat 当前在线和健康状态。
type Status struct {
	Online bool           `json:"online"`
	Good   bool           `json:"good"`
	Stat   map[string]any `json:"stat"`
}

// Capability 表示 NapCat 是否支持某种消息能力。
type Capability struct {
	Yes bool `json:"yes"`
}

// GetLoginInfo 获取当前登录帐号。
// @param ctx：控制请求取消。
// @returns 登录帐号资料或调用错误。
// ⚠️副作用说明：通过 NapCat 读取当前登录状态。
func (a *API) GetLoginInfo(ctx context.Context) (LoginInfo, error) {
	var result LoginInfo
	err := a.Call(ctx, ActionGetLoginInfo, nil, &result)

	// >>> 数据演变示例
	// 1. data{user_id:1,nickname:"bot"} -> LoginInfo{UserID:1,Nickname:"bot"}。
	// 2. status=failed -> LoginInfo{} + ActionError。
	return result, err
}

// GetVersionInfo 获取 NapCat 与 OneBot 协议版本。
// @param ctx：控制请求取消。
// @returns 版本信息或调用错误。
// ⚠️副作用说明：通过 NapCat 读取版本信息。
func (a *API) GetVersionInfo(ctx context.Context) (VersionInfo, error) {
	var result VersionInfo
	err := a.Call(ctx, ActionGetVersionInfo, nil, &result)

	// >>> 数据演变示例
	// 1. app_version=4.18.13 -> VersionInfo.AppVersion=4.18.13。
	// 2. data 结构错误 -> VersionInfo{} + JSON 解码错误。
	return result, err
}

// GetStatus 获取 NapCat 运行状态。
// @param ctx：控制请求取消。
// @returns 在线、健康和统计信息或调用错误。
// ⚠️副作用说明：通过 NapCat 读取运行状态。
func (a *API) GetStatus(ctx context.Context) (Status, error) {
	var result Status
	err := a.Call(ctx, ActionGetStatus, nil, &result)

	// >>> 数据演变示例
	// 1. online=true,good=true -> Status{Online:true,Good:true}。
	// 2. 连接断开 -> Status{} + 传输错误。
	return result, err
}

// CanSendImage 查询当前帐号能否发送图片。
// @param ctx：控制请求取消。
// @returns 图片能力标记或调用错误。
// ⚠️副作用说明：通过 NapCat 读取帐号能力。
func (a *API) CanSendImage(ctx context.Context) (bool, error) {
	var result Capability
	err := a.Call(ctx, ActionCanSendImage, nil, &result)

	// >>> 数据演变示例
	// 1. data{yes:true} -> true,nil。
	// 2. status=failed -> false,ActionError。
	return result.Yes, err
}

// CanSendRecord 查询当前帐号能否发送语音。
// @param ctx：控制请求取消。
// @returns 语音能力标记或调用错误。
// ⚠️副作用说明：通过 NapCat 读取帐号能力。
func (a *API) CanSendRecord(ctx context.Context) (bool, error) {
	var result Capability
	err := a.Call(ctx, ActionCanSendRecord, nil, &result)

	// >>> 数据演变示例
	// 1. data{yes:true} -> true,nil。
	// 2. 响应 data 为空 -> false,解析错误。
	return result.Yes, err
}
