package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tursom/blivedm-go/message"
	"github.com/tursom/blivedm-go/packet"
)

// These tests replace process-wide transports, like mockRoomAPI. Do not use
// t.Parallel: all clients must stop before the transports are restored.
func mockReconnectAPI(t *testing.T, danmu roundTripFunc) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/room/v1/Room/room_init":
			return reconnectJSON(`{"code":0,"data":{"room_id":123}}`), nil
		case "/x/web-interface/nav":
			// WBI keys may already be cached by an earlier test.
			return reconnectJSON(`{"code":0,"data":{"wbi_img":{"img_url":"https://example.invalid/0123456789abcdef0123456789abcdef.png","sub_url":"https://example.invalid/fedcba9876543210fedcba9876543210.png"}}}`), nil
		case "/xlive/web-room/v1/index/getDanmuInfo":
			if r.URL.Query().Get("id") != "123" {
				return nil, fmt.Errorf("token requested for unresolved room: %s", r.URL)
			}
			return danmu(r)
		default:
			return nil, fmt.Errorf("unexpected API request: %s", r.URL)
		}
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func reconnectJSON(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func reconnectDanmuInfo(server *httptest.Server, token string) *http.Response {
	addr := server.Listener.Addr().(*net.TCPAddr)
	return reconnectJSON(fmt.Sprintf(`{"code":0,"data":{"token":%q,"host_list":[{"host":%q,"wss_port":%d}]}}`, token, addr.IP.String(), addr.Port))
}

func reconnectTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	var handlers sync.WaitGroup
	var sockets sync.Map
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		handler.ServeHTTP(w, r)
	}))
	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateNew {
			sockets.Store(conn, struct{}{})
		} else if state == http.StateClosed {
			sockets.Delete(conn)
		}
	}
	server.StartTLS()
	previous := websocket.DefaultDialer
	dialer := *previous
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	dialer.TLSClientConfig = &tls.Config{RootCAs: pool}
	dialer.Proxy = nil
	dialer.NetDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			return nil, fmt.Errorf("test forbids non-loopback dial: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	websocket.DefaultDialer = &dialer
	t.Cleanup(func() {
		// httptest.Close does not close hijacked WebSocket connections.
		sockets.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		server.Close()
		handlers.Wait()
		websocket.DefaultDialer = previous
	})
	return server
}

func reconnectSocketHandler(t *testing.T, session func(*websocket.Conn, packet.Enter)) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, wire, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read enter packet: %v", err)
			return
		}
		p, err := packet.Decode(wire)
		if err != nil || p.Operation != packet.RoomEnter {
			t.Errorf("expected enter packet: operation=%d, error=%v", p.Operation, err)
			return
		}
		var enter packet.Enter
		if err := json.Unmarshal(p.Body, &enter); err != nil {
			t.Errorf("decode enter packet: %v", err)
			return
		}
		session(conn, enter)
	})
}

// Count dial attempts, including sockets that never reach HTTP upgrade or auth.
// Install after reconnectTLSServer so its loopback-only transport is retained.
func trackReconnectDials(t *testing.T) *atomic.Int32 {
	t.Helper()
	var count atomic.Int32
	previous := websocket.DefaultDialer
	dialer := *previous
	dialer.NetDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		count.Add(1)
		return previous.NetDialContext(ctx, network, address)
	}
	websocket.DefaultDialer = &dialer
	t.Cleanup(func() { websocket.DefaultDialer = previous })
	return &count
}

