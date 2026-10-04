package message

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func wireInt(field protowire.Number, value uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, field, protowire.VarintType), value)
}
func wireBytes(field protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, field, protowire.BytesType), value)
}
func wireText(field protowire.Number, value string) []byte { return wireBytes(field, []byte(value)) }
func wireJoin(fields ...[]byte) []byte {
	var result []byte
	for _, field := range fields {
		result = append(result, field...)
	}
	return result
}
func pbNotification(wire []byte) []byte {
	return []byte(`{"data":{"pb":"` + base64.StdEncoding.EncodeToString(wire) + `"}}`)
}

func TestGiftV2TenDrawFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/ten_blind_gift_v2.json")
	if err != nil {
		t.Fatal(err)
	}
	gifts, err := ParseGiftsV2(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(gifts) != 3 {
		t.Fatalf("got %d gift results", len(gifts))
	}
	ids, quantities := []int{32128, 32125, 32126}, []int{4, 1, 5}
	var paid, revealed uint64
	for index, gift := range gifts {
		if gift.GiftId != ids[index] || gift.Num != quantities[index] || gift.Uid != 42 || gift.BlindGiftInfo == nil {
			t.Fatalf("gift %d: %+v", index, gift)
		}
		paid += uint64(gift.TotalCoin)
		revealed += gift.RevealedTotalCoin()
	}
	if paid != 150000 || revealed != 111000 {
		t.Fatalf("cost=%d revealed=%d", paid, revealed)
	}
}

func TestGiftV2WireNormalization(t *testing.T) {
	// Hand-written wire field numbers are independent from the generated schema.
	sender := wireJoin(wireInt(1, 1<<33), wireBytes(2, wireJoin(wireText(1, "nested"), wireText(2, "face"))))
	resource := wireJoin(wireText(1, "static"), wireText(2, "animated"), wireInt(3, 0))
	item := wireJoin(wireInt(1, 100), wireInt(3, 2), wireInt(5, 2500), wireInt(7, 3000),
		wireText(9, "transaction"), wireText(12, "batch"), wireInt(17, 1), wireBytes(35, resource), wireInt(36, 2500))
	blind := wireJoin(wireInt(1, 139), wireInt(2, 200), wireText(3, "box"), wireInt(6, 1500))
	body := pbNotification(wireJoin(wireInt(1, 7), wireText(2, "top"), wireBytes(9, blind),
		wireBytes(10, item), wireBytes(10, wireInt(1, 101)), wireBytes(15, sender)))
	gifts, err := ParseGiftsV2(body)
	if err != nil {
		t.Fatal(err)
	}
	gift := gifts[0]
	if len(gifts) != 2 || gift.Uid != 1<<33 || gift.Uname != "nested" || gift.Face != "face" {
		t.Fatalf("sender/batch: %+v", gifts)
	}
	if gift.EffectId == nil || *gift.EffectId != 0 || gift.GiftIcon != "static" || gift.ShowBatchComboSend == nil || !*gift.ShowBatchComboSend {
		t.Fatalf("display: %+v", gift)
	}
	if gift.Tid != "transaction" || !gift.IsCombo() || gift.RevealedTotalCoin() != 5000 || gift.ValueCNYFen() != 500 {
		t.Fatalf("transaction: %+v", gift)
	}
	if gifts[1].Num != 1 || gifts[1].Timestamp == 0 || gifts[1].CoinType != "gold" {
		t.Fatalf("defaults: %+v", gifts[1])
	}
}

func TestGiftLegacyOptionalMetadata(t *testing.T) {
	raw := []byte(`{"data":{"giftId":1,"num":2,"price":200,"total_coin":300,"coin_type":"gold","uid":7,
        "timestamp":1700000000,"show_batch_combo_send":1,
        "sender_uinfo":{"uid":42,"base":{"name":"nested","face":"face","name_color":"#abcdef","is_mystery":1}},
        "gift_info":{"img_basic":"static","webp":"animated","effect_id":0},
        "medal_info":{"is_lighted":true},
        "blind_gift":{"gift_tip_price":200}}}`)
	var gift Gift
	if err := gift.ParseJSON(raw); err != nil {
		t.Fatal(err)
	}
	if gift.Uid != 42 || gift.Uname != "nested" || gift.GiftIcon != "static" || gift.RevealedTotalCoin() != 400 {
		t.Fatalf("unexpected gift %+v", gift)
	}
	if gift.MedalInfo.IsLighted != 1 || gift.SenderUinfo.Base.NameColor != 0xabcdef || !gift.SenderUinfo.Base.IsMystery {
		t.Fatalf("metadata %+v", gift)
	}
	if gift.ShowBatchComboSend == nil || !*gift.ShowBatchComboSend {
		t.Fatal("numeric combo flag not accepted")
	}
}

