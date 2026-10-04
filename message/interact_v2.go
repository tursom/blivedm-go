package message

import (
	"fmt"
	"math"

	"github.com/tursom/blivedm-go/pb"
)

func (i *InteractWord) parseV2(data []byte) error {
	var event pb.EventInteractWord
	if err := decodeProto(data, &event); err != nil {
		return err
	}
	info := userInfoFromProto(event.Uinfo)
	next := InteractWord{Uname: event.Username, Uinfo: legacyUserInfo(info), UserDetails: &info}
	uid := event.Uid
	if uid == 0 {
		uid = info.Uid
	}
	if err := setInts(intField{&next.Uid, uid}, intField{&next.MsgType, uint64(event.MsgType)},
		intField{&next.Roomid, uint64(event.RoomId)}); err != nil {
		return err
	}
	next.Uname = firstNonEmpty(next.Uname, next.Uinfo.Base.Name)
	next.Uinfo.Guard.Level = int(event.GuardType)
	if next.MsgType == 0 {
		next.MsgType = 1
	}
	timestamp := uint64(event.Timestamp)
	if event.TriggerTime != 0 {
		// Round nanoseconds to milliseconds without overflowing on the addition.
		timestamp = event.TriggerTime / 1_000_000
		if event.TriggerTime%1_000_000 >= 500_000 {
			timestamp++
		}
	} else if event.TimestampMs != 0 {
		timestamp = event.TimestampMs
	}
	if err := setInts(intField{&next.Timestamp, timestamp}); err != nil {
		return err
	}
	if event.TriggerTime > math.MaxInt64 {
		return fmt.Errorf("interact trigger_time exceeds int64")
	}
	next.TriggerTime = int64(event.TriggerTime)
	userMedal := event.Uinfo.GetMedal()
	if userMedal != nil {
		next.FansMedal.MedalColor = int(userMedal.Color)
		if userMedal.ColorV2 != 0 {
			next.FansMedal.MedalColor = int(userMedal.ColorV2)
		}
	}
	if medal := event.Medal; medal != nil && medal.Level > 0 {
		next.FansMedal.MedalLevel = int(medal.Level)
		next.FansMedal.MedalName = firstNonEmpty(medal.Name, userMedal.GetName())
		next.FansMedal.AnchorRoomid = int(medal.RoomId)
		if next.FansMedal.AnchorRoomid == 0 {
			next.FansMedal.AnchorRoomid = next.Roomid
		}
		if err := setInts(intField{&next.FansMedal.TargetId, medal.Ruid}); err != nil {
			return err
		}
		next.FansMedal.GuardLevel = int(medal.GuardLevel)
		next.FansMedal.IsLighted = 1
		if medal.IsLighted != nil {
			next.FansMedal.IsLighted = boolInt(*medal.IsLighted)
		}
	} else if userMedal != nil && userMedal.Level > 0 {
		next.FansMedal.MedalLevel = int(userMedal.Level)
		next.FansMedal.MedalName = userMedal.Name
		next.FansMedal.AnchorRoomid = next.Roomid
		next.FansMedal.IsLighted = 1
		if err := setInts(intField{&next.FansMedal.TargetId, userMedal.Ruid}); err != nil {
			return err
		}
	}
	next.populateUser()
	*i = next
	return nil
}
