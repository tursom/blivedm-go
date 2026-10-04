package message

import (
	"fmt"
	"math"
	"strconv"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// GuardBuy 购买守护消息结构体
type GuardBuy struct {
	Uid        int    `json:"uid"`         // 用户ID
	Username   string `json:"username"`    // 用户名
	GuardLevel int    `json:"guard_level"` // 守护等级
	Num        int    `json:"num"`         // 数量
	Price      int    `json:"price"`       // 价格
	GiftId     int    `json:"gift_id"`     // 礼物ID
	GiftName   string `json:"gift_name"`   // 礼物名称
	StartTime  int    `json:"start_time"`  // 开始时间戳
	EndTime    int    `json:"end_time"`    // 结束时间戳
}

func (g *GuardBuy) Parse(data []byte) {
	if err := g.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse guard purchase failed")
	}
}

func (g *GuardBuy) ParseJSON(data []byte) error {
	var next GuardBuy
	if err := decodeData(data, &next); err != nil {
		return err
	}
	fields := gjson.GetBytes(data, "data")
	if err := requireUnsignedFields(fields, "uid", "guard_level", "num", "price", "gift_id"); err != nil {
		return err
	}
	if err := requireSignedField(fields, "start_time"); err != nil {
		return err
	}
	if next.GuardLevel < 1 || next.GuardLevel > 3 || uint64(next.Num) > math.MaxUint32 {
		return fmt.Errorf("invalid guard level or quantity")
	}
	if value := fields.Get("end_time"); !value.Exists() || value.Type == gjson.Null {
		next.EndTime = next.StartTime
	}
	*g = next
	return nil
}

func (g GuardBuy) GuardName() string { return guardName(g.GuardLevel) }

// ValueCNYFen returns the quoted amount; GUARD_BUY may differ from the paid order.
func (g GuardBuy) ValueCNYFen() uint64 {
	if g.Price <= 0 {
		return 0
	}
	return uint64(g.Price) / 10
}

func guardName(level int) string {
	switch level {
	case 1:
		return "总督"
	case 2:
		return "提督"
	case 3:
		return "舰长"
	default:
		return "无"
	}
}

func requireUnsignedFields(data gjson.Result, fields ...string) error {
	for _, field := range fields {
		value := data.Get(field)
		if value.Type != gjson.Number {
			return fmt.Errorf("missing or invalid %s", field)
		}
		if _, err := strconv.ParseUint(value.Raw, 10, 64); err != nil {
			return fmt.Errorf("invalid %s: %w", field, err)
		}
	}
	return nil
}

func requireSignedField(data gjson.Result, field string) error {
	value := data.Get(field)
	if value.Type != gjson.Number {
		return fmt.Errorf("missing or invalid %s", field)
	}
	if _, err := strconv.ParseInt(value.Raw, 10, 64); err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	return nil
}
