package message

import (
	"fmt"
	"time"

	"github.com/tursom/blivedm-go/pb"
)

// ParseGiftsV2 decodes every result in SEND_GIFT_V2, including ten-draw blind boxes.
// A malformed result rejects the whole batch, so callers never receive partial batches.
func ParseGiftsV2(data []byte) ([]Gift, error) {
	var event pb.EventSendGift
	if err := decodeProto(data, &event); err != nil {
		return nil, err
	}
	if len(event.GiftItems) == 0 {
		return nil, fmt.Errorf("gift batch has no items")
	}
	gifts := make([]Gift, 0, len(event.GiftItems))
	now := time.Now().Unix()
	for index, item := range event.GiftItems {
		gift, err := normalizeGiftV2(&event, item, now)
		if err != nil {
			return nil, fmt.Errorf("gift batch item %d: %w", index, err)
		}
		gifts = append(gifts, gift)
	}
	return gifts, nil
}

func normalizeGiftV2(event *pb.EventSendGift, item *pb.EventGiftItem, now int64) (Gift, error) {
	gift := Gift{
		Uname: event.Uname, Face: event.Face,
		GiftName: item.GiftName, CoinType: item.CoinType, Tid: item.Tid,
		BatchComboId: item.BatchComboId, Action: item.Action, ShowBatchComboSend: item.ShowBatchComboSend,
	}
	if err := setInts(
		intField{&gift.Uid, event.Uid}, intField{&gift.GuardLevel, event.GuardLevel},
		intField{&gift.GiftId, uint64(item.GiftId)}, intField{&gift.Num, uint64(item.Num)},
		intField{&gift.Price, item.Price}, intField{&gift.TotalCoin, item.TotalCoin},
		intField{&gift.Timestamp, item.Timestamp}, intField{&gift.SuperBatchGiftNum, item.SuperBatchGiftNum},
		intField{&gift.ComboResourcesId, item.ComboResourcesId}, intField{&gift.ComboTotalCoin, item.ComboTotalCoin},
		intField{&gift.ComboStayTime, item.ComboStayTime},
	); err != nil {
		return Gift{}, err
	}
	if gift.Num == 0 {
		gift.Num = 1
	}
	if gift.Timestamp == 0 {
		gift.Timestamp = int(now)
	}
	if gift.Action == "" {
		gift.Action = "--"
	}
	if gift.CoinType == "" {
		gift.CoinType = "gold"
	}
	if gift.CoinType != "gold" && gift.CoinType != "silver" {
		return Gift{}, fmt.Errorf("unsupported coin type %q", gift.CoinType)
	}
	if sender := event.SenderUinfo; sender != nil {
		info := userInfoFromProto(sender)
		gift.SenderUinfo = &info
		if sender.Uid != 0 {
			uid, err := checkedInt(sender.Uid)
			if err != nil {
				return Gift{}, err
			}
			gift.Uid = uid
		}
		gift.Uname = firstNonEmpty(info.Base.Name, gift.Uname)
		gift.Face = firstNonEmpty(info.Base.Face, gift.Face)
	}
	if info := item.GiftInfo; info != nil {
		gift.GiftInfo = &GiftInfo{ImgBasic: info.ImgBasic, Webp: info.Webp, Gif: info.Gif, EffectId: info.EffectId}
		gift.GiftIcon = firstNonEmpty(info.ImgBasic, info.Webp, info.Gif)
		gift.EffectId = info.EffectId
	}
	if blind := event.BlindGift; blind != nil && (blind.BlindGiftConfigId != 0 || blind.OriginalGiftId != 0 || blind.OriginalGiftName != "") {
		value := &BlindGift{
			BlindGiftConfigId: blind.BlindGiftConfigId, From: blind.From,
			GiftAction: blind.GiftAction, OriginalGiftId: blind.OriginalGiftId,
			OriginalGiftName: blind.OriginalGiftName, OriginalGiftPrice: blind.OriginalGiftPrice,
			GiftTipPrice: item.GiftTipPrice,
		}
		if value.GiftTipPrice == 0 {
			value.GiftTipPrice = blind.OriginalGiftPrice
		}
		gift.BlindGiftInfo, gift.BlindGift = value, value
	}
	if medal := event.MedalInfo; medal != nil {
		if err := setInts(intField{&gift.MedalInfo.TargetId, medal.Ruid}, intField{&gift.MedalInfo.MedalLevel, uint64(medal.Level)}); err != nil {
			return Gift{}, err
		}
		gift.MedalInfo.MedalName = medal.Name
		gift.MedalInfo.IsLighted = 1
		if medal.IsLighted != nil {
			gift.MedalInfo.IsLighted = boolInt(*medal.IsLighted)
		}
	}
	return gift, nil
}

func userInfoFromProto(info *pb.EventUserInfo) UserInfo {
	var out UserInfo
	if info == nil {
		return out
	}
	out.Uid = info.Uid
	if base := info.Base; base != nil {
		out.Base.Name, out.Base.Face = base.Name, base.Face
		out.Base.NameColor, out.Base.IsMystery = base.NameColor, base.IsMystery
	}
	if guard := info.Guard; guard != nil {
		out.Guard.Level, out.Guard.ExpiredStr = int(guard.Level), guard.ExpiredStr
	}
	if info.Wealth != nil {
		out.Wealth.Level = info.Wealth.Level
	}
	return out
}
