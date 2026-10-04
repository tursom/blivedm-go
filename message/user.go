package message

import (
	"encoding/json"
	"strconv"
	"strings"
)

type User struct {
	Uid          int
	Uname        string
	Admin        bool
	Urank        int
	MobileVerify bool
	Medal        *Medal
	GuardLevel   int
	UserLevel    int64
	Face         string
	WealthLevel  uint32
}

type Medal struct {
	Name     string
	Level    int
	Color    int
	UpRoomId int
	UpUid    int
	UpName   string
	IsLight  bool
}

// UserInfo is the nested user representation shared by newer notifications.
type UserInfo struct {
	Uid  uint64 `json:"uid"`
	Base struct {
		Name      string `json:"name"`
		Face      string `json:"face"`
		NameColor uint32 `json:"name_color"`
		IsMystery bool   `json:"is_mystery"`
	} `json:"base"`
	Guard struct {
		Level      int    `json:"level"`
		ExpiredStr string `json:"expired_str"`
	} `json:"guard"`
	Wealth struct {
		Level uint32 `json:"level"`
	} `json:"wealth"`
}

// UnmarshalJSON accepts numeric and textual name colors and numeric boolean
// flags without forcing callers to handle different wire representations.
func (u *UserInfo) UnmarshalJSON(data []byte) error {
	var value struct {
		Uid  uint64 `json:"uid"`
		Base struct {
			Name      string          `json:"name"`
			Face      string          `json:"face"`
			NameColor json.RawMessage `json:"name_color"`
			IsMystery json.RawMessage `json:"is_mystery"`
		} `json:"base"`
		Guard struct {
			Level      int    `json:"level"`
			ExpiredStr string `json:"expired_str"`
		} `json:"guard"`
		Wealth struct {
			Level json.RawMessage `json:"level"`
		} `json:"wealth"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var next UserInfo
	next.Uid = value.Uid
	next.Base.Name, next.Base.Face = value.Base.Name, value.Base.Face
	next.Guard.Level, next.Guard.ExpiredStr = value.Guard.Level, value.Guard.ExpiredStr
	if level, err := strconv.ParseUint(string(value.Wealth.Level), 10, 32); err == nil {
		next.Wealth.Level = uint32(level)
	}
	if color := value.Base.NameColor; len(color) != 0 && string(color) != "null" {
		raw := string(color)
		base := 10
		if raw[0] == '"' {
			if err := json.Unmarshal(color, &raw); err != nil {
				return err
			}
			if strings.HasPrefix(raw, "#") {
				raw, base = strings.TrimPrefix(raw, "#"), 16
			}
		}
		if raw != "" {
			parsed, err := strconv.ParseUint(raw, base, 32)
			if err == nil {
				next.Base.NameColor = uint32(parsed)
			}
		}
	}
	if flag := value.Base.IsMystery; len(flag) != 0 && string(flag) != "null" {
		switch string(flag) {
		case "true", "1":
			next.Base.IsMystery = true
		case "false", "0":
		default:
			// Unknown optional display flags do not invalidate the user.
		}
	}
	*u = next
	return nil
}

// LegacyUserInfo retains the exact anonymous field layout exposed by V1/V2.
type LegacyUserInfo = struct {
	Base struct {
		Name string `json:"name"`
		Face string `json:"face"`
	} `json:"base"`
	Guard struct {
		Level int `json:"level"`
	} `json:"guard"`
}

func legacyUserInfo(info UserInfo) LegacyUserInfo {
	var out LegacyUserInfo
	out.Base.Name, out.Base.Face = info.Base.Name, info.Base.Face
	out.Guard.Level = info.Guard.Level
	return out
}
