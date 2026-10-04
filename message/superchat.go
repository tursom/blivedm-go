package message

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// SuperChat 超级弹幕消息结构体
// message_jpn: 消息日文翻译（目前只出现在SUPER_CHAT_MESSAGE_JPN）
// id_: str，消息ID，删除时用
type SuperChat struct {
	Sender                *User     `json:"-"`
	Medal                 *Medal    `json:"-"`
	SenderUinfo           *UserInfo `json:"sender_uinfo,omitempty"`
	BackgroundBottomColor string    `json:"background_bottom_color"` //底部背景色
	BackgroundColor       string    `json:"background_color"`        //背景色
	BackgroundColorEnd    string    `json:"background_color_end"`
	BackgroundColorStart  string    `json:"background_color_start"`
	BackgroundIcon        string    `json:"background_icon"`        //背景图标
	BackgroundImage       string    `json:"background_image"`       //背景图
	BackgroundPriceColor  string    `json:"background_price_color"` //背景价格颜色
	ColorPoint            float64   `json:"color_point"`
	Dmscore               int       `json:"dmscore"`
	EndTime               int       `json:"end_time"` //结束时间戳
	Gift                  struct {
		GiftId   int    `json:"gift_id"`   //礼物ID
		GiftName string `json:"gift_name"` //礼物名
		Num      int    `json:"num"`
	} `json:"gift"`
	Id          int `json:"id"`
	IsRanked    int `json:"is_ranked"`
	IsSendAudit int `json:"is_send_audit"`
	MedalInfo   struct {
		AnchorRoomid     int    `json:"anchor_roomid"`
		AnchorUname      string `json:"anchor_uname"`
		GuardLevel       int    `json:"guard_level"` // 舰队等级，0:非舰队，1:总督，2:提督，3:舰长
		IconId           int    `json:"icon_id"`
		IsLighted        int    `json:"is_lighted"`
		MedalColor       string `json:"medal_color"`
		MedalColorBorder int    `json:"medal_color_border"`
		MedalColorEnd    int    `json:"medal_color_end"`
		MedalColorStart  int    `json:"medal_color_start"`
		MedalLevel       int    `json:"medal_level"`
		MedalName        string `json:"medal_name"`
		Special          string `json:"special"`
		TargetId         int    `json:"target_id"`
	} `json:"medal_info"`
	Message          string `json:"message"` // 消息
	MessageFontColor string `json:"message_font_color"`
	MessageTrans     string `json:"message_trans"`
	Price            int    `json:"price"` // 价格（人民币）
	Rate             int    `json:"rate"`
	StartTime        int    `json:"start_time"` // 开始时间戳
	Time             int    `json:"time"`       //剩余时间
	Token            string `json:"token"`
	TransMark        int    `json:"trans_mark"`
	Ts               int    `json:"ts"`
	Uid              int    `json:"uid"` //用户ID
	UserInfo         struct {
		Face       string `json:"face"` //用户头像URL
		FaceFrame  string `json:"face_frame"`
		GuardLevel int    `json:"guard_level"`
		IsMainVip  int    `json:"is_main_vip"`
		IsSvip     int    `json:"is_svip"`
		IsVip      int    `json:"is_vip"`
		LevelColor string `json:"level_color"`
		Manager    int    `json:"manager"`
		NameColor  string `json:"name_color"`
		Title      string `json:"title"`
		Uname      string `json:"uname"`      //用户名
		UserLevel  int    `json:"user_level"` //用户等级
	} `json:"user_info"`
}

func (s *SuperChat) Parse(data []byte) {
	if err := s.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse superchat failed")
	}
}