func danmakuFixture(t testing.TB) []byte {
	t.Helper()
	meta := []any{0, 1, 25, 0xffffff, 1700000000123, 0, 0, "", 0, 0, 0, "", 0, map[string]any{}, map[string]any{},
		map[string]any{"user": map[string]any{"base": map[string]any{"face": "avatar"},
			"medal": map[string]any{"is_light": 0, "ruid": 987}, "wealth": map[string]any{"level": 3}}}}
	info := []any{meta, "hello", []any{42, "tester", 1, 0, 0, 5, 1},
		[]any{12, "medal", "anchor", 100, 200, "", 0, 0, 0, 0, 0, 1, 999}, []any{29}, nil, 0, 3,
		nil, nil, nil, nil, nil, nil, nil, nil, []any{41}}
	data, err := json.Marshal(map[string]any{"cmd": "DANMU_MSG:4:0:2:2:2:0", "info": info})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDanmakuMetadataAndOwnership(t *testing.T) {
	raw := danmakuFixture(t)
	original := string(raw)
	var dm Danmaku
	if err := dm.ParseJSON(raw); err != nil {
		t.Fatal(err)
	}
	if dm.Sender.UserLevel != 29 || dm.Sender.WealthLevel != 41 || dm.Sender.Face != "avatar" {
		t.Fatalf("sender: %+v", dm.Sender)
	}
	if dm.Sender.Medal.UpUid != 987 || dm.Sender.Medal.IsLight || dm.Mode != 1 || dm.Color != 0xffffff {
		t.Fatalf("medal/meta: %+v", dm)
	}
	for index := range raw {
		raw[index] = 'x'
	}
	if dm.Content != "hello" || dm.Sender.Uname != "tester" || dm.Raw != original {
		t.Fatal("decoded message aliases reusable input")
	}
	saved := dm
	if err := dm.ParseJSON([]byte(`{"info":[]}`)); err == nil {
		t.Fatal("malformed info accepted")
	}
	if !reflect.DeepEqual(dm, saved) {
		t.Fatal("failed parse changed receiver")
	}
}

func TestDMV2SupportsLargeUIDAndLegacyTimestamp(t *testing.T) {
	payload := wireJoin(wireText(6, "protobuf"), wireInt(7, 1700000000), wireInt(2, 1),
		wireBytes(20, wireJoin(wireInt(1, 1<<33), wireText(2, "new name"), wireText(4, "new face"))))
	var input map[string]any
	if err := json.Unmarshal(danmakuFixture(t), &input); err != nil {
		t.Fatal(err)
	}
	input["dm_v2"] = base64.StdEncoding.EncodeToString(payload)
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var dm Danmaku
	if err := dm.ParseJSON(data); err != nil {
		t.Fatal(err)
	}
	if dm.Sender.Uid != 1<<33 || dm.Content != "protobuf" || dm.Timestamp != 1700000000123 {
		t.Fatalf("unexpected dm v2: %+v", dm)
	}
	// Black is a valid protobuf color, despite being its default numeric value.
	if dm.Color != 0 {
		t.Fatalf("black became %x", dm.Color)
	}
	input = map[string]any{"dm_v2": base64.StdEncoding.EncodeToString(payload)}
	data, _ = json.Marshal(input)
	if err := dm.ParseJSON(data); err != nil {
		t.Fatal(err)
	}
	if dm.Timestamp != 1700000000000 || dm.Sender.Medal == nil {
		t.Fatalf("v2 defaults: %+v", dm)
	}
}

func TestInteractV2TimeAndMedalFallback(t *testing.T) {
	base := wireJoin(wireText(1, "fallback"), wireText(2, "avatar"))
	userMedal := wireJoin(wireText(1, "fallback medal"), wireInt(2, 11), wireInt(6, 0x123456), wireInt(10, 9988))
	user := wireJoin(wireInt(1, 1<<33), wireBytes(2, base), wireBytes(3, userMedal), wireBytes(4, wireInt(1, 7)))
	medal := wireJoin(wireInt(1, 9988), wireInt(2, 22), wireText(3, "top medal"), wireInt(8, 0), wireInt(12, 456))
	wire := wireJoin(wireInt(6, 456), wireBytes(9, medal), wireInt(15, 1700000000123456789), wireInt(16, 3), wireBytes(22, user))
	var event InteractWord
	if err := event.ParseJSON(pbNotification(wire)); err != nil {
		t.Fatal(err)
	}
	if event.Uid != 1<<33 || event.Uname != "fallback" || event.Timestamp != 1700000000123 || event.MsgType != 1 {
		t.Fatalf("unexpected interact: %+v", event)
	}
	if event.User.WealthLevel != 7 || event.User.GuardLevel != 3 || event.User.Medal.Name != "top medal" || event.User.Medal.IsLight || event.User.Medal.Color != 0x123456 {
		t.Fatalf("unexpected user: %+v", event.User)
	}
	for _, test := range []struct {
		field protowire.Number
		value uint64
		want  int
	}{
		{7, 1700000000, 1700000000}, {8, 1700000000123, 1700000000123}, {15, 1700000000123500000, 1700000000124},
	} {
		if err := event.ParseJSON(pbNotification(wireInt(test.field, test.value))); err != nil {
			t.Fatal(err)
		}
		if event.Timestamp != test.want {
			t.Fatalf("field %d time=%d want=%d", test.field, event.Timestamp, test.want)
		}
	}
}

func TestOnlineRankV3NormalizationAndZeroGuard(t *testing.T) {
	base := wireJoin(wireText(1, "nested"), wireText(2, "avatar"), wireInt(4, 1))
	info := wireJoin(wireInt(1, 1<<33), wireBytes(2, base), wireBytes(4, wireInt(1, 8)), wireBytes(6, wireInt(1, 3)))
	entry := wireJoin(wireText(3, "12345"), wireText(4, "top"), wireInt(5, 1), wireInt(6, 0), wireInt(7, 1), wireBytes(8, info))
	var rank OnlineRankV3
	if err := rank.ParseJSON(pbNotification(wireJoin(wireText(1, "online_rank"), wireBytes(3, entry)))); err != nil {
		t.Fatal(err)
	}
	users := rank.OnlineUsers()
	if len(users) != 1 || users[0].Uid != 1<<33 || users[0].Name() != "top" || users[0].Avatar() != "avatar" || users[0].Guard() != 0 {
		t.Fatalf("normalized rank: %+v", users)
	}
	if !*rank.OnlineList[0].IsMystery || users[0].UserDetails.Wealth.Level != 8 {
		t.Fatal("nested metadata lost")
	}
}

func TestProtocolRejectsMalformedPayloads(t *testing.T) {
	malformed := [][]byte{
		[]byte(`{`), []byte(`{"data":null}`), []byte(`{"data":[]}`), []byte(`{"data":{"pb":"!"}}`),
		pbNotification([]byte{0x08, 0x80}), pbNotification([]byte{0x52, 0xff}),
		pbNotification([]byte{0x08, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02}),
	}
	for _, raw := range malformed {
		if _, err := ParseGiftsV2(raw); err == nil {
			t.Fatalf("gift accepted %q", raw)
		}
		if err := new(InteractWord).ParseJSON(raw); err == nil {
			t.Fatalf("interact accepted %q", raw)
		}
		if err := new(OnlineRankV3).ParseJSON(raw); err == nil {
			t.Fatalf("rank accepted %q", raw)
		}
	}
	if _, err := ParseGiftsV2(pbNotification(wireInt(1, 42))); err == nil {
		t.Fatal("empty gift batch accepted")
	}
	tooLarge := pbNotification(wireJoin(wireInt(1, math.MaxUint64), wireBytes(10, wireInt(1, 1))))
	if _, err := ParseGiftsV2(tooLarge); err == nil {
		t.Fatal("integer overflow accepted")
	}
}

func FuzzProtocolParsers(f *testing.F) {
	f.Add([]byte(`{"data":{"pb":"CAEqAWc="}}`))
	f.Add([]byte(`{"info":[]}`))
	f.Add([]byte(`{"data":null}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		_ = new(Danmaku).ParseJSON(body)
		_ = new(Gift).ParseJSON(body)
		_, _ = ParseGiftsV2(body)
		_ = new(InteractWord).ParseJSON(body)
		_ = new(OnlineRankCount).ParseJSON(body)
		_ = new(OnlineRankV2).ParseJSON(body)
		_ = new(OnlineRankV3).ParseJSON(body)
	})
}

func BenchmarkDanmakuParseJSON(b *testing.B) {
	body := danmakuFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		var dm Danmaku
		if err := dm.ParseJSON(body); err != nil {
			b.Fatal(err)
		}
	}
}

func TestProtocolRejectsUint32Overflow(t *testing.T) {
	tooMany := wireJoin(wireInt(1, 1), wireInt(3, math.MaxUint32+1))
	if _, err := ParseGiftsV2(pbNotification(wireBytes(10, tooMany))); err == nil {
		t.Fatal("gift quantity silently truncated")
	}
	rank := pbNotification(wireBytes(3, wireInt(5, math.MaxUint32+1)))
	if err := new(OnlineRankV3).ParseJSON(rank); err == nil {
		t.Fatal("rank silently truncated")
	}
}

func TestLegacyNestedStructLiteralCompatibility(t *testing.T) {
	// This is the exact Uinfo type exposed before adding V2/V3 metadata.
	info := struct {
		Base struct {
			Name string `json:"name"`
			Face string `json:"face"`
		} `json:"base"`
		Guard struct {
			Level int `json:"level"`
		} `json:"guard"`
	}{}
	_ = InteractWord{Uinfo: info}
	_ = OnlineRankUser{Uinfo: info}
}

func TestUnknownOptionalUserMetadataDoesNotDropGift(t *testing.T) {
	var gift Gift
	err := gift.ParseJSON([]byte(`{"data":{"giftId":1,"num":1,"price":1,"total_coin":1,"coin_type":"gold","uid":42,"timestamp":1,
        "sender_uinfo":{"base":{"name":"tester","name_color":{"future":1},"is_mystery":"future"},"wealth":{"level":4294967296}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if gift.Uname != "tester" || gift.SenderUinfo.Wealth.Level != 0 {
		t.Fatalf("unexpected metadata %+v", gift.SenderUinfo)
	}
}
