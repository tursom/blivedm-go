package message

import "testing"

func TestInteractWordParse(t *testing.T) {
	body := []byte(`{
		"cmd": "INTERACT_WORD",
		"data": {
			"uid": 1001,
			"uname": "fallback-name",
			"timestamp": 1710000000,
			"msg_type": 1,
			"uinfo": {
				"base": {"name": "nested-name", "face": "https://example.com/face.jpg"},
				"guard": {"level": 3}
			},
			"fans_medal": {
				"medal_level": 12,
				"medal_name": "medal",
				"anchor_roomid": 732,
				"target_id": 1000,
				"medal_color": 16777215
			}
		}
	}`)

	msg := &InteractWord{}
	msg.Parse(body)

	if msg.Uid != 1001 || msg.Uname != "fallback-name" {
		t.Fatalf("unexpected user: uid=%d uname=%q", msg.Uid, msg.Uname)
	}
	if msg.Uinfo.Base.Name != "nested-name" || msg.Uinfo.Guard.Level != 3 {
		t.Fatalf("unexpected uinfo: %+v", msg.Uinfo)
	}
	if msg.FansMedal.MedalLevel != 12 || msg.FansMedal.MedalName != "medal" {
		t.Fatalf("unexpected medal: %+v", msg.FansMedal)
	}
}

func TestOnlineRankCountParse(t *testing.T) {
	body := []byte(`{
		"cmd": "ONLINE_RANK_COUNT",
		"data": {
			"count": 100,
			"count_text": "100+",
			"online_count": 2000,
			"online_count_text": "2000"
		}
	}`)

	msg := &OnlineRankCount{}
	msg.Parse(body)

	if msg.Count != 100 || msg.CountText != "100+" || msg.OnlineCount != 2000 || msg.OnlineCountText != "2000" {
		t.Fatalf("unexpected online rank count: %+v", msg)
	}
}

func TestOnlineRankV2Parse(t *testing.T) {
	body := []byte(`{
		"cmd": "ONLINE_RANK_V2",
		"data": {
			"rank_type": "online_rank",
			"online_list": [{
				"uid": 1001,
				"uname": "fallback-name",
				"face": "fallback-face",
				"rank": 1,
				"score": "1000",
				"guard_level": 1,
				"uinfo": {
					"base": {"name": "nested-name", "face": "nested-face"},
					"guard": {"level": 3}
				}
			}]
		}
	}`)

	msg := &OnlineRankV2{}
	msg.Parse(body)

	if msg.RankType != "online_rank" || len(msg.OnlineList) != 1 {
		t.Fatalf("unexpected online rank v2: %+v", msg)
	}
	user := msg.OnlineList[0]
	if user.Name() != "nested-name" || user.Avatar() != "nested-face" || user.Guard() != 3 {
		t.Fatalf("unexpected online rank user helpers: %+v", user)
	}
}
