package message

import (
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// WidgetBanner
// TODO: widget_list的code不定
type WidgetBanner struct {
	Timestamp  int `json:"timestamp"`
	WidgetList struct {
		Field1 struct {
			Id             int      `json:"id"`
			Title          string   `json:"title"`
			Cover          string   `json:"cover"`
			WebCover       string   `json:"web_cover"`
			TipText        string   `json:"tip_text"`
			TipTextColor   string   `json:"tip_text_color"`
			TipBottomColor string   `json:"tip_bottom_color"`
			JumpUrl        string   `json:"jump_url"`
			Url            string   `json:"url"`
			StayTime       int      `json:"stay_time"`
			Site           int      `json:"site"`
			PlatformIn     []string `json:"platform_in"`
			Type           int      `json:"type"`
			BandId         int      `json:"band_id"`
			SubKey         string   `json:"sub_key"`
			SubData        string   `json:"sub_data"`
			IsAdd          bool     `json:"is_add"`
		} `json:"58"`
	} `json:"widget_list"`
}

type HotRankChanged struct {
	Rank        int    `json:"rank"`
	Trend       int    `json:"trend"`
	Countdown   int    `json:"countdown"`
	Timestamp   int    `json:"timestamp"`
	WebUrl      string `json:"web_url"`
	LiveUrl     string `json:"live_url"`
	BlinkUrl    string `json:"blink_url"`
	LiveLinkUrl string `json:"live_link_url"`
	PcLinkUrl   string `json:"pc_link_url"`
	Icon        string `json:"icon"`
	AreaName    string `json:"area_name"`
	RankDesc    string `json:"rank_desc"`
}

type HotRankChangedV2 HotRankChanged

type HotRankSettlement struct {
	AreaName  string `json:"area_name"`
	CacheKey  string `json:"cache_key"`
	DmMsg     string `json:"dm_msg"`
	Dmscore   int    `json:"dmscore"`
	Face      string `json:"face"`
	Icon      string `json:"icon"`
	Rank      int    `json:"rank"`
	Timestamp int    `json:"timestamp"`
	Uname     string `json:"uname"`
	Url       string `json:"url"`
}

type HotRankSettlementV2 struct {
	Rank      int    `json:"rank"`
	Uname     string `json:"uname"`
	Face      string `json:"face"`
	Timestamp int    `json:"timestamp"`
	Icon      string `json:"icon"`
	AreaName  string `json:"area_name"`
	Url       string `json:"url"`
	CacheKey  string `json:"cache_key"`
	DmMsg     string `json:"dm_msg"`
}

type InteractWord struct {
	UserDetails  *UserInfo `json:"-"`
	User         *User     `json:"-"`
	Contribution struct {
		Grade int `json:"grade"`
	} `json:"contribution"`
	Dmscore   int `json:"dmscore"`
	FansMedal struct {
		AnchorRoomid     int    `json:"anchor_roomid"`
		GuardLevel       int    `json:"guard_level"`
		IconId           int    `json:"icon_id"`
		IsLighted        int    `json:"is_lighted"`
		MedalColor       int    `json:"medal_color"`
		MedalColorBorder int    `json:"medal_color_border"`
		MedalColorEnd    int    `json:"medal_color_end"`
		MedalColorStart  int    `json:"medal_color_start"`
		MedalLevel       int    `json:"medal_level"`
		MedalName        string `json:"medal_name"`
		Score            int    `json:"score"`
		Special          string `json:"special"`
		TargetId         int    `json:"target_id"`
	} `json:"fans_medal"`
	Identities  []int          `json:"identities"`
	IsSpread    int            `json:"is_spread"`
	MsgType     int            `json:"msg_type"`
	Roomid      int            `json:"roomid"`
	Score       int64          `json:"score"`
	SpreadDesc  string         `json:"spread_desc"`
	SpreadInfo  string         `json:"spread_info"`
	TailIcon    int            `json:"tail_icon"`
	Timestamp   int            `json:"timestamp"`
	TriggerTime int64          `json:"trigger_time"`
	Uid         int            `json:"uid"`
	Uinfo       LegacyUserInfo `json:"uinfo"`
	Uname       string         `json:"uname"`
	UnameColor  string         `json:"uname_color"`
}

type OnlineRankCount struct {
	Count           int    `json:"count"`
	CountText       string `json:"count_text"`
	OnlineCount     int    `json:"online_count"`
	OnlineCountText string `json:"online_count_text"`
}

type OnlineRankV2 struct {
	RankType   string           `json:"rank_type"`
	OnlineList []OnlineRankUser `json:"online_list"`
}

type OnlineRankUser struct {
	UserDetails *UserInfo      `json:"-"`
	Uid         int            `json:"uid"`
	Uname       string         `json:"uname"`
	Face        string         `json:"face"`
	Rank        int            `json:"rank"`
	Score       string         `json:"score"`
	GuardLevel  int            `json:"guard_level"`
	Uinfo       LegacyUserInfo `json:"uinfo"`
}

func (u OnlineRankUser) Name() string {
	if u.Uinfo.Base.Name != "" {
		return u.Uinfo.Base.Name
	}
	return u.Uname
}

func (u OnlineRankUser) Avatar() string {
	if u.Uinfo.Base.Face != "" {
		return u.Uinfo.Base.Face
	}
	return u.Face
}

func (u OnlineRankUser) Guard() int {
	if u.Uinfo.Guard.Level != 0 {
		return u.Uinfo.Guard.Level
	}
	return u.GuardLevel
}

func (i *InteractWord) Parse(data []byte) {
	if err := i.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse interact word failed")
	}
}