func newReconnectAPIClient(t *testing.T, interval time.Duration) *Client {
	t.Helper()
	c, err := NewClientWithOptions(1, Options{
		ReconnectInterval: interval, HandshakeTimeout: 5 * time.Second,
		JoinTimeout: time.Second, ReadIdleTimeout: 5 * time.Second,
		HeartbeatInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately do not call SetHost: exercise API token/host discovery.
	t.Cleanup(func() { c.Stop(); waitDone(t, c.Done()) })
	return c
}

func reconnectReceive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func stopReconnectClient(t *testing.T, c *Client) {
	t.Helper()
	c.Stop()
	select {
	case <-c.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop did not finish within 500ms")
	}
}

func TestReconnectRefreshesAPITokenAfterShortSessions(t *testing.T) {
	for _, mode := range []string{"authentication_rejected", "immediate_disconnect", "accepted_then_disconnected"} {
		t.Run(mode, func(t *testing.T) {
			var requests, sessions atomic.Int32
			enters := make(chan packet.Enter, 16)
			delivered := make(chan string, 1)
			server := reconnectTLSServer(t, reconnectSocketHandler(t, func(conn *websocket.Conn, enter packet.Enter) {
				enters <- enter
				if sessions.Add(1) <= 3 {
					switch mode {
					case "authentication_rejected":
						_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":-101}`)
						// Keep the socket open: rejection itself must trigger retry.
						for {
							if _, _, err := conn.ReadMessage(); err != nil {
								return
							}
						}
					case "accepted_then_disconnected":
						_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":0}`)
					}
					return
				}
				_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":0}`)
				_ = sendPacket(conn, packet.Notification, `{"cmd":"RECOVERED","data":"ok"}`)
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}))
			mockReconnectAPI(t, func(*http.Request) (*http.Response, error) {
				return reconnectDanmuInfo(server, fmt.Sprintf("token-%d", requests.Add(1))), nil
			})
			c := newReconnectAPIClient(t, 20*time.Millisecond)
			c.RegisterCustomEventHandler("RECOVERED", func(raw string) { delivered <- raw })
			if err := c.StartContext(testContext(t)); err != nil {
				t.Fatal(err)
			}
			if got := reconnectReceive(t, delivered, "recovered event"); got != `{"cmd":"RECOVERED","data":"ok"}` {
				t.Fatalf("event = %s", got)
			}
			stopReconnectClient(t, c)
			for i := 1; i <= 4; i++ {
				enter := reconnectReceive(t, enters, "enter packet")
				if enter.Key != fmt.Sprintf("token-%d", i) || enter.RoomID != 123 {
					t.Errorf("session %d: token=%q room=%d", i, enter.Key, enter.RoomID)
				}
			}
			if requests.Load() != 4 || sessions.Load() != 4 {
				t.Fatalf("API requests=%d sessions=%d, want 4 each", requests.Load(), sessions.Load())
			}
		})
	}
}

func TestReconnectAPIFailureDoesNotDialStaleTokenAndRecovers(t *testing.T) {
	var requests, oldSessions, newSessions atomic.Int32
	recoveryRequested := make(chan struct{}, 1)
	recoverAPI := make(chan struct{})
	delivered := make(chan string, 1)
	enters := make(chan packet.Enter, 16)
	oldServer := reconnectTLSServer(t, reconnectSocketHandler(t, func(conn *websocket.Conn, enter packet.Enter) {
		oldSessions.Add(1)
		enters <- enter
		_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":-101}`)
	}))
	newServer := reconnectTLSServer(t, reconnectSocketHandler(t, func(conn *websocket.Conn, enter packet.Enter) {
		newSessions.Add(1)
		enters <- enter
		_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":0}`)
		_ = sendPacket(conn, packet.Notification, `{"cmd":"RECOVERED"}`)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	dials := trackReconnectDials(t)
	mockReconnectAPI(t, func(r *http.Request) (*http.Response, error) {
		switch n := requests.Add(1); n {
		case 1:
			return reconnectDanmuInfo(oldServer, "token-1"), nil
		case 2:
			return nil, errors.New("mock token API transport failure")
		case 3:
			response := reconnectJSON(`{"code":0}`)
			response.StatusCode, response.Status = http.StatusServiceUnavailable, "503 Service Unavailable"
			return response, nil
		case 4:
			return reconnectJSON(`{"code":-500,"message":"mock token API failure","data":[]}`), nil
		default:
			recoveryRequested <- struct{}{}
			select {
			case <-recoverAPI:
				return reconnectDanmuInfo(newServer, "token-"+strconv.Itoa(int(n))), nil
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
	})
	c := newReconnectAPIClient(t, 20*time.Millisecond)
	c.RegisterCustomEventHandler("RECOVERED", func(raw string) { delivered <- raw })
	if err := c.StartContext(testContext(t)); err != nil {
		t.Fatal(err)
	}
	reconnectReceive(t, recoveryRequested, "API retry after three failures")
	if dials.Load() != 1 || oldSessions.Load() != 1 || newSessions.Load() != 0 {
		t.Errorf("dialed while token API failed: dials=%d old=%d new=%d", dials.Load(), oldSessions.Load(), newSessions.Load())
	}
	close(recoverAPI)
	reconnectReceive(t, delivered, "event from refreshed host")
	stopReconnectClient(t, c)
	if requests.Load() != 5 || dials.Load() != 2 || oldSessions.Load() != 1 || newSessions.Load() != 1 {
		t.Fatalf("requests=%d dials=%d old sessions=%d new sessions=%d", requests.Load(), dials.Load(), oldSessions.Load(), newSessions.Load())
	}
	for _, want := range []string{"token-1", "token-5"} {
		if got := reconnectReceive(t, enters, "enter token").Key; got != want {
			t.Errorf("enter token=%q, want %q", got, want)
		}
	}
}

func TestReconnectIntervalDoublesAcrossShortSessions(t *testing.T) {
	for _, mode := range []string{"upgrade_failure", "authentication_rejected", "accepted_then_disconnected"} {
		t.Run(mode, func(t *testing.T) {
			mockRoomAPI(t)
			attempts := make(chan time.Time, 32)
			var server *httptest.Server
			if mode == "upgrade_failure" {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attempts <- time.Now()
					http.Error(w, "offline", http.StatusServiceUnavailable)
				}))
				t.Cleanup(server.Close)
			} else {
				server = socketServer(t, func(conn *websocket.Conn) {
					attempts <- time.Now()
					if mode == "authentication_rejected" {
						_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":-101}`)
						for {
							if _, _, err := conn.ReadMessage(); err != nil {
								return
							}
						}
					}
					_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":0}`)
				})
			}
			c := newTestClient(t, socketURL(server))
			const base = 40 * time.Millisecond
			c.options.ReconnectInterval = base
			started := make(chan error, 1)
			go func() { started <- c.Start() }()
			previous := reconnectReceive(t, attempts, "first connection")
			for i := 0; i < 3; i++ {
				now := reconnectReceive(t, attempts, "retry connection")
				elapsed, want := now.Sub(previous), base*time.Duration(1<<i)
				t.Logf("retry %d: interval=%s, configured delay=%s", i+1, elapsed, want)
				// Allow scheduler/network overhead without accepting a constant
				// delay or a reset after a successful but short authentication.
				if elapsed < want-5*time.Millisecond || elapsed > want+60*time.Millisecond {
					t.Errorf("retry %d interval=%s, want [%s, %s]", i+1, elapsed, want-5*time.Millisecond, want+60*time.Millisecond)
				}
				previous = now
			}
			stopReconnectClient(t, c)
			err := reconnectReceive(t, started, "Start return")
			if mode == "upgrade_failure" {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("Start error=%v, want canceled", err)
				}
			} else if err != nil {
				t.Error(err)
			}
		})
	}
}

func TestReconnectDelayCap(t *testing.T) {
	for _, tc := range []struct {
		name                string
		base, current, want time.Duration
	}{
		{"small_base", 40 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond},
		{"below_cap", 3 * time.Second, 8 * time.Second, 16 * time.Second},
		{"crosses_cap", 3 * time.Second, 16 * time.Second, 30 * time.Second},
		{"at_cap", 3 * time.Second, 30 * time.Second, 30 * time.Second},
		{"large_base_below_cap", 20 * time.Second, 20 * time.Second, 30 * time.Second},
		{"base_equals_cap", 30 * time.Second, 30 * time.Second, 30 * time.Second},
		{"base_above_cap", 45 * time.Second, 45 * time.Second, 45 * time.Second},
		{"no_duration_overflow", time.Duration(1<<63 - 1), time.Duration(1<<63 - 1), time.Duration(1<<63 - 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextReconnectDelay(tc.current, tc.base); got != tc.want {
				t.Errorf("next delay=%s, want %s (base=%s, current=%s)", got, tc.want, tc.base, tc.current)
			}
		})
	}
}

func TestStopInterruptsUnansweredWebSocketUpgrade(t *testing.T) {
	for _, phase := range []string{"initial_connection", "reconnect"} {
		t.Run(phase, func(t *testing.T) {
			var upgrades, requests atomic.Int32
			entered, socketClosed := make(chan struct{}, 4), make(chan struct{}, 4)
			release := make(chan struct{})
			firstSession := reconnectSocketHandler(t, func(conn *websocket.Conn, _ packet.Enter) {
				_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":-101}`)
			})
			server := reconnectTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if upgrades.Add(1) == 1 && phase == "reconnect" {
					firstSession.ServeHTTP(w, r)
					return
				}
				entered <- struct{}{}
				// TCP and TLS succeeded, but no HTTP upgrade response is sent.
				select {
				case <-r.Context().Done():
					socketClosed <- struct{}{}
				case <-release:
					http.Error(w, "test cleanup", http.StatusServiceUnavailable)
				}
			}))
			mockReconnectAPI(t, func(*http.Request) (*http.Response, error) {
				return reconnectDanmuInfo(server, fmt.Sprintf("token-%d", requests.Add(1))), nil
			})
			dials := trackReconnectDials(t)
			c := newReconnectAPIClient(t, 20*time.Millisecond)
			// Unblock the server even if the cancellation assertion fails.
			t.Cleanup(func() { close(release); server.CloseClientConnections() })
			started := make(chan error, 1)
			go func() { started <- c.Start() }()
			reconnectReceive(t, entered, "blocked HTTP upgrade")
			stopReconnectClient(t, c)
			reconnectReceive(t, socketClosed, "underlying socket closure")
			err := reconnectReceive(t, started, "Start return")
			if phase == "initial_connection" {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("Start error=%v, want canceled", err)
				}
			} else if err != nil {
				t.Error(err)
			}
			want := int32(1)
			if phase == "reconnect" {
				want = 2
			}
			time.Sleep(100 * time.Millisecond)
			if dials.Load() != want || upgrades.Load() != want || requests.Load() != want {
				t.Errorf("activity after Stop: dials=%d upgrades=%d API requests=%d, want %d", dials.Load(), upgrades.Load(), requests.Load(), want)
			}
		})
	}
}

