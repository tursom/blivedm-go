package packet

import "encoding/json"

type Enter struct {
	UID      int    `json:"uid"`
	Buvid    string `json:"buvid"`
	RoomID   int    `json:"roomid"`
	ProtoVer int    `json:"protover"`
	Platform string `json:"platform"`
	Type     int    `json:"type"`
	Key      string `json:"key"`
}

// NewEnterPacket 构造进入房间的包
// uid 可以为 0, key 在使用 broadcastlv 服务器的时候不需要
func NewEnterPacket(uid int, buvid string, roomID int, key string) []byte {
	ent := Enter{
		UID:      uid,
		Buvid:    buvid,
		RoomID:   roomID,
		ProtoVer: Brotli,
		Platform: "web",
		Type:     2,
		Key:      key,
	}
	// Enter contains only JSON-safe primitive fields.
	body, _ := json.Marshal(ent)
	pkt := NewPlainPacket(RoomEnter, body)
	return pkt.Build()
}
