package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tursom/blivedm-go/api"
	"github.com/tursom/blivedm-go/packet"
)

func waitStatus(t *testing.T, c *Client, state string) ConnectionStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s := c.Status()
		if s.State == state {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("status=%+v, want %s", s, state)
		}
		time.Sleep(time.Millisecond)
	}
}

type statusPeer struct {
	conn      *websocket.Conn
	enteredAt time.Time
	pongs     chan struct{}
}

// The test writes packets while the server handler continuously reads client
// heartbeats and pongs. A ping/pong roundtrip fences all preceding test frames.
func statusSocketServer(t *testing.T) (*httptest.Server, <-chan statusPeer) {
	t.Helper()
	peers := make(chan statusPeer, 64)
	server := socketServer(t, func(conn *websocket.Conn) {
		pongs := make(chan struct{}, 16)
		conn.SetPongHandler(func(string) error { pongs <- struct{}{}; return nil })
		peers <- statusPeer{conn: conn, enteredAt: time.Now(), pongs: pongs}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	return server, peers
}

func statusFence(t *testing.T, peer statusPeer) {
	t.Helper()
	if err := peer.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	reconnectReceive(t, peer.pongs, "protocol read fence")
}

func TestConnectionStatusProtocolConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   uint32
		body string
	}{
		{"authentication", packet.RoomEnterResponse, `{"code":0}`},
		{"heartbeat", packet.HeartBeatResponse, "\x00\x00\x00\x01"},
		{"notification", packet.Notification, `{"cmd":"STATUS_TEST"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockRoomAPI(t)
			server, peers := statusSocketServer(t)
			c := newTestClient(t, socketURL(server))
			initial := c.Status()
			if initial.State != "idle" || initial.UpdatedAt.IsZero() || initial.Host != "" || initial.Error != "" || !initial.RetryAt.IsZero() {
				t.Fatalf("initial snapshot=%+v", initial)
			}
			if c.Status() != initial {
				t.Fatal("reading a snapshot changed it")
			}
			// Mutating a returned copy must not affect the client.
			copy := c.Status()
			copy.State, copy.Error = "changed", "changed"
			if c.Status() != initial {
				t.Fatal("snapshot aliases client state")
			}
			fromCallback := make(chan ConnectionStatus, 1)
			c.RegisterCustomEventHandler("STATUS_TEST", func(string) { fromCallback <- c.Status() })
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			peer := reconnectReceive(t, peers, "enter packet")
			authenticating := c.Status()
			if authenticating.State != "authenticating" || authenticating.Host != socketURL(server) || authenticating.Error != "" || !authenticating.RetryAt.IsZero() || !authenticating.UpdatedAt.After(initial.UpdatedAt) {
				t.Fatalf("after enter=%+v", authenticating)
			}
			if err := c.Start(); !errors.Is(err, ErrAlreadyStarted) || c.Status() != authenticating {
				t.Fatalf("duplicate Start disturbed the run: err=%v status=%+v", err, c.Status())
			}
			// Invalid protocol data and unrelated WebSocket traffic cannot
			// confirm authentication, even after the read loop consumes it.
			for _, invalid := range []struct {
				op   uint32
				body string
			}{
				{packet.RoomEnterResponse, `{"code":"0"}`},
				{packet.RoomEnterResponse, `{"code":0.5}`},
				{packet.RoomEnterResponse, `{"message":"missing code"}`},
				{packet.RoomEnterResponse, `{"code":0`},
				{packet.HeartBeatResponse, "\x00\x00\x00"},
				{packet.Notification, `{"cmd":42}`},
				{packet.Notification, `{"cmd":""}`},
				{packet.Notification, `{"cmd":"TEST"`},
			} {
				if err := sendPacket(peer.conn, invalid.op, invalid.body); err != nil {
					t.Fatal(err)
				}
			}
			if err := peer.conn.WriteMessage(websocket.BinaryMessage, []byte("invalid frame")); err != nil {
				t.Fatal(err)
			}
			if err := peer.conn.WriteMessage(websocket.TextMessage, []byte(`{"code":0}`)); err != nil {
				t.Fatal(err)
			}
			statusFence(t, peer)
			if c.Status() != authenticating {
				t.Fatalf("invalid traffic changed status: %+v", c.Status())
			}
			if err := sendPacket(peer.conn, tc.op, tc.body); err != nil {
				t.Fatal(err)
			}
			connected := waitStatus(t, c, "connected")
			if !connected.UpdatedAt.After(authenticating.UpdatedAt) || connected.Host != authenticating.Host || connected.Error != "" || !connected.RetryAt.IsZero() {
				t.Fatalf("connected snapshot=%+v", connected)
			}
			if tc.name == "notification" {
				if got := reconnectReceive(t, fromCallback, "callback snapshot"); got.State != "connected" {
					t.Fatalf("callback saw %+v", got)
				}
			}
			if err := sendPacket(peer.conn, packet.HeartBeatResponse, "\x00\x00\x00\x02"); err != nil {
				t.Fatal(err)
			}
			statusFence(t, peer)
			if c.Status() != connected {
				t.Fatal("heartbeat changed an unchanged snapshot")
			}
			c.Stop()
			stopped := c.Status()
			if stopped.State != "stopped" || stopped.Host != connected.Host || stopped.Error != "" || !stopped.RetryAt.IsZero() {
				t.Fatalf("Stop snapshot=%+v", stopped)
			}
			waitDone(t, c.Done())
			c.Stop()
			if c.Status() != stopped {
				t.Fatal("finish or repeated Stop changed stopped snapshot")
			}
		})
	}
}

func TestConnectionStatusRetryAndHostRotation(t *testing.T) {
	mockRoomAPI(t)
	first, firstPeers := statusSocketServer(t)
	second, secondPeers := statusSocketServer(t)
	c := newTestClient(t, socketURL(first))
	c.hostList = []string{socketURL(first) + "?token=private-token", socketURL(second)}
	c.options.ReconnectInterval = 250 * time.Millisecond
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	peer := reconnectReceive(t, firstPeers, "first enter")
	if err := sendPacket(peer.conn, packet.RoomEnterResponse, `{"code":-101,"message":"Cookie: private-cookie; token=private-token"}`); err != nil {
		t.Fatal(err)
	}
	retrying := waitStatus(t, c, "retrying")
	if retrying.Error != "room authentication rejected: code=-101" || retrying.Host != socketURL(first) || !retrying.RetryAt.After(time.Now()) {
		t.Fatalf("authentication rejection=%+v", retrying)
	}
	if delta := retrying.RetryAt.Sub(retrying.UpdatedAt); delta < 200*time.Millisecond || delta > c.options.ReconnectInterval {
		t.Fatalf("retry deadline does not match configured wait: %s", delta)
	}
	peer = reconnectReceive(t, secondPeers, "rotated host enter")
	if peer.enteredAt.Before(retrying.RetryAt) || peer.enteredAt.Sub(retrying.RetryAt) > time.Second {
		t.Fatalf("actual retry=%v, advertised=%v", peer.enteredAt, retrying.RetryAt)
	}
	authenticating := waitStatus(t, c, "authenticating")
	if authenticating.Host != socketURL(second) || authenticating.Error != "" || !authenticating.RetryAt.IsZero() {
		t.Fatalf("rotated host snapshot=%+v", authenticating)
	}
	if err := sendPacket(peer.conn, packet.RoomEnterResponse, `{"code":0}`); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, c, "connected")
	if err := peer.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1008, "Cookie: private-cookie; token=private-token"), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	retrying = waitStatus(t, c, "retrying")
	if retrying.Error != "WebSocket closed: code=1008" || retrying.Host != socketURL(second) || !retrying.RetryAt.After(time.Now()) {
		t.Fatalf("disconnect snapshot=%+v", retrying)
	}
	// A short successful authentication must not reset the existing backoff.
	if delta := retrying.RetryAt.Sub(retrying.UpdatedAt); delta < 450*time.Millisecond || delta > 500*time.Millisecond {
		t.Fatalf("short session reset backoff: %s", delta)
	}
	c.Stop()
	if s := c.Status(); s.State != "stopped" || s.Error != "" || !s.RetryAt.IsZero() {
		t.Fatalf("stopped retry=%+v", s)
	}
}

func TestConnectionStatusTimeout(t *testing.T) {
	for _, phase := range []string{"join", "read_idle"} {
		t.Run(phase, func(t *testing.T) {
			mockRoomAPI(t)
			server, peers := statusSocketServer(t)
			c := newTestClient(t, socketURL(server))
			c.options.JoinTimeout = 100 * time.Millisecond
			c.options.ReadIdleTimeout = 100 * time.Millisecond
			c.options.ReconnectInterval = time.Second
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			peer := reconnectReceive(t, peers, "enter")
			if phase == "read_idle" {
				if err := sendPacket(peer.conn, packet.RoomEnterResponse, `{"code":0}`); err != nil {
					t.Fatal(err)
				}
				waitStatus(t, c, "connected")
			}
			s := waitStatus(t, c, "retrying")
			if s.Error != "connection timed out" || s.Host != socketURL(server) || !s.RetryAt.After(time.Now()) {
				t.Fatalf("timeout snapshot=%+v", s)
			}
		})
	}
}

func TestConnectionStatusConnectingAndCancellation(t *testing.T) {
	for _, phase := range []string{"initialization", "dial"} {
		t.Run(phase, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			host := ""
			if phase == "initialization" {
				previous := http.DefaultTransport
				http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					entered <- struct{}{}
					<-r.Context().Done()
					return nil, r.Context().Err()
				})
				t.Cleanup(func() { http.DefaultTransport = previous })
			} else {
				mockRoomAPI(t)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					entered <- struct{}{}
					<-r.Context().Done()
				}))
				t.Cleanup(server.Close)
				host = socketURL(server)
			}
			c := newTestClient(t, host)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan error, 1)
			go func() { started <- c.StartContext(ctx) }()
			reconnectReceive(t, entered, "blocked "+phase)
			s := c.Status()
			if s.State != "connecting" || s.Host != host || s.Error != "" || !s.RetryAt.IsZero() {
				t.Fatalf("%s snapshot=%+v", phase, s)
			}
			if phase == "initialization" {
				cancel()
			} else {
				c.Stop()
				if c.Status().State != "stopped" {
					t.Fatal("Stop did not synchronously publish stopped")
				}
			}
			if err := reconnectReceive(t, started, "canceled Start"); !errors.Is(err, context.Canceled) {
				t.Fatalf("Start returned %v", err)
			}
			waitDone(t, c.Done())
			if s := c.Status(); s.State != "stopped" || s.Error != "" || !s.RetryAt.IsZero() {
				t.Fatalf("canceled snapshot=%+v", s)
			}
		})
	}
}

func TestConnectionStatusDialFailure(t *testing.T) {
	mockRoomAPI(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Cookie: private-cookie; token=private-token", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	c := newTestClient(t, socketURL(server)+"?token=private-token")
	c.options.ReconnectInterval = time.Second
	started := make(chan error, 1)
	go func() { started <- c.Start() }()
	s := waitStatus(t, c, "retrying")
	if s.Error != "WebSocket handshake failed" || s.Host != socketURL(server) || !s.RetryAt.After(time.Now()) {
		t.Fatalf("dial failure snapshot=%+v", s)
	}
	c.Stop()
	if err := reconnectReceive(t, started, "canceled dial retry"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start returned %v", err)
	}
}

func TestConnectionStatusInitializationErrorsAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		transportError   error
	}{
		{"business", `{"code":-101,"message":"Cookie: private-cookie; token=private-token"}`, "bilibili API: code=-101", nil},
		{"transport", "", "connection failed", errors.New("Cookie: private-cookie; token=private-token")},
		{"http", "", "bilibili HTTP: status=503", &api.HTTPError{StatusCode: 503, Status: "private-cookie private-token"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := http.DefaultTransport
			var requests int
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					if tc.transportError != nil {
						return nil, tc.transportError
					}
					return reconnectJSON(tc.body), nil
				}
				return reconnectJSON(`{"code":0,"data":{"room_id":123}}`), nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			server, peers := statusSocketServer(t)
			c := newTestClient(t, socketURL(server))
			c.SetCookie("DedeUserID=42; SESSDATA=private-cookie; buvid3=private-buvid")
			if err := c.Start(); err == nil {
				t.Fatal("initialization unexpectedly succeeded")
			}
			waitDone(t, c.Done())
			failed := c.Status()
			if failed.State != "error" || failed.Error != tc.want || failed.Host != "" || !failed.RetryAt.IsZero() {
				t.Fatalf("initialization failure=%+v", failed)
			}
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			peer := reconnectReceive(t, peers, "recovered enter")
			if err := sendPacket(peer.conn, packet.RoomEnterResponse, `{"code":0}`); err != nil {
				t.Fatal(err)
			}
			if s := waitStatus(t, c, "connected"); s.Error != "" || !s.UpdatedAt.After(failed.UpdatedAt) {
				t.Fatalf("recovered snapshot=%+v", s)
			}
		})
	}
}

func TestConnectionStatusInvalidEndpoint(t *testing.T) {
	mockRoomAPI(t)
	c := newTestClient(t, "https://user:private-cookie@localhost/sub?token=private-token")
	if err := c.Start(); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	if s := c.Status(); s.State != "error" || s.Error != "invalid WebSocket endpoint" || s.Host != "" || !s.RetryAt.IsZero() {
		t.Fatalf("invalid endpoint snapshot=%+v", s)
	}
}

func TestConnectionStatusTerminalReconnectError(t *testing.T) {
	server := reconnectTLSServer(t, reconnectSocketHandler(t, func(conn *websocket.Conn, _ packet.Enter) {
		_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":-101}`)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	var requests atomic.Int32
	mockReconnectAPI(t, func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return reconnectDanmuInfo(server, "private-token"), nil
		}
		// A malformed refreshed endpoint is a terminal connect error, unlike
		// transient API/transport failures. Exercise the background finish path.
		return reconnectJSON(`{"code":0,"data":{"token":"private-token","host_list":[{"host":"%private-token","wss_port":443}]}}`), nil
	})
	c := newReconnectAPIClient(t, 10*time.Millisecond)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	waitDone(t, c.Done())
	s := c.Status()
	wantHost := "wss://" + server.Listener.Addr().String() + "/sub"
	if requests.Load() != 2 || s.State != "error" || s.Error != "invalid WebSocket endpoint" || s.Host != wantHost || !s.RetryAt.IsZero() {
		t.Fatalf("terminal reconnect snapshot=%+v, requests=%d", s, requests.Load())
	}
}

func TestConnectionStatusConcurrentStopRestart(t *testing.T) {
	mockRoomAPI(t)
	server, peers := statusSocketServer(t)
	c := newTestClient(t, socketURL(server))
	readersDone := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-readersDone:
					return
				default:
					s := c.Status()
					if s.UpdatedAt.IsZero() || (s.State != "retrying" && !s.RetryAt.IsZero()) || ((s.State == "connected" || s.State == "authenticating") && (s.Host == "" || s.Error != "")) {
						t.Errorf("inconsistent concurrent snapshot=%+v", s)
						return
					}
				}
			}
		}()
	}
	defer func() { close(readersDone); readers.Wait() }()
	var oldRun *clientRun
	for i := 0; i < 20; i++ {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		peer := reconnectReceive(t, peers, "restart enter")
		if err := sendPacket(peer.conn, packet.RoomEnterResponse, `{"code":0}`); err != nil {
			t.Fatal(err)
		}
		connected := waitStatus(t, c, "connected")
		if oldRun != nil {
			// Force a late active update from the previous run while a real
			// new WebSocket is connected; neither state nor timestamp may move.
			c.setRunStatus(oldRun, "retrying", errors.New("old failure"), "ws://old.invalid/sub", time.Now().Add(time.Hour))
			if c.Status() != connected {
				t.Fatal("old run overwrote the restarted client")
			}
		}
		c.mu.Lock()
		oldRun = c.run
		c.mu.Unlock()
		var stops sync.WaitGroup
		for j := 0; j < 4; j++ {
			stops.Add(1)
			go func() { defer stops.Done(); c.Stop() }()
		}
		stops.Wait()
		stopped := c.Status()
		c.setRunStatus(oldRun, "connected", nil, socketURL(server), time.Time{})
		waitDone(t, c.Done())
		if stopped.State != "stopped" || c.Status() != stopped {
			t.Fatalf("canceled run revived: stopped=%+v now=%+v", stopped, c.Status())
		}
	}
}