func TestStopInterruptsReconnectTokenRequest(t *testing.T) {
	var requests, sessions atomic.Int32
	entered := make(chan struct{}, 1)
	canceled := make(chan error, 1)
	server := reconnectTLSServer(t, reconnectSocketHandler(t, func(conn *websocket.Conn, _ packet.Enter) {
		sessions.Add(1)
		_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":-101}`)
	}))
	dials := trackReconnectDials(t)
	mockReconnectAPI(t, func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return reconnectDanmuInfo(server, "token-1"), nil
		}
		entered <- struct{}{}
		<-r.Context().Done()
		canceled <- r.Context().Err()
		return nil, r.Context().Err()
	})
	c := newReconnectAPIClient(t, 20*time.Millisecond)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	reconnectReceive(t, entered, "blocked reconnect token request")
	stopReconnectClient(t, c)
	if err := reconnectReceive(t, canceled, "API context cancellation"); !errors.Is(err, context.Canceled) {
		t.Errorf("token request error=%v, want canceled", err)
	}
	time.Sleep(100 * time.Millisecond)
	if requests.Load() != 2 || dials.Load() != 1 || sessions.Load() != 1 {
		t.Errorf("activity after Stop: API requests=%d dials=%d sessions=%d", requests.Load(), dials.Load(), sessions.Load())
	}
}

