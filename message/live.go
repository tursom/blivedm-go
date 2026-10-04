package message

import (
	"encoding/json"
	"fmt"
	"strconv"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// StopLiveRoomList 停止直播房间列表结构体
type StopLiveRoomList struct {
	RoomIdList []int `json:"room_id_list"` // 房间ID列表
}

// Live 直播消息结构体
type LiveStart struct {
	Cmd             string `json:"cmd"`              // 命令
	LiveKey         string `json:"live_key"`         // 直播密钥
	VoiceBackground string `json:"voice_background"` // 语音背景
	SubSessionKey   string `json:"sub_session_key"`  // 子会话密钥
	LivePlatform    string `json:"live_platform"`    // 直播平台
	LiveModel       int    `json:"live_model"`       // 直播模式
	LiveTime        int    `json:"live_time"`        // 直播时间
	Roomid          int    `json:"roomid"`           // 房间ID
}

// 停止直播
type LiveStop struct {
	Cmd      string `json:"cmd"`
	MsgId    string `json:"msg_id"`
	PIsAck   bool   `json:"p_is_ack"`
	PMsgType int    `json:"p_msg_type"`
	Roomid   string `json:"roomid"`
	Round    int    `json:"round"` //开启轮播时存在,轮播状态: 1正在轮播 0未轮播
	SendTime int64  `json:"send_time"`
}

// Preparing 直播准备中消息结构体
type Preparing struct {
	Round  int    `json:"round"`
	Cmd    string `json:"cmd"`    // 命令
	Roomid string `json:"roomid"` // 房间ID
}

func (l *LiveStart) Parse(data []byte) {
	if err := l.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse live start failed")
	}
}

func (l *LiveStart) ParseJSON(data []byte) error {
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return fmt.Errorf("live event must be a JSON object")
	}
	type plain LiveStart
	var next LiveStart
	wire := struct {
		*plain
		Roomid json.RawMessage `json:"roomid"`
	}{plain: (*plain)(&next)}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	roomID, err := liveRoomID(wire.Roomid)
	if err != nil {
		return err
	}
	if roomID != "" {
		next.Roomid, err = strconv.Atoi(roomID)
		if err != nil {
			return fmt.Errorf("invalid live room ID: %w", err)
		}
	}
	*l = next
	return nil
}

func (l *LiveStop) Parse(data []byte) {
	if err := l.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse live stop failed")
	}
}

func (l *LiveStop) ParseJSON(data []byte) error {
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return fmt.Errorf("live event must be a JSON object")
	}
	type plain LiveStop
	var next LiveStop
	wire := struct {
		*plain
		Roomid json.RawMessage `json:"roomid"`
	}{plain: (*plain)(&next)}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	roomID, err := liveRoomID(wire.Roomid)
	if err != nil {
		return err
	}
	next.Roomid = roomID
	*l = next
	return nil
}

func (p *Preparing) Parse(data []byte) {
	if err := p.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse preparing failed")
	}
}

func (p *Preparing) ParseJSON(data []byte) error {
	var stop LiveStop
	if err := stop.ParseJSON(data); err != nil {
		return err
	}
	*p = Preparing{Cmd: stop.Cmd, Roomid: stop.Roomid, Round: stop.Round}
	return nil
}

func liveRoomID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var value string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
	} else {
		value = string(raw)
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return "", fmt.Errorf("invalid live room ID: %w", err)
	}
	return value, nil
}
