package message

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// 礼物消息结构体
type Gift struct {
	GiftInfo           *GiftInfo   `json:"gift_info,omitempty"`
	GiftIcon           string      `json:"-"`
	EffectId           *uint64     `json:"-"`
	SenderUinfo        *UserInfo   `json:"sender_uinfo,omitempty"`
	ShowBatchComboSend *bool       `json:"show_batch_combo_send,omitempty"`
	BlindGiftInfo      *BlindGift  `json:"-"`
	Action             string      `json:"action"`               // 操作类型
	BatchComboId       string      `json:"batch_combo_id"`       // 批量组合ID
	BatchComboSend     interface{} `json:"batch_combo_send"`     // 批量组合发送信息
	BeatId             string      `json:"beatId"`               // Beat ID
	BizSource          string      `json:"biz_source"`           // 业务来源
	BlindGift          interface{} `json:"blind_gift"`           // 盲盒礼物信息
	BroadcastId        int         `json:"broadcast_id"`         // 广播ID
	CoinType           string      `json:"coin_type"`            // 币种类型
	ComboResourcesId   int         `json:"combo_resources_id"`   // 组合资源ID
	ComboSend          interface{} `json:"combo_send"`           // 组合发送信息
	ComboStayTime      int         `json:"combo_stay_time"`      // 组合停留时间
	ComboTotalCoin     int         `json:"combo_total_coin"`     // 组合总硬币数
	CritProb           int         `json:"crit_prob"`            // 批判概率
	Demarcation        int         `json:"demarcation"`          // 分界线
	DiscountPrice      int         `json:"discount_price"`       // 折扣价格
	Dmscore            int         `json:"dmscore"`              // DM分数
	Draw               int         `json:"draw"`                 // 抽奖
	Effect             int         `json:"effect"`               // 效果
	EffectBlock        int         `json:"effect_block"`         // 效果块
	Face               string      `json:"face"`                 // 头像URL
	FloatScResourceId  int         `json:"float_sc_resource_id"` // 浮动资源ID
	GiftId             int         `json:"giftId"`               // 礼物ID
	GiftName           string      `json:"giftName"`             // 礼物名称
	GiftType           int         `json:"giftType"`             // 礼物类型
	Gold               int         `json:"gold"`                 // 金币数量
	GuardLevel         int         `json:"guard_level"`          // 守护等级
	IsFirst            bool        `json:"is_first"`             // 是否首次赠送
	IsSpecialBatch     int         `json:"is_special_batch"`     // 是否特殊批量
	Magnification      float64     `json:"magnification"`        // 放大倍数
	MedalInfo          struct {
		AnchorRoomid     int    `json:"anchor_roomid"`      // 主播房间ID
		AnchorUname      string `json:"anchor_uname"`       // 主播用户名
		GuardLevel       int    `json:"guard_level"`        // 守护等级
		IconId           int    `json:"icon_id"`            // 图标ID
		IsLighted        int    `json:"is_lighted"`         // 是否点亮
		MedalColor       int    `json:"medal_color"`        // 勋章颜色
		MedalColorBorder int    `json:"medal_color_border"` // 勋章边框颜色
		MedalColorEnd    int    `json:"medal_color_end"`    // 勋章结束颜色
		MedalColorStart  int    `json:"medal_color_start"`  // 勋章开始颜色
		MedalLevel       int    `json:"medal_level"`        // 勋章等级
		MedalName        string `json:"medal_name"`         // 勋章名称
		Special          string `json:"special"`            // 特殊标记
		TargetId         int    `json:"target_id"`          // 目标ID
	} `json:"medal_info"`
	NameColor         string      `json:"name_color"`           // 名称颜色
	Num               int         `json:"num"`                  // 数量
	OriginalGiftName  string      `json:"original_gift_name"`   // 原始礼物名称
	Price             int         `json:"price"`                // 价格
	Rcost             int         `json:"rcost"`                // 实际花费
	Remain            int         `json:"remain"`               // 剩余数量
	Rnd               string      `json:"rnd"`                  // 随机数
	SendMaster        interface{} `json:"send_master"`          // 发送者信息
	Silver            int         `json:"silver"`               // 银币数量
	Super             int         `json:"super"`                // 是否超级礼物
	SuperBatchGiftNum int         `json:"super_batch_gift_num"` // 超级批量礼物数量
	SuperGiftNum      int         `json:"super_gift_num"`       // 超级礼物数量
	SvgaBlock         int         `json:"svga_block"`           // SVGA块
	TagImage          string      `json:"tag_image"`            // 标签图片URL
	Tid               string      `json:"tid"`                  // TID
	Timestamp         int         `json:"timestamp"`            // 时间戳
	TopList           interface{} `json:"top_list"`             // 顶级列表
	TotalCoin         int         `json:"total_coin"`           // 总硬币数
	Uid               int         `json:"uid"`                  // 用户ID
	Uname             string      `json:"uname"`                // 用户名
}