func TestStopInterruptsReconnectWaitAfterAPIFailure(t *testing.T) {
	var requests, sessions atomic.Int32
	failed := make(chan struct{}, 4)
	server := reconnectTLSServer(t, reconnectSocketHandler(t, func(conn *websocket.Conn, _ packet.Enter) {
		sessions.Add(1)
		_ = sendPacket(conn, packet.RoomEnterResponse, `{"code":-101}`)
	}))
	dials := trackReconnectDials(t)
	mockReconnectAPI(t, func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return reconnectDanmuInfo(server, "token-1"), nil
		}
		failed <- struct{}{}
		return nil, errors.New("mock token API unavailable")
	})
	// The second wait is 800ms, longer than stopReconnectClient's 500ms
	// deadline, so an uncancellable sleep cannot pass by finishing naturally.
	const base = 400 * time.Millisecond
	c := newReconnectAPIClient(t, base)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	reconnectReceive(t, failed, "failed reconnect token request")
	time.Sleep(20 * time.Millisecond)
	stopReconnectClient(t, c)
	// Observe beyond the pending retry deadline, not just the Stop call.
	time.Sleep(2*base + 100*time.Millisecond)
	if requests.Load() != 2 || dials.Load() != 1 || sessions.Load() != 1 {
		t.Errorf("activity after Stop: API requests=%d dials=%d sessions=%d", requests.Load(), dials.Load(), sessions.Load())
	}
}