func (i *InteractWord) ParseJSON(data []byte) error {
	if gjson.GetBytes(data, "data.pb").Exists() {
		return i.parseV2(data)
	}
	data = normalizeIntFlags(data, "data.fans_medal.is_lighted")
	var next InteractWord
	if err := decodeData(data, &next); err != nil {
		return err
	}
	if err := requireJSONFields(data, "data.uid", "data.uname", "data.timestamp"); err != nil {
		return err
	}
	if next.Uid < 0 {
		return fmt.Errorf("invalid interact user ID")
	}
	if next.MsgType == 0 {
		next.MsgType = 1
	}
	if raw := gjson.GetBytes(data, "data.uinfo"); raw.IsObject() {
		if err := json.Unmarshal([]byte(raw.Raw), &next.UserDetails); err != nil {
			return err
		}
	}
	next.populateUser()
	if next.User.Medal != nil {
		next.User.Medal.IsLight = jsonFlag(gjson.GetBytes(data, "data.fans_medal.is_lighted"), true)
	}
	*i = next
	return nil
}

func (i *InteractWord) populateUser() {
	i.User = &User{Uid: i.Uid, Uname: i.Uname, Face: i.Uinfo.Base.Face,
		GuardLevel: i.Uinfo.Guard.Level}
	if i.UserDetails != nil {
		i.User.WealthLevel = i.UserDetails.Wealth.Level
	}
	if m := i.FansMedal; m.MedalLevel > 0 {
		i.User.Medal = &Medal{Name: m.MedalName, Level: m.MedalLevel, Color: m.MedalColor,
			UpRoomId: m.AnchorRoomid, UpUid: m.TargetId, IsLight: m.IsLighted != 0}
	}
}

func (o *OnlineRankCount) Parse(data []byte) {
	if err := o.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse online rank count failed")
	}
}
func (o *OnlineRankCount) ParseJSON(data []byte) error {
	var next OnlineRankCount
	if err := decodeData(data, &next); err != nil {
		return err
	}
	if err := requireJSONFields(data, "data.count", "data.count_text", "data.online_count", "data.online_count_text"); err != nil {
		return err
	}
	if next.Count < 0 || next.OnlineCount < 0 {
		return fmt.Errorf("negative online count")
	}
	*o = next
	return nil
}

func (o *OnlineRankV2) Parse(data []byte) {
	if err := o.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse online rank v2 failed")
	}
}
func (o *OnlineRankV2) ParseJSON(data []byte) error {
	var next OnlineRankV2
	if err := decodeData(data, &next); err != nil {
		return err
	}
	if next.OnlineList == nil {
		return fmt.Errorf("online rank has no online_list")
	}
	for index, value := range gjson.GetBytes(data, "data.online_list").Array() {
		if raw := value.Get("uinfo"); raw.IsObject() {
			if err := json.Unmarshal([]byte(raw.Raw), &next.OnlineList[index].UserDetails); err != nil {
				return err
			}
		}
	}
	if next.RankType == "" {
		next.RankType = "online_rank"
	}
	*o = next
	return nil
}

type LiveInteractiveGame struct {
	Type           int         `json:"type"`
	Uid            int         `json:"uid"`
	Uname          string      `json:"uname"`
	Uface          string      `json:"uface"`
	GiftId         int         `json:"gift_id"`
	GiftName       string      `json:"gift_name"`
	GiftNum        int         `json:"gift_num"`
	Price          int         `json:"price"`
	Paid           bool        `json:"paid"`
	Msg            string      `json:"msg"`
	FansMedalLevel int         `json:"fans_medal_level"`
	GuardLevel     int         `json:"guard_level"`
	Timestamp      int         `json:"timestamp"`
	AnchorLottery  interface{} `json:"anchor_lottery"`
	PkInfo         interface{} `json:"pk_info"`
	AnchorInfo     struct {
		Uid   int    `json:"uid"`
		Uname string `json:"uname"`
		Uface string `json:"uface"`
	} `json:"anchor_info"`
}
