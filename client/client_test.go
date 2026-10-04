package client

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tursom/blivedm-go/message"
	"github.com/tursom/blivedm-go/packet"
	"github.com/tursom/blivedm-go/pb"
	"google.golang.org/protobuf/proto"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockRoomAPI(t *testing.T) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/room/v1/Room/room_init" {
			return nil, fmt.Errorf("unexpected API %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"room_id":123}}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}
func newTestClient(t *testing.T, host string) *Client {
	t.Helper()
	c, err := NewClientWithOptions(1, Options{ReconnectInterval: 5 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond, JoinTimeout: time.Second, ReadIdleTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c.SetHost(host)
	t.Cleanup(func() { c.Stop(); waitDone(t, c.Done()) })
	return c
}
func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not stop")
	}
}
func sendPacket(conn *websocket.Conn, operation uint32, body string) error {
	p := packet.NewPacket(packet.Plain, operation, []byte(body))
	return conn.WriteMessage(websocket.BinaryMessage, p.Build())
}
func socketServer(t *testing.T, session func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		p, err := packet.Decode(data)
		if err != nil || p.Operation != packet.RoomEnter {
			t.Errorf("expected enter packet, got %v, %v", p.Operation, err)
			return
		}
		session(conn)
	}))
	t.Cleanup(server.Close)
	return server
}
func socketURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/sub"
}