func TestRegisterFullEventHandlerReplacementAndDispatch(t *testing.T) {
	c := NewClient(1)
	const raw = `{"cmd" : "ONLINE_RANK_COUNT:extra", "data":{"count":12,"count_text":"12","online_count":12,"online_count_text":"12"}}`
	p := packet.NewPacket(packet.Plain, packet.Notification, []byte(raw))
	var first, replacement, rawEvents, custom []string
	var typed int
	c.OnRawEvent(func(cmd string, data []byte) {
		if cmd != "ONLINE_RANK_COUNT:extra" {
			t.Errorf("raw cmd=%q", cmd)
		}
		rawEvents = append(rawEvents, string(data))
	})
	c.OnOnlineRankCount(func(*message.OnlineRankCount) { typed++ })
	c.RegisterCustomEventHandler("ONLINE_RANK_COUNT", func(data string) { custom = append(custom, data) })
	firstHandler := func(data string) { first = append(first, data) }
	secondHandler := func(data string) { replacement = append(replacement, data) }
	for _, handler := range []func(string){firstHandler, secondHandler, nil} {
		c.RegisterFullEventHandler(handler)
		if err := c.HandlePacket(p); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(first, []string{raw}) || !reflect.DeepEqual(replacement, []string{raw}) {
		t.Fatalf("full handler replacement/clear: first=%v replacement=%v", first, replacement)
	}
	if !reflect.DeepEqual(rawEvents, []string{raw, raw, raw}) || !reflect.DeepEqual(custom, rawEvents) || typed != 0 {
		t.Fatalf("dispatch with custom override: raw=%v custom=%v typed=%d", rawEvents, custom, typed)
	}
	c.RegisterCustomEventHandler("ONLINE_RANK_COUNT", nil)
	c.RegisterFullEventHandler(secondHandler)
	if err := c.HandlePacket(p); err != nil {
		t.Fatal(err)
	}
	if typed != 1 || len(rawEvents) != 4 || len(replacement) != 2 || len(custom) != 3 {
		t.Fatalf("dispatch without custom override: raw=%d full=%d custom=%d typed=%d", len(rawEvents), len(replacement), len(custom), typed)
	}
}

func TestRegisterFullEventHandlerConcurrentReplacement(t *testing.T) {
	c := NewClient(1)
	var raws atomic.Int32
	c.OnRawEvent(func(string, []byte) { raws.Add(1) })
	p := packet.NewPacket(packet.Plain, packet.Notification, []byte(`{"cmd":"UNKNOWN"}`))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			c.RegisterFullEventHandler(func(string) {})
			c.RegisterFullEventHandler(nil)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			c.Handle(p)
		}
	}()
	wg.Wait()
	if raws.Load() != 1000 {
		t.Fatalf("raw handler received %d events, want 1000", raws.Load())
	}
}