func (s *SuperChat) ParseJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeData(data, &fields); err != nil {
		return err
	}
	parsed := gjson.GetBytes(data, "data")
	if err := requireUnsignedFields(parsed, "id", "uid", "price"); err != nil {
		return err
	}
	if err := requireSignedField(parsed, "start_time"); err != nil {
		return err
	}
	// 可选展示字段和期限按 Rust 参考实现独立解析，不让其格式变化丢失订单。
	for _, field := range []string{"message", "background_color", "message_font_color", "time", "end_time", "user_info", "medal_info"} {
		delete(fields, field)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	var next SuperChat
	if err := json.Unmarshal(encoded, &next); err != nil {
		return err
	}
	if uint64(next.Price) > math.MaxUint32 {
		return fmt.Errorf("superchat price exceeds uint32 range")
	}
	next.Message = optionalString(parsed.Get("message"))
	next.BackgroundColor = "#EDF5FF"
	if color := parsed.Get("background_color"); color.Type == gjson.String {
		next.BackgroundColor = color.String()
	}
	next.MessageFontColor = "#323232"
	if color := parsed.Get("message_font_color"); color.Type == gjson.String {
		next.MessageFontColor = color.String()
	}
	duration, hasDuration := optionalUint32(parsed.Get("time"))
	end, hasEnd := optionalSignedInt(parsed.Get("end_time"))
	if !hasDuration && hasEnd && end >= next.StartTime {
		difference := uint64(end) - uint64(next.StartTime)
		if difference <= math.MaxUint32 {
			duration = uint32(difference)
		}
	}
	if uint64(duration) > uint64(math.MaxInt) {
		return fmt.Errorf("superchat duration exceeds int range")
	}
	next.Time = int(duration)
	next.EndTime = end
	if !hasEnd {
		if next.StartTime > math.MaxInt-next.Time {
			next.EndTime = math.MaxInt
		} else {
			next.EndTime = next.StartTime + next.Time
		}
	}
	user := parsed.Get("user_info")
	if user.IsObject() {
		if err := json.Unmarshal([]byte(user.Raw), &next.UserInfo); err != nil {
			next.UserInfo = (SuperChat{}).UserInfo
		}
		next.UserInfo.Uname = optionalString(user.Get("uname"))
		next.UserInfo.Face = optionalString(user.Get("face"))
		level, _ := optionalUint32(user.Get("user_level"))
		if uint64(level) <= uint64(math.MaxInt) {
			next.UserInfo.UserLevel = int(level)
		}
		guard, _ := optionalSignedInt(user.Get("guard_level"))
		if guard >= 1 && guard <= 3 {
			next.UserInfo.GuardLevel = guard
		} else {
			next.UserInfo.GuardLevel = 0
		}
	}
	if sender := next.SenderUinfo; sender != nil {
		next.UserInfo.Uname = firstNonEmpty(sender.Base.Name, next.UserInfo.Uname)
		next.UserInfo.Face = firstNonEmpty(sender.Base.Face, next.UserInfo.Face)
	}
	medal := parsed.Get("medal_info")
	if medal.IsObject() {
		var medalFields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(medal.Raw), &medalFields); err != nil {
			return err
		}
		delete(medalFields, "medal_color")
		delete(medalFields, "is_lighted")
		medalJSON, err := json.Marshal(medalFields)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(medalJSON, &next.MedalInfo); err != nil {
			next.MedalInfo = (SuperChat{}).MedalInfo
		}
		next.MedalInfo.IsLighted = boolInt(jsonFlag(medal.Get("is_lighted"), true))
		var color uint64
		if rawColor := medal.Get("medal_color"); rawColor.Type == gjson.String {
			next.MedalInfo.MedalColor = rawColor.String()
			parsedColor, err := strconv.ParseUint(strings.TrimPrefix(rawColor.String(), "#"), 16, 32)
			if err == nil {
				color = parsedColor
			}
		} else if rawColor.Type == gjson.Number {
			parsedColor, err := strconv.ParseUint(rawColor.Raw, 10, 32)
			if err == nil {
				color = parsedColor
			}
			next.MedalInfo.MedalColor = fmt.Sprintf("#%06x", color)
		}
		if next.MedalInfo.MedalLevel > 0 && uint64(next.MedalInfo.MedalLevel) <= math.MaxUint32 {
			next.Medal = &Medal{Name: next.MedalInfo.MedalName, Level: next.MedalInfo.MedalLevel,
				Color: int(color), UpRoomId: next.MedalInfo.AnchorRoomid, UpUid: next.MedalInfo.TargetId,
				UpName: next.MedalInfo.AnchorUname, IsLight: next.MedalInfo.IsLighted != 0}
		}
	}
	next.Sender = &User{Uid: next.Uid, Uname: next.UserInfo.Uname, Face: next.UserInfo.Face,
		Medal: next.Medal, GuardLevel: next.UserInfo.GuardLevel, UserLevel: int64(next.UserInfo.UserLevel)}
	if next.SenderUinfo != nil {
		next.Sender.WealthLevel = next.SenderUinfo.Wealth.Level
	}
	*s = next
	return nil
}

// ValueCNYFen converts the API price in yuan to CNY cents.
func (s SuperChat) ValueCNYFen() uint64 {
	if s.Price <= 0 {
		return 0
	}
	price := uint64(s.Price)
	if price > math.MaxUint64/100 {
		return math.MaxUint64
	}
	return price * 100
}

func optionalString(value gjson.Result) string {
	if value.Type != gjson.String {
		return ""
	}
	return value.String()
}

func optionalUint32(value gjson.Result) (uint32, bool) {
	if value.Type != gjson.Number {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value.Raw, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}

func optionalSignedInt(value gjson.Result) (int, bool) {
	if value.Type != gjson.Number {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value.Raw, 10, strconv.IntSize)
	return int(parsed), err == nil
}
