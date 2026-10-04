package message

import (
	"reflect"
	"testing"
)

func TestSuperChatOptionalMetadataAndPrice(t *testing.T) {
	minimal := []byte(`{"data":{"id":123,"price":30,"uid":456,"start_time":1700000000}}`)
	var sc SuperChat
	if err := sc.ParseJSON(minimal); err != nil {
		t.Fatal(err)
	}
	if sc.EndTime != sc.StartTime || sc.Time != 0 || sc.BackgroundColor != "#EDF5FF" || sc.MessageFontColor != "#323232" {
		t.Fatalf("unexpected defaults: %+v", sc)
	}
	if sc.ValueCNYFen() != 3000 {
		t.Fatalf("SC price unit changed: %d", sc.ValueCNYFen())
	}
	if err := sc.ParseJSON([]byte(`{"data":{"id":123,"price":30,"uid":456,"start_time":1700000000,"time":60}}`)); err != nil {
		t.Fatal(err)
	}
	if sc.EndTime != 1700000060 || sc.Time != 60 {
		t.Fatalf("duration not applied: %+v", sc)
	}
	if err := sc.ParseJSON([]byte(`{"data":{"id":123,"price":30,"uid":456,"start_time":1700000000,"end_time":1700000120}}`)); err != nil {
		t.Fatal(err)
	}
	if sc.Time != 120 {
		t.Fatalf("expiry not used for duration: %+v", sc)
	}
	if err := sc.ParseJSON([]byte(`{"data":{"id":123,"price":30,"uid":456,"start_time":1700000000,"time":4294967296,"user_info":{"user_level":4294967296}}}`)); err != nil {
		t.Fatal(err)
	}
	if sc.Time != 0 || sc.EndTime != sc.StartTime || sc.UserInfo.UserLevel != 0 {
		t.Fatalf("noncritical overflow was not defaulted: %+v", sc)
	}
}

func TestSuperChatMedalColorAndLightVariants(t *testing.T) {
	for _, fixture := range []string{
		`{"data":{"id":1,"price":30,"uid":2,"start_time":3,"medal_info":{"medal_level":10,"medal_name":"medal","anchor_uname":"anchor","anchor_roomid":1,"target_id":99,"medal_color":16711680,"is_lighted":false}}}`,
		`{"data":{"id":1,"price":30,"uid":2,"start_time":3,"medal_info":{"medal_level":10,"medal_name":"medal","anchor_uname":"anchor","anchor_roomid":1,"target_id":99,"medal_color":"#ff0000","is_lighted":0}}}`,
	} {
		var sc SuperChat
		if err := sc.ParseJSON([]byte(fixture)); err != nil {
			t.Fatal(err)
		}
		if sc.Medal == nil || sc.Medal.Color != 0xff0000 || sc.Medal.IsLight || sc.Medal.UpUid != 99 {
			t.Fatalf("wrong medal: %+v", sc.Medal)
		}
		if sc.MedalInfo.MedalColor != "#ff0000" || sc.Sender.Medal != sc.Medal {
			t.Fatalf("legacy and normalized fields disagree: %+v", sc)
		}
	}
}

func TestGuardAndSuperChatRejectMalformedCriticalFields(t *testing.T) {
	for _, body := range []string{
		`{"data":{}}`,
		`{"data":{"uid":42,"guard_level":0,"num":1,"price":198000,"gift_id":10003,"start_time":1700000000}}`,
		`{"data":{"uid":42,"guard_level":3,"num":4294967296,"price":198000,"gift_id":10003,"start_time":1700000000}}`,
		`{"data":{"uid":42,"guard_level":3,"num":1,"gift_id":10003,"start_time":1700000000}}`,
	} {
		before := GuardBuy{Uid: 99}
		guard := before
		if err := guard.ParseJSON([]byte(body)); err == nil {
			t.Fatalf("accepted invalid guard: %s", body)
		}
		if !reflect.DeepEqual(before, guard) {
			t.Fatal("failed guard parse modified receiver")
		}
	}
	for _, body := range []string{
		`{"data":{}}`,
		`{"data":{"id":1,"uid":2,"price":4294967296,"start_time":3}}`,
		`{"data":{"id":1,"uid":2,"price":-1,"start_time":3}}`,
	} {
		before := SuperChat{Id: 99}
		sc := before
		if err := sc.ParseJSON([]byte(body)); err == nil {
			t.Fatalf("accepted invalid SC: %s", body)
		}
		if !reflect.DeepEqual(before, sc) {
			t.Fatal("failed SC parse modified receiver")
		}
	}
}