// 组合发送结构体
type ComboSend struct {
	Action         string `json:"action"`           // 操作类型
	BatchComboId   string `json:"batch_combo_id"`   // 批量组合ID
	BatchComboNum  int    `json:"batch_combo_num"`  // 批量组合数量
	ComboId        string `json:"combo_id"`         // 组合ID
	ComboNum       int    `json:"combo_num"`        // 组合数量
	ComboTotalCoin int    `json:"combo_total_coin"` // 组合总硬币数
	Dmscore        int    `json:"dmscore"`          // DM分数
	GiftId         int    `json:"gift_id"`          // 礼物ID
	GiftName       string `json:"gift_name"`        // 礼物名称
	GiftNum        int    `json:"gift_num"`         // 礼物数量
	IsShow         int    `json:"is_show"`          // 是否显示
	MedalInfo      struct {
		AnchorRoomid     int    `json:"anchor_roomid"`      // 主播房间ID
		AnchorUname      string `json:"anchor_uname"`       // 主播用户名
		GuardLevel       int    `json:"guard_level"`        // 守护等级
		IconId           int    `json:"icon_id"`            // 图标ID
		IsLighted        int    `json:"is_lighted"`         // 是否点亮
		MedalColor       int    `json:"medal_color"`        // 勋章颜色
		MedalColorBorder int    `json:"medal_color_border"` // 勋章边框颜色
		MedalColorEnd    int    `json:"medal_color_end"`    // 勋章结束颜色
		MedalColorStart  int    `json:"medal_color_start"`  // 勋章开始颜色
		MedalLevel       int    `json:"medal_level"`        // 勋章等级
		MedalName        string `json:"medal_name"`         // 勋章名称
		Special          string `json:"special"`            // 特殊标记
		TargetId         int    `json:"target_id"`          // 目标ID
	} `json:"medal_info"`
	NameColor  string      `json:"name_color"`  // 名称颜色
	RUname     string      `json:"r_uname"`     // 接收者用户名
	Ruid       int         `json:"ruid"`        // 接收者用户ID
	SendMaster interface{} `json:"send_master"` // 发送者信息
	TotalNum   int         `json:"total_num"`   // 总数量
	Uid        int         `json:"uid"`         // 用户ID
	Uname      string      `json:"uname"`       // 用户名
}

// GiftInfo contains display resources. A present zero EffectId disables effects.
type GiftInfo struct {
	ImgBasic string  `json:"img_basic"`
	Webp     string  `json:"webp"`
	Gif      string  `json:"gif"`
	EffectId *uint64 `json:"effect_id"`
}

// BlindGift separates the revealed gift value from the paid box cost.
type BlindGift struct {
	BlindGiftConfigId uint64 `json:"blind_gift_config_id"`
	From              uint64 `json:"from"`
	GiftAction        string `json:"gift_action"`
	GiftTipPrice      uint64 `json:"gift_tip_price"`
	OriginalGiftId    uint64 `json:"original_gift_id"`
	OriginalGiftName  string `json:"original_gift_name"`
	OriginalGiftPrice uint64 `json:"original_gift_price"`
}

