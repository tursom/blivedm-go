package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// RoomInfo
// api https://api.live.bilibili.com/room/v1/Room/room_init?id={} response
type RoomInfo struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Message string `json:"message"`
	Data    struct {
		RoomId      int   `json:"room_id"`
		ShortId     int   `json:"short_id"`
		Uid         int   `json:"uid"`
		NeedP2P     int   `json:"need_p2p"`
		IsHidden    bool  `json:"is_hidden"`
		IsLocked    bool  `json:"is_locked"`
		IsPortrait  bool  `json:"is_portrait"`
		LiveStatus  int   `json:"live_status"`
		HiddenTill  int   `json:"hidden_till"`
		LockTill    int   `json:"lock_till"`
		Encrypted   bool  `json:"encrypted"`
		PwdVerified bool  `json:"pwd_verified"`
		LiveTime    int64 `json:"live_time"`
		RoomShield  int   `json:"room_shield"`
		IsSp        int   `json:"is_sp"`
		SpecialType int   `json:"special_type"`
	} `json:"data"`
}

// DanmuInfo
// api https://api.live.bilibili.com/xlive/web-room/v1/index/getDanmuInfo?id={}&type=0 response
type DanmuInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Ttl     int    `json:"ttl"`
	Data    struct {
		Group            string  `json:"group"`
		BusinessId       int     `json:"business_id"`
		RefreshRowFactor float64 `json:"refresh_row_factor"`
		RefreshRate      int     `json:"refresh_rate"`
		MaxDelay         int     `json:"max_delay"`
		Token            string  `json:"token"`
		HostList         []struct {
			Host    string `json:"host"`
			Port    int    `json:"port"`
			WssPort int    `json:"wss_port"`
			WsPort  int    `json:"ws_port"`
		} `json:"host_list"`
	} `json:"data"`
}

func GetUid(cookie string) (int, error) {
	return GetUidContext(context.Background(), cookie)
}

func GetUidContext(ctx context.Context, cookie string) (int, error) {
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			IsLogin bool `json:"isLogin"`
			Mid     int  `json:"mid"`
		} `json:"data"`
	}
	if err := GetJsonWithHeaderContext(ctx, "https://api.bilibili.com/x/web-interface/nav", liveHeaders(cookie), &result); err != nil {
		return 0, err
	}
	if err := apiError(result.Code, result.Message, ""); err != nil {
		return 0, err
	}
	if !result.Data.IsLogin || result.Data.Mid <= 0 {
		return 0, errors.New("bilibili: not logged in")
	}
	return result.Data.Mid, nil
}

func GetDanmuInfo(roomID int, cookie string) (*DanmuInfo, error) {
	return GetDanmuInfoContext(context.Background(), roomID, cookie)
}

func GetDanmuInfoContext(ctx context.Context, roomID int, cookie string) (*DanmuInfo, error) {
	if roomID <= 0 {
		return nil, fmt.Errorf("invalid room ID: %d", roomID)
	}
	signedURL, err := WbiKeysSignStringContext(ctx, fmt.Sprintf("https://api.live.bilibili.com/xlive/web-room/v1/index/getDanmuInfo?id=%d&type=0", roomID))
	if err != nil {
		return nil, err
	}
	// 风控响应的 data 可能是 []，需先检查 code 再解析成功数据。
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Ttl     int             `json:"ttl"`
		Data    json.RawMessage `json:"data"`
	}
	if err := GetJsonWithHeaderContext(ctx, signedURL, liveHeaders(cookie), &envelope); err != nil {
		return nil, err
	}
	result := &DanmuInfo{Code: envelope.Code, Message: envelope.Message, Ttl: envelope.Ttl}
	if result.Code == 0 {
		if err := json.Unmarshal(envelope.Data, &result.Data); err != nil {
			return nil, err
		}
	}
	// 与 Rust 客户端一致，风控和空主机列表使用公共弹幕服务器。
	if result.Code == -352 || result.Code == 352 {
		result.Code = 0
		result.Data.Token = ""
		result.Data.HostList = nil
	}
	if err := apiError(result.Code, result.Message, ""); err != nil {
		return nil, err
	}
	if len(result.Data.HostList) == 0 {
		result.Data.HostList = append(result.Data.HostList, struct {
			Host    string `json:"host"`
			Port    int    `json:"port"`
			WssPort int    `json:"wss_port"`
			WsPort  int    `json:"ws_port"`
		}{Host: "broadcastlv.chat.bilibili.com", Port: 2243, WssPort: 443, WsPort: 2244})
	}
	return result, nil
}

func GetRoomInfo(roomID int) (*RoomInfo, error) {
	return GetRoomInfoContext(context.Background(), roomID)
}

func GetRoomInfoContext(ctx context.Context, roomID int) (*RoomInfo, error) {
	if roomID <= 0 {
		return nil, fmt.Errorf("invalid room ID: %d", roomID)
	}
	result := &RoomInfo{}
	if err := GetJsonWithHeaderContext(ctx, fmt.Sprintf("https://api.live.bilibili.com/room/v1/Room/room_init?id=%d", roomID), liveHeaders(""), result); err != nil {
		return nil, err
	}
	if err := apiError(result.Code, result.Message, result.Msg); err != nil {
		return nil, err
	}
	return result, nil
}

func GetRoomRealID(roomID int) (string, error) {
	return GetRoomRealIDContext(context.Background(), roomID)
}

func GetRoomRealIDContext(ctx context.Context, roomID int) (string, error) {
	res, err := GetRoomInfoContext(ctx, roomID)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(res.Data.RoomId), nil
}