func TestStartStopRestartAndHeartbeat(t *testing.T) {
	mockRoomAPI(t)
	heartbeats := make(chan struct{}, 16)
	server := socketServer(t, func(conn *websocket.Conn) {
		if sendPacket(conn, packet.RoomEnterResponse, `{"code":0}`) != nil {
			return
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			p, err := packet.Decode(data)
			if err != nil {
				t.Error(err)
				return
			}
			if p.Operation == packet.HeartBeat {
				select {
				case heartbeats <- struct{}{}:
				default:
				}
			}
		}
	})
	c := newTestClient(t, socketURL(server))
	c.Stop()
	for i := 0; i < 2; i++ {
		if err := c.StartContext(testContext(t)); err != nil {
			t.Fatal(err)
		}
		if err := c.Start(); !errors.Is(err, ErrAlreadyStarted) {
			t.Fatalf("duplicate Start: %v", err)
		}
		select {
		case <-heartbeats:
		case <-time.After(time.Second):
			t.Fatal("missing heartbeat")
		}
		c.Stop()
		c.Stop()
		waitDone(t, c.Done())
	}
}
func TestReconnectAfterAuthenticationRejected(t *testing.T) {
	mockRoomAPI(t)
	var sessions atomic.Int32
	delivered := make(chan string, 1)
	server := socketServer(t, func(conn *websocket.Conn) {
		if sessions.Add(1) == 1 {
			_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":-101}`)
		} else {
			_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":0}`)
			_ = sendPacket(conn, packet.Notification, `{"cmd":"TEST","data":"ok"}`)
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	c := newTestClient(t, socketURL(server))
	c.RegisterCustomEventHandler("TEST", func(raw string) {
		select {
		case delivered <- raw:
		default:
		}
	})
	if err := c.StartContext(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("missing event after authentication rejection")
	}
	if sessions.Load() < 2 {
		t.Fatal("did not reconnect")
	}
}
func TestNotificationsRemainOrderedAndStopFromHandler(t *testing.T) {
	mockRoomAPI(t)
	server := socketServer(t, func(conn *websocket.Conn) {
		var frame []byte
		for i := 0; i < 20; i++ {
			p := packet.NewPacket(packet.Plain, packet.Notification, []byte(fmt.Sprintf(`{"cmd":"TEST","n":%d}`, i)))
			frame = append(frame, p.Build()...)
		}
		_ = conn.WriteMessage(websocket.BinaryMessage, frame)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	c := newTestClient(t, socketURL(server))
	var got []string
	c.RegisterCustomEventHandler("TEST", func(raw string) {
		got = append(got, raw)
		if len(got) == 20 {
			c.Stop()
		}
	})
	if err := c.StartContext(testContext(t)); err != nil {
		t.Fatal(err)
	}
	waitDone(t, c.Done())
	for i, raw := range got {
		if raw != fmt.Sprintf(`{"cmd":"TEST","n":%d}`, i) {
			t.Fatalf("out of order: %v", got)
		}
	}
	if len(got) != 20 {
		t.Fatalf("got %d events", len(got))
	}
}
func TestStartContextCancelsInitialization(t *testing.T) {
	previous := http.DefaultTransport
	entered := make(chan struct{})
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	defer func() { http.DefaultTransport = previous }()
	c := NewClient(1)
	result := make(chan error, 1)
	go func() { result <- c.Start() }()
	<-entered
	c.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initialization not cancelled")
	}
	waitDone(t, c.Done())
}
func TestStartContextCancelsRetry(t *testing.T) {
	mockRoomAPI(t)
	attempted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case attempted <- struct{}{}:
		default:
		}
		http.Error(w, "offline", 503)
	}))
	defer server.Close()
	c := newTestClient(t, socketURL(server))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- c.StartContext(ctx) }()
	<-attempted
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry not cancelled")
	}
}
func TestJoinTimeoutReconnects(t *testing.T) {
	mockRoomAPI(t)
	joined := make(chan struct{}, 1)
	var sessions atomic.Int32
	server := socketServer(t, func(conn *websocket.Conn) {
		if sessions.Add(1) >= 2 {
			select {
			case joined <- struct{}{}:
			default:
			}
			_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":0}`)
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	c := newTestClient(t, socketURL(server))
	c.options.JoinTimeout = 30 * time.Millisecond
	if err := c.StartContext(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("join timeout did not reconnect")
	}
}
func TestHandlerParsingOverrideAndRaw(t *testing.T) {
	c := NewClient(1)
	var calls []string
	c.OnRawEvent(func(cmd string, _ []byte) { calls = append(calls, "raw:"+cmd) })
	c.OnOnlineRankCount(func(*message.OnlineRankCount) { calls = append(calls, "typed") })
	c.RegisterCustomEventHandler("ONLINE_RANK_COUNT", func(string) { calls = append(calls, "custom") })
	p := packet.NewPacket(packet.Plain, packet.Notification, []byte("{\"cmd\" : \"ONLINE_RANK_COUNT:extra\", \"data\":{\"count\":12,\"count_text\":\"12\",\"online_count\":12,\"online_count_text\":\"12\"}}"))
	if err := c.HandlePacket(p); err != nil {
		t.Fatal(err)
	}
	c.RegisterCustomEventHandler("ONLINE_RANK_COUNT", nil)
	if err := c.HandlePacket(p); err != nil {
		t.Fatal(err)
	}
	want := []string{"raw:ONLINE_RANK_COUNT:extra", "custom", "raw:ONLINE_RANK_COUNT:extra", "typed"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("got %v", calls)
	}
	for _, raw := range []string{`{"cmd":5}`, `{"cmd":"TEST"`, `{"data":{"cmd":"NESTED"}}`} {
		p.Body = []byte(raw)
		if err := c.HandlePacket(p); err == nil {
			t.Fatalf("accepted malformed envelope %s", raw)
		}
	}
}
func TestHandlerPanicAndConcurrentRegistration(t *testing.T) {
	c := NewClient(1)
	c.OnOnlineRankCount(func(*message.OnlineRankCount) { panic("expected test panic") })
	var seen int
	c.OnOnlineRankCount(func(*message.OnlineRankCount) { seen++ })
	p := packet.NewPacket(packet.Plain, packet.Notification, []byte(`{"cmd":"ONLINE_RANK_COUNT","data":{"count":1,"count_text":"1","online_count":1,"online_count_text":"1"}}`))
	c.Handle(p)
	if seen != 1 {
		t.Fatal("panic suppressed following handler")
	}
	c = NewClient(1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			c.OnOnlineRankCount(func(*message.OnlineRankCount) {})
			c.RegisterCustomEventHandler("UNKNOWN", func(string) {})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			c.Handle(p)
		}
	}()
	wg.Wait()
}
func BenchmarkDispatch(b *testing.B) {
	c := NewClient(1)
	c.OnOnlineRankCount(func(*message.OnlineRankCount) {})
	p := packet.NewPacket(packet.Plain, packet.Notification, []byte(`{"cmd":"ONLINE_RANK_COUNT","data":{"count":123,"count_text":"123","online_count":123,"online_count_text":"123"}}`))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Handle(p)
	}
}

func TestOptionsAndEndpoints(t *testing.T) {
	if _, err := NewClientWithOptions(1, Options{EventBufferCapacity: -1}); err == nil {
		t.Fatal("accepted negative capacity")
	}
	if _, err := NewClientWithOptions(1, Options{HeartbeatInterval: -1}); err == nil {
		t.Fatal("accepted negative heartbeat interval")
	}
	for input, want := range map[string]string{"host.example": "wss://host.example/sub", "host.example:2245": "wss://host.example:2245/sub", "ws://localhost:1234": "ws://localhost:1234/sub", "wss://host.example/custom": "wss://host.example/custom"} {
		got, err := endpoint(input)
		if err != nil || got != want {
			t.Fatalf("endpoint(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := endpoint("https://host.example"); err == nil {
		t.Fatal("accepted HTTP endpoint")
	}
}
func TestStopWithFullEventQueue(t *testing.T) {
	mockRoomAPI(t)
	serverClosed := make(chan struct{})
	server := socketServer(t, func(conn *websocket.Conn) {
		defer close(serverClosed)
		var frame []byte
		for i := 0; i < 20; i++ {
			p := packet.NewPacket(packet.Plain, packet.Notification, []byte(`{"cmd":"TEST"}`))
			frame = append(frame, p.Build()...)
		}
		_ = conn.WriteMessage(websocket.BinaryMessage, frame)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	c := newTestClient(t, socketURL(server))
	c.options.EventBufferCapacity = 1
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	c.RegisterCustomEventHandler("TEST", func(string) { close(entered); <-release })
	if err := c.StartContext(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("missing callback")
	}
	c.Stop()
	select {
	case <-serverClosed:
	case <-time.After(time.Second):
		t.Fatal("Stop did not close transport with a blocked callback")
	}
	releaseOnce.Do(func() { close(release) })
	waitDone(t, c.Done())
}

func TestNewProtocolDispatch(t *testing.T) {
	c := NewClient(1)
	var gifts []int
	var interactions, ranks, toasts, raws int
	c.OnGift(func(g *message.Gift) { gifts = append(gifts, g.Num) })
	c.OnInteractWord(func(*message.InteractWord) { interactions++ })
	c.OnOnlineRankV3(func(*message.OnlineRankV3) { ranks++ })
	c.OnUserToast(func(*message.UserToast) { toasts++ })
	c.OnRawEvent(func(string, []byte) { raws++ })
	for _, entry := range []struct {
		cmd     string
		payload proto.Message
	}{
		{"SEND_GIFT_V2", &pb.EventSendGift{Uid: 42, GiftItems: []*pb.EventGiftItem{{GiftId: 1, Num: 4}, {GiftId: 2, Num: 6}}}},
		{"INTERACT_WORD_V2", &pb.EventInteractWord{Uid: 42, Username: "user", MsgType: 1}},
		{"ONLINE_RANK_V3", &pb.EventOnlineRank{RankType: "online_rank", OnlineList: []*pb.EventOnlineRankUser{{Uid: 42}}}},
	} {
		wire, err := proto.Marshal(entry.payload)
		if err != nil {
			t.Fatal(err)
		}
		raw := fmt.Sprintf(`{"cmd":%q,"data":{"pb":%q}}`, entry.cmd, base64.StdEncoding.EncodeToString(wire))
		if err := c.HandlePacket(packet.NewPacket(packet.Plain, packet.Notification, []byte(raw))); err != nil {
			t.Fatalf("%s: %v", entry.cmd, err)
		}
	}
	toast := `{"cmd":"USER_TOAST_MSG_V2","data":{"sender_uinfo":{"uid":42,"base":{"name":"user"}},"guard_info":{"guard_level":3,"role_name":"舰长","start_time":1,"end_time":2},"pay_info":{"num":1,"price":138000,"unit":"月","payflow_id":"order"},"gift_info":{"gift_id":10003},"option":{"source":0}}}`
	if err := c.HandlePacket(packet.NewPacket(packet.Plain, packet.Notification, []byte(toast))); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gifts, []int{4, 6}) || interactions != 1 || ranks != 1 || toasts != 1 || raws != 4 {
		t.Fatalf("dispatch: gifts=%v interactions=%d ranks=%d toasts=%d raws=%d", gifts, interactions, ranks, toasts, raws)
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}