func TestGuardDefaultsAndToastVersions(t *testing.T) {
	var guard GuardBuy
	if err := guard.ParseJSON([]byte(`{"data":{"uid":42,"guard_level":3,"num":1,"price":198000,"gift_id":10003,"start_time":1700000000}}`)); err != nil {
		t.Fatal(err)
	}
	if guard.EndTime != guard.StartTime || guard.GuardName() != "舰长" || guard.ValueCNYFen() != 19800 {
		t.Fatalf("unexpected guard: %+v", guard)
	}
	var first, second UserToast
	if err := first.ParseJSON([]byte(`{"cmd":"USER_TOAST_MSG","data":{"uid":42,"username":"tester","guard_level":3,"num":3,"price":138000,"gift_id":10003,"role_name":"舰长","unit":"月","payflow_id":"order","source":2,"start_time":1700000000}}`)); err != nil {
		t.Fatal(err)
	}
	if err := second.ParseJSON([]byte(`{"cmd":"USER_TOAST_MSG_V2:1","data":{"sender_uinfo":{"uid":42,"base":{"name":"tester","face":"face"}},"guard_info":{"guard_level":3,"role_name":"舰长","start_time":1700000000},"pay_info":{"num":3,"price":138000,"unit":"月","payflow_id":"order"},"gift_info":{"gift_id":10003},"option":{"source":2}}}`)); err != nil {
		t.Fatal(err)
	}
	if first.Uid != second.Uid || first.Username != second.Username || first.Price != second.Price || first.PayflowId != second.PayflowId || first.EndTime != second.EndTime || first.Num != second.Num || first.Source != second.Source {
		t.Fatalf("toast versions differ: %+v / %+v", first, second)
	}
	if second.Face != "face" || second.GuardName() != "舰长" || second.ValueCNYFen() != 13800 {
		t.Fatalf("wrong toast total or sender: %+v", second)
	}
}

func TestLiveRoomIDVariants(t *testing.T) {
	for _, room := range []string{`123`, `"123"`} {
		var start LiveStart
		if err := start.ParseJSON([]byte(`{"cmd":"LIVE","live_key":"key","live_time":1700000000,"roomid":` + room + `}`)); err != nil {
			t.Fatal(err)
		}
		if start.Roomid != 123 || start.LiveKey != "key" {
			t.Fatalf("wrong live start: %+v", start)
		}
		var stop LiveStop
		if err := stop.ParseJSON([]byte(`{"cmd":"PREPARING","roomid":` + room + `,"round":1}`)); err != nil {
			t.Fatal(err)
		}
		if stop.Roomid != "123" || stop.Round != 1 {
			t.Fatalf("wrong live stop: %+v", stop)
		}
		var preparing Preparing
		if err := preparing.ParseJSON([]byte(`{"cmd":"PREPARING","roomid":` + room + `,"round":1}`)); err != nil {
			t.Fatal(err)
		}
		if preparing.Roomid != "123" || preparing.Round != 1 {
			t.Fatalf("wrong preparing: %+v", preparing)
		}
	}
	var start LiveStart
	if err := start.ParseJSON([]byte(`{"roomid":-1}`)); err == nil {
		t.Fatal("accepted negative room ID")
	}
	if err := start.ParseJSON([]byte(`null`)); err == nil {
		t.Fatal("accepted null live event")
	}
}
