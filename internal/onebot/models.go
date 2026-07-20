// 📌 影响范围：定义 NapCat 4.18.13 OneBot 公共数据模型；无外部变量。
package onebot

// UserInfo 对应 OpenAPI 的 OB11User 完整字段集合。
type UserInfo struct {
	BirthdayYear  int64  `json:"birthday_year"`
	BirthdayMonth int64  `json:"birthday_month"`
	BirthdayDay   int64  `json:"birthday_day"`
	PhoneNumber   string `json:"phone_num"`
	Email         string `json:"email"`
	CategoryID    int64  `json:"category_id"`
	UserID        int64  `json:"user_id"`
	Nickname      string `json:"nickname"`
	Remark        string `json:"remark"`
	Sex           string `json:"sex"`
	Level         int64  `json:"level"`
	Age           int64  `json:"age"`
	QID           string `json:"qid"`
	LoginDays     int64  `json:"login_days"`
	CategoryName  string `json:"categoryName"`
	CategoryIDV2  int64  `json:"categoryId"`
}
