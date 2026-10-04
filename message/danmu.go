package message

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tursom/blivedm-go/pb"
)

const (
	TextDanmaku = iota
	EmoticonDanmaku
)

type (
	Danmaku struct {
		Sender    *User
		Content   string
		Extra     *Extra
		Emoticon  *Emoticon
		Type      int
		Timestamp int64
		Color     uint32
		Mode      uint32
		Raw       string
	}

	Extra struct {
		SendFromMe            bool   `json:"send_from_me"`
		MasterPlayerHidden    bool   `json:"master_player_hidden"`
		Mode                  int    `json:"mode"`
		Color                 int    `json:"color"`
		DmType                int    `json:"dm_type"`
		FontSize              int    `json:"font_size"`
		PlayerMode            int    `json:"player_mode"`
		ShowPlayerType        int    `json:"show_player_type"`
		Content               string `json:"content"`
		UserHash              string `json:"user_hash"`
		EmoticonUnique        string `json:"emoticon_unique"`
		BulgeDisplay          int    `json:"bulge_display"`
		RecommendScore        int    `json:"recommend_score"`
		MainStateDmColor      string `json:"main_state_dm_color"`
		ObjectiveStateDmColor string `json:"objective_state_dm_color"`
		Direction             int    `json:"direction"`
		PkDirection           int    `json:"pk_direction"`
		QuartetDirection      int    `json:"quartet_direction"`
		AnniversaryCrowd      int    `json:"anniversary_crowd"`
		YeahSpaceType         string `json:"yeah_space_type"`
		YeahSpaceURL          string `json:"yeah_space_url"`
		JumpToURL             string `json:"jump_to_url"`
		SpaceType             string `json:"space_type"`
		SpaceURL              string `json:"space_url"`
		// Animation             any    `json:"animation"`
		// Emots                 any    `json:"emots"`
		IsAudited bool   `json:"is_audited"`
		IDStr     string `json:"id_str"`
		// Icon                  any    `json:"icon"`
		ShowReply       bool   `json:"show_reply"`
		ReplyMid        int    `json:"reply_mid"`
		ReplyUname      string `json:"reply_uname"`
		ReplyUnameColor string `json:"reply_uname_color"`
		ReplyIsMystery  bool   `json:"reply_is_mystery"`
		ReplyTypeEnum   int    `json:"reply_type_enum"`
		HitCombo        int    `json:"hit_combo"`
		EsportsJumpURL  string `json:"esports_jump_url"`
	}
	Emoticon struct {
		BulgeDisplay   int    `json:"bulge_display"`
		EmoticonUnique string `json:"emoticon_unique"`
		Height         int    `json:"height"`
		InPlayerArea   int    `json:"in_player_area"`
		IsDynamic      int    `json:"is_dynamic"`
		Url            string `json:"url"`
		Width          int    `json:"width"`
	}
	CommonNoticeDanmaku struct {
		ContentSegments []struct {
			FontColor string `json:"font_color"`
			Text      string `json:"text"`
			Type      int    `json:"type"`
		} `json:"content_segments"`
		Dmscore   int   `json:"dmscore"`
		Terminals []int `json:"terminals"`
	}
)

// Parse is the compatibility wrapper for ParseJSON.
func (d *Danmaku) Parse(data []byte) {
	if err := d.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse danmaku failed")
	}
}

