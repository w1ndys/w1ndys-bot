// 📌 影响范围：声明 NapCat 4.18.13 群组接口与群组扩展 Action；不发起网络请求或修改群数据。
package onebot

const (
	ActionDeleteGroupAlbumMedia     Action = "del_group_album_media"
	ActionSetGroupAlbumMediaLike    Action = "set_group_album_media_like"
	ActionCancelGroupAlbumMediaLike Action = "cancel_group_album_media_like"
	ActionDoGroupAlbumComment       Action = "do_group_album_comment"
	ActionGetGroupAlbumMediaList    Action = "get_group_album_media_list"
	ActionGetQunAlbumList           Action = "get_qun_album_list"
	ActionUploadImageToQunAlbum     Action = "upload_image_to_qun_album"
	ActionGetGroupDetailInfo        Action = "get_group_detail_info"
	ActionSetGroupAddOption         Action = "set_group_add_option"
	ActionSetGroupRobotAddOption    Action = "set_group_robot_add_option"
	ActionSetGroupSearch            Action = "set_group_search"
	ActionSetGroupRemark            Action = "set_group_remark"
	ActionGetGroupInfoEx            Action = "get_group_info_ex"
	ActionSetGroupSign              Action = "set_group_sign"
	ActionSendGroupSign             Action = "send_group_sign"
	ActionGetGroupSignedList        Action = "get_group_signed_list"
	ActionGetGroupList              Action = "get_group_list"
	ActionGetGroupInfo              Action = "get_group_info"
	ActionGetGroupMemberList        Action = "get_group_member_list"
	ActionGetGroupMemberInfo        Action = "get_group_member_info"
	ActionSendGroupMessage          Action = "send_group_msg"
	ActionSetGroupAddRequest        Action = "set_group_add_request"
	ActionSetGroupLeave             Action = "set_group_leave"
	ActionSetGroupWholeBan          Action = "set_group_whole_ban"
	ActionSetGroupBan               Action = "set_group_ban"
	ActionSetGroupKick              Action = "set_group_kick"
	ActionSetGroupAdmin             Action = "set_group_admin"
	ActionSetGroupName              Action = "set_group_name"
	ActionSetGroupCard              Action = "set_group_card"
	ActionGetGroupNotice            Action = "_get_group_notice"
	ActionGetEssenceMessageList     Action = "get_essence_msg_list"
	ActionGetGroupIgnoredNotifies   Action = "get_group_ignored_notifies"
	ActionDeleteEssenceMessage      Action = "delete_essence_msg"
	ActionSetEssenceMessage         Action = "set_essence_msg"
	ActionDeleteGroupNotice         Action = "_del_group_notice"
	ActionGetGroupShutList          Action = "get_group_shut_list"
	ActionGetGroupIgnoreAddRequest  Action = "get_group_ignore_add_request"
)

// GroupIDParams 表示仅需要群号的群组请求参数。
type GroupIDParams struct {
	GroupID string `json:"group_id"`
}

// GroupMemberParams 表示查询单个群成员的请求参数。
type GroupMemberParams struct {
	GroupID string `json:"group_id"`
	UserID  string `json:"user_id"`
	NoCache bool   `json:"no_cache,omitempty"`
}

// SetGroupBanParams 表示设置群成员禁言的请求参数。
type SetGroupBanParams struct {
	GroupID  string `json:"group_id"`
	UserID   string `json:"user_id"`
	Duration any    `json:"duration"`
}

// SetGroupKickParams 表示将成员移出群组的请求参数。
type SetGroupKickParams struct {
	GroupID          string `json:"group_id"`
	UserID           string `json:"user_id"`
	RejectAddRequest bool   `json:"reject_add_request,omitempty"`
}

// SetGroupAdminParams 表示设置或取消群管理员的请求参数。
type SetGroupAdminParams struct {
	GroupID string `json:"group_id"`
	UserID  string `json:"user_id"`
	Enable  bool   `json:"enable"`
}

// SetGroupNameParams 表示修改群名称的请求参数。
type SetGroupNameParams struct {
	GroupID   string `json:"group_id"`
	GroupName string `json:"group_name"`
}

// SetGroupCardParams 表示修改群成员名片的请求参数。
type SetGroupCardParams struct {
	GroupID string `json:"group_id"`
	UserID  string `json:"user_id"`
	Card    string `json:"card"`
}

// SetGroupAddRequestParams 表示处理加群请求的参数。
type SetGroupAddRequestParams struct {
	Flag    string  `json:"flag"`
	Approve *bool   `json:"approve,omitempty"`
	Reason  string  `json:"reason,omitempty"`
	Count   float64 `json:"count,omitempty"`
}

// SetGroupLeaveParams 表示退出或解散群组的参数。
type SetGroupLeaveParams struct {
	GroupID   string `json:"group_id"`
	IsDismiss bool   `json:"is_dismiss,omitempty"`
}

// GroupInfo 表示 OneBot 群信息的常用字段；NapCat 增量字段由具体调用方按需扩展。
type GroupInfo struct {
	GroupID        int64  `json:"group_id"`
	GroupName      string `json:"group_name"`
	GroupRemark    string `json:"group_remark"`
	GroupAllShut   int64  `json:"group_all_shut"`
	MemberCount    int64  `json:"member_count"`
	MaxMemberCount int64  `json:"max_member_count"`
}

// GroupMemberInfo 表示 OneBot 群成员信息的常用字段。
type GroupMemberInfo struct {
	GroupID         int64  `json:"group_id"`
	UserID          int64  `json:"user_id"`
	Nickname        string `json:"nickname"`
	Card            string `json:"card"`
	Sex             string `json:"sex"`
	Age             int64  `json:"age"`
	Area            string `json:"area"`
	JoinTime        int64  `json:"join_time"`
	LastSentTime    int64  `json:"last_sent_time"`
	Level           string `json:"level"`
	Role            string `json:"role"`
	Unfriendly      bool   `json:"unfriendly"`
	Title           string `json:"title"`
	TitleExpireTime int64  `json:"title_expire_time"`
	CardChangeable  bool   `json:"card_changeable"`
	QQLevel         int64  `json:"qq_level"`
	ShutUpTimestamp int64  `json:"shut_up_timestamp"`
	IsRobot         bool   `json:"is_robot"`
	QAge            int64  `json:"qage"`
}
