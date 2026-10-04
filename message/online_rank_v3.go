package message

import (
	log "github.com/sirupsen/logrus"
	"github.com/tursom/blivedm-go/pb"
)

type OnlineRankV3 struct {
	RankType   string             `json:"rank_type"`
	OnlineList []OnlineRankV3User `json:"online_list"`
}

type OnlineRankV3User struct {
	Uid        uint64    `json:"uid"`
	Face       string    `json:"face"`
	Score      string    `json:"score"`
	Uname      string    `json:"uname"`
	Rank       uint32    `json:"rank"`
	GuardLevel *uint32   `json:"guard_level,omitempty"`
	IsMystery  *bool     `json:"is_mystery,omitempty"`
	Uinfo      *UserInfo `json:"uinfo,omitempty"`
}

func (o *OnlineRankV3) Parse(data []byte) {
	if err := o.ParseJSON(data); err != nil {
		log.WithError(err).Error("parse online rank v3 failed")
	}
}

func (o *OnlineRankV3) ParseJSON(data []byte) error {
	var event pb.EventOnlineRank
	if err := decodeProto(data, &event); err != nil {
		return err
	}
	next := OnlineRankV3{RankType: event.RankType, OnlineList: make([]OnlineRankV3User, 0, len(event.OnlineList))}
	for _, user := range event.OnlineList {
		if _, err := checkedInt(user.Uid); err != nil {
			return err
		}
		entry := OnlineRankV3User{Uid: user.Uid, Face: user.Face, Score: user.Score,
			Uname: user.Uname, Rank: user.Rank, GuardLevel: user.GuardLevel, IsMystery: user.IsMystery}
		if user.Uinfo != nil {
			if _, err := checkedInt(user.Uinfo.Uid); err != nil {
				return err
			}
			info := userInfoFromProto(user.Uinfo)
			entry.Uinfo = &info
		}
		next.OnlineList = append(next.OnlineList, entry)
	}
	*o = next
	return nil
}

// OnlineUsers normalizes V3 users to the existing V2 public user model.
func (o OnlineRankV3) OnlineUsers() []OnlineRankUser {
	users := make([]OnlineRankUser, 0, len(o.OnlineList))
	for _, entry := range o.OnlineList {
		user := OnlineRankUser{Uid: int(entry.Uid), Uname: entry.Uname, Face: entry.Face,
			Score: entry.Score, Rank: int(entry.Rank)}
		if info := entry.Uinfo; info != nil {
			user.Uinfo = legacyUserInfo(*info)
			user.UserDetails = info
			if user.Uid == 0 {
				user.Uid = int(info.Uid)
			}
			user.Uname = firstNonEmpty(user.Uname, info.Base.Name)
			user.Face = firstNonEmpty(user.Face, info.Base.Face)
			user.GuardLevel = info.Guard.Level
		}
		if entry.GuardLevel != nil {
			user.GuardLevel = int(*entry.GuardLevel)
		}
		user.Uinfo.Base.Name, user.Uinfo.Base.Face = user.Uname, user.Face
		user.Uinfo.Guard.Level = user.GuardLevel
		users = append(users, user)
	}
	return users
}