// ParseJSON decodes legacy DANMU_MSG and its optional dm_v2 payload.
// It owns Raw and all decoded strings; callers may safely reuse data.
func (d *Danmaku) ParseJSON(data []byte) error {
	if !gjson.ValidBytes(data) {
		return fmt.Errorf("invalid danmaku JSON")
	}
	parsed := gjson.ParseBytes(data)
	info := parsed.Get("info")
	next := Danmaku{Raw: string(data), Extra: &Extra{}, Emoticon: &Emoticon{}, Color: 0xffffff, Mode: 1}
	if info.Exists() {
		if !info.IsArray() || !info.Get("0").IsArray() || info.Get("1").Type != gjson.String || !info.Get("2").IsArray() {
			return fmt.Errorf("invalid danmaku info array")
		}
		meta, user, medal := info.Get("0"), info.Get("2"), info.Get("3")
		uid := user.Get("0")
		if !uid.Exists() || uid.Type != gjson.Number || uid.Int() < 0 || user.Get("1").Type != gjson.String {
			return fmt.Errorf("invalid danmaku sender")
		}
		if err := requireUnsignedFields(user, "0"); err != nil {
			return err
		}
		senderID, err := checkedInt(uid.Uint())
		if err != nil {
			return err
		}
		next.Content = info.Get("1").String()
		if meta.Get("4").Type != gjson.Number {
			return fmt.Errorf("missing danmaku timestamp")
		}
		next.Timestamp = meta.Get("4").Int()
		next.Type = int(meta.Get("12").Int())
		if meta.Get("1").Exists() {
			next.Mode = uint32(meta.Get("1").Uint())
		}
		if meta.Get("3").Exists() {
			next.Color = uint32(meta.Get("3").Uint())
		}
		nested := meta.Get("15.user")
		next.Sender = &User{
			Uid: senderID, Uname: user.Get("1").String(), Admin: jsonFlag(user.Get("2"), false),
			Urank: int(user.Get("5").Int()), MobileVerify: jsonFlag(user.Get("6"), false),
			GuardLevel: int(info.Get("7").Int()), UserLevel: info.Get("4.0").Int(),
			Face: nested.Get("base.face").String(),
		}
		next.Sender.Medal = &Medal{}
		wealth := info.Get("16.0")
		if !wealth.Exists() || wealth.Type == gjson.Null {
			wealth = nested.Get("wealth.level")
		}
		if wealth.Uint() <= math.MaxUint32 {
			next.Sender.WealthLevel = uint32(wealth.Uint())
		}
		if medal.IsArray() && medal.Get("0").Int() > 0 {
			anchor := nested.Get("medal.ruid").Uint()
			if anchor == 0 {
				anchor = medal.Get("12").Uint()
			}
			anchorID, err := checkedInt(anchor)
			if err != nil {
				return err
			}
			next.Sender.Medal = &Medal{
				Level: int(medal.Get("0").Int()), Name: medal.Get("1").String(), UpName: medal.Get("2").String(),
				UpRoomId: int(medal.Get("3").Int()), Color: int(medal.Get("4").Int()), UpUid: anchorID,
				IsLight: jsonFlag(nested.Get("medal.is_light"), jsonFlag(medal.Get("11"), true)),
			}
		}
		extra := meta.Get("15.extra")
		if extra.Exists() && extra.Type != gjson.Null && extra.String() != "" {
			raw := extra.Raw
			if extra.Type == gjson.String {
				raw = extra.String()
			}
			if err := json.Unmarshal([]byte(raw), next.Extra); err != nil {
				return fmt.Errorf("decode danmaku extra: %w", err)
			}
		}
		emo := meta.Get("13")
		if emo.IsObject() {
			if err := json.Unmarshal([]byte(emo.Raw), next.Emoticon); err != nil {
				return fmt.Errorf("decode emoticon: %w", err)
			}
		}
	} else if !parsed.Get("dm_v2").Exists() {
		return fmt.Errorf("danmaku has neither info nor dm_v2")
	}
	encoded := parsed.Get("dm_v2")
	if encoded.Exists() && encoded.Type != gjson.Null && encoded.String() != "" {
		if encoded.Type != gjson.String {
			return fmt.Errorf("dm_v2 must be a base64 string")
		}
		var dm pb.EventDm
		if err := decodeBase64Proto(encoded.String(), &dm); err != nil {
			return err
		}
		if err := next.applyDMV2(&dm, info.Exists()); err != nil {
			return err
		}
	}
	if next.Sender == nil {
		return fmt.Errorf("danmaku has no sender")
	}
	*d = next
	return nil
}

