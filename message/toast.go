package message

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type UserToast struct {
	Face             string    `json:"face,omitempty"`
	Source           uint32    `json:"source"`
	SenderUinfo      *UserInfo `json:"sender_uinfo,omitempty"`
	AnchorShow       bool      `json:"anchor_show"`
	Color            string    `json:"color"`
	Dmscore          int       `json:"dmscore"`
	EffectId         int       `json:"effect_id"`
	EndTime          int       `json:"end_time"`
	FaceEffectId     int       `json:"face_effect_id"`
	GiftId           int       `json:"gift_id"`
	GuardLevel       int       `json:"guard_level"`
	IsShow           int       `json:"is_show"`
	Num              int       `json:"num"`
	OpType           int       `json:"op_type"`
	PayflowId        string    `json:"payflow_id"`
	Price            int       `json:"price"`
	RoleName         string    `json:"role_name"`
	RoomEffectId     int       `json:"room_effect_id"`
	StartTime        int       `json:"start_time"`
	SvgaBlock        int       `json:"svga_block"`
	TargetGuardCount int       `json:"target_guard_count"`
	ToastMsg         string    `json:"toast_msg"`
	Uid              int       `json:"uid"`
	Unit             string    `json:"unit"`
	UserShow         bool      `json:"user_show"`
	Username         string    `json:"username"`
}

func (u *UserToast) Parse(data []byte) {
	if err := u.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse guard toast failed")
	}
}

// ParseJSON normalizes both USER_TOAST_MSG and USER_TOAST_MSG_V2.
func (u *UserToast) ParseJSON(data []byte) error {
	fields := gjson.GetBytes(data, "data")
	command := strings.SplitN(gjson.GetBytes(data, "cmd").String(), ":", 2)[0]
	var next UserToast
	if command == "USER_TOAST_MSG_V2" || fields.Get("guard_info").Exists() {
		var wire struct {
			SenderUinfo *UserInfo `json:"sender_uinfo"`
			GuardInfo   struct {
				GuardLevel int    `json:"guard_level"`
				RoleName   string `json:"role_name"`
				StartTime  int    `json:"start_time"`
				EndTime    int    `json:"end_time"`
			} `json:"guard_info"`
			PayInfo struct {
				Num       int    `json:"num"`
				Price     int    `json:"price"`
				Unit      string `json:"unit"`
				PayflowId string `json:"payflow_id"`
			} `json:"pay_info"`
			GiftInfo struct {
				GiftId int `json:"gift_id"`
			} `json:"gift_info"`
		}
		if err := decodeData(data, &wire); err != nil {
			return err
		}
		if err := requireUnsignedFields(fields, "sender_uinfo.uid", "guard_info.guard_level", "pay_info.num", "pay_info.price", "gift_info.gift_id"); err != nil {
			return err
		}
		if err := requireSignedField(fields, "guard_info.start_time"); err != nil {
			return err
		}
		uid, err := checkedInt(wire.SenderUinfo.Uid)
		if err != nil {
			return err
		}
		next.Uid, next.Username, next.Face = uid, wire.SenderUinfo.Base.Name, wire.SenderUinfo.Base.Face
		next.SenderUinfo = wire.SenderUinfo
		next.GuardLevel, next.RoleName = wire.GuardInfo.GuardLevel, wire.GuardInfo.RoleName
		next.StartTime, next.EndTime = wire.GuardInfo.StartTime, wire.GuardInfo.EndTime
		if value := fields.Get("guard_info.end_time"); !value.Exists() || value.Type == gjson.Null {
			next.EndTime = next.StartTime
		}
		next.Num, next.Price, next.Unit, next.PayflowId = wire.PayInfo.Num, wire.PayInfo.Price, wire.PayInfo.Unit, wire.PayInfo.PayflowId
		next.GiftId = wire.GiftInfo.GiftId
		next.Source, _ = optionalUint32(fields.Get("option.source"))
	} else {
		type PlainToast UserToast
		var wire struct {
			*PlainToast
			Source json.RawMessage `json:"source"`
		}
		if err := decodeData(data, &wire); err != nil {
			return err
		}
		if err := requireUnsignedFields(fields, "uid", "guard_level", "num", "price", "gift_id"); err != nil {
			return err
		}
		next = UserToast(*wire.PlainToast)
		next.Source, _ = optionalUint32(fields.Get("source"))
		if err := requireSignedField(fields, "start_time"); err != nil {
			return err
		}
		if value := fields.Get("end_time"); !value.Exists() || value.Type == gjson.Null {
			next.EndTime = next.StartTime
		}
		if sender := next.SenderUinfo; sender != nil {
			next.Username = firstNonEmpty(sender.Base.Name, next.Username)
			next.Face = firstNonEmpty(sender.Base.Face, next.Face)
		}
	}
	if next.GuardLevel < 1 || next.GuardLevel > 3 || uint64(next.Num) > math.MaxUint32 {
		return fmt.Errorf("invalid guard toast level or quantity")
	}
	*u = next
	return nil
}

func (u UserToast) GuardName() string { return firstNonEmpty(u.RoleName, guardName(u.GuardLevel)) }

// ValueCNYFen is the paid order total. Price already includes every purchased unit.
func (u UserToast) ValueCNYFen() uint64 {
	if u.Price <= 0 {
		return 0
	}
	return uint64(u.Price) / 10
}