func (g *Gift) Parse(data []byte) {
	if err := g.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse gift failed")
	}
}

// ParseJSON parses a legacy SEND_GIFT notification. Use ParseGiftsV2 for batches.
func (g *Gift) ParseJSON(data []byte) error {
	data = normalizeIntFlags(data, "data.medal_info.is_lighted")
	data = normalizeBoolFlags(data, "data.show_batch_combo_send")
	var next Gift
	if err := decodeData(data, &next); err != nil {
		return err
	}
	if gjson.GetBytes(data, "data.pb").Exists() {
		return fmt.Errorf("SEND_GIFT_V2 may contain multiple gifts; use ParseGiftsV2")
	}
	if err := requireJSONFields(data, "data.giftId", "data.num", "data.price", "data.total_coin", "data.coin_type", "data.timestamp"); err != nil {
		return err
	}
	if !gjson.GetBytes(data, "data.uid").Exists() && (next.SenderUinfo == nil || next.SenderUinfo.Uid == 0) {
		return fmt.Errorf("missing gift sender uid")
	}
	if next.CoinType != "gold" && next.CoinType != "silver" {
		return fmt.Errorf("unsupported gift coin type %q", next.CoinType)
	}
	fields := gjson.GetBytes(data, "data")
	if err := requireUnsignedFields(fields, "giftId", "num", "price", "total_coin"); err != nil {
		return err
	}
	if err := requireSignedField(fields, "timestamp"); err != nil {
		return err
	}
	if next.SenderUinfo == nil || next.SenderUinfo.Uid == 0 {
		if err := requireUnsignedFields(fields, "uid"); err != nil {
			return err
		}
	}
	if next.Num < 0 || uint64(next.Num) > math.MaxUint32 || next.Price < 0 || next.TotalCoin < 0 {
		return fmt.Errorf("invalid gift quantity or price")
	}
	if sender := next.SenderUinfo; sender != nil {
		if sender.Uid != 0 {
			uid, err := checkedInt(sender.Uid)
			if err != nil {
				return err
			}
			next.Uid = uid
		}
		next.Uname = firstNonEmpty(sender.Base.Name, next.Uname)
		next.Face = firstNonEmpty(sender.Base.Face, next.Face)
	}
	if info := next.GiftInfo; info != nil {
		next.GiftIcon = firstNonEmpty(info.ImgBasic, info.Webp, info.Gif)
		next.EffectId = info.EffectId
	}
	blind := gjson.GetBytes(data, "data.blind_gift")
	if blind.IsObject() {
		var value BlindGift
		if err := json.Unmarshal([]byte(blind.Raw), &value); err != nil {
			return fmt.Errorf("decode blind gift: %w", err)
		}
		next.BlindGiftInfo = &value
	}
	if next.Action == "" {
		next.Action = "投喂"
	}
	*g = next
	return nil
}

func (g Gift) IsPaid() bool  { return g.CoinType == "gold" }
func (g Gift) IsCombo() bool { return strings.TrimSpace(g.BatchComboId) != "" }

// RevealedTotalCoin is the revealed gift value; TotalCoin remains the paid cost.
func (g Gift) RevealedTotalCoin() uint64 {
	if blind := g.BlindGiftInfo; blind != nil {
		if g.Num <= 0 {
			return 0
		}
		num := uint64(g.Num)
		if blind.GiftTipPrice > math.MaxUint64/num {
			return math.MaxUint64
		}
		return blind.GiftTipPrice * num
	}
	if g.TotalCoin <= 0 {
		return 0
	}
	return uint64(g.TotalCoin)
}

func (g Gift) ValueCNYFen() uint64 {
	if !g.IsPaid() {
		return 0
	}
	return g.RevealedTotalCoin() / 10
}