func jsonFlag(value gjson.Result, fallback bool) bool {
	switch value.Type {
	case gjson.True:
		return true
	case gjson.False:
		return false
	case gjson.Number:
		return value.Int() != 0
	default:
		return fallback
	}
}

func (d *Danmaku) applyDMV2(dm *pb.EventDm, legacy bool) error {
	d.Content = dm.Content
	if !legacy {
		d.Timestamp = dm.Ctime
		if d.Timestamp > 0 && d.Timestamp < 1_000_000_000_000 {
			d.Timestamp *= 1000
		}
	}
	if !legacy || dm.Mode != 0 {
		d.Mode = uint32(dm.Mode)
	}
	if !legacy || dm.Mode != 0 || dm.Color != 0 {
		d.Color = dm.Color
	}
	if !legacy || dm.Mode != 0 || dm.DmType != 0 {
		d.Type = int(dm.DmType)
	}
	if d.Sender == nil {
		d.Sender = &User{Medal: &Medal{}}
	}
	if user := dm.User; user != nil {
		if user.Uid != 0 {
			uid, err := checkedInt(user.Uid)
			if err != nil {
				return err
			}
			d.Sender.Uid = uid
		}
		d.Sender.Uname = firstNonEmpty(user.Name, d.Sender.Uname)
		d.Sender.Face = firstNonEmpty(user.Face, d.Sender.Face)
		if !legacy || user.Rank != 0 {
			d.Sender.Urank = int(user.Rank)
		}
		if !legacy || user.MobileVerify != 0 {
			d.Sender.MobileVerify = user.MobileVerify != 0
		}
		if user.Level != nil {
			d.Sender.UserLevel = int64(user.Level.Level)
		}
		if user.Wealth != nil {
			d.Sender.WealthLevel = user.Wealth.Level
		}
		if medal := user.Medal; medal != nil && medal.Level > 0 {
			if d.Sender.Medal == nil {
				d.Sender.Medal = &Medal{}
			}
			d.Sender.Medal.Level = int(medal.Level)
			d.Sender.Medal.Name = medal.Name
			d.Sender.Medal.Color = int(medal.Color)
			d.Sender.Medal.IsLight = medal.Light != 0
			d.Sender.GuardLevel = int(medal.Privilege)
		}
	}
	if room := dm.Room; room != nil && d.Sender.Medal != nil {
		uid, err := checkedInt(room.Uid)
		if err != nil {
			return err
		}
		d.Sender.Medal.UpUid = uid
		d.Sender.Medal.UpName = room.Name
	}
	d.Extra.IDStr = firstNonEmpty(dm.IdStr, d.Extra.IDStr)
	d.Extra.SendFromMe = dm.SendFromMe
	d.Extra.Content = dm.Content
	d.Extra.UserHash = firstNonEmpty(dm.MidHash, d.Extra.UserHash)
	if dm.Fontsize != 0 {
		d.Extra.FontSize = int(dm.Fontsize)
	}
	if len(dm.Emoticons) != 0 && d.Type == EmoticonDanmaku {
		key := d.Extra.EmoticonUnique
		emo := dm.Emoticons[key]
		if emo == nil {
			keys := make([]string, 0, len(dm.Emoticons))
			for k := range dm.Emoticons {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			emo = dm.Emoticons[keys[0]]
		}
		if emo != nil {
			d.Emoticon = &Emoticon{EmoticonUnique: emo.Unique, Url: emo.Url, Width: int(emo.Width),
				Height: int(emo.Height), IsDynamic: boolInt(emo.IsDynamic),
				InPlayerArea: int(emo.InPlayerArea), BulgeDisplay: int(emo.BulgeDisplay)}
		}
	}
	return nil
}