func TestConnectionStatusJSONAndSafeSummaries(t *testing.T) {
	var c Client
	s := c.Status()
	if s.State != "idle" || s.UpdatedAt.IsZero() || c.Status() != s {
		t.Fatalf("zero-value snapshot=%+v", s)
	}
	wire, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || string(fields["state"]) != `"idle"` || string(fields["retryAt"]) != `"0001-01-01T00:00:00Z"` || fields["updatedAt"] == nil {
		t.Fatalf("JSON contract=%s", wire)
	}
	const secret = "private-cookie-and-token"
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&api.APIError{Code: -400, Message: secret}, "bilibili API: code=-400"},
		{&api.HTTPError{StatusCode: 502, Status: secret}, "bilibili HTTP: status=502"},
		{&websocket.CloseError{Code: 1008, Text: secret}, "WebSocket closed: code=1008"},
		{errors.New(secret + " code=-999"), "connection failed"},
	} {
		got := safeConnectionError(fmt.Errorf("outer %s: %w", secret, tc.err))
		if got != tc.want || strings.Contains(got, secret) {
			t.Errorf("safe error=%q, want %q", got, tc.want)
		}
	}
	if got := statusHost("wss://user:" + secret + "@example.test:443/sub?token=" + secret + "#" + secret); got != "wss://example.test:443/sub" {
		t.Fatalf("safe host=%q", got)
	}
}
