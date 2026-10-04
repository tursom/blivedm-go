package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tursom/blivedm-go/packet"
)

type notification struct {
	body []byte
	cmd  string
}

func endpoint(host string) (string, error) {
	if !strings.Contains(host, "://") {
		host = "wss://" + host + "/sub"
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" {
		return "", fmt.Errorf("invalid WebSocket endpoint %q", host)
	}
	if u.Path == "" {
		u.Path = "/sub"
	}
	return u.String(), nil
}
func (c *Client) connect(r *clientRun, info *connectionInfo, options Options, attempt *int, delay *time.Duration, refresh bool) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = options.HandshakeTimeout
	headers := http.Header{"User-Agent": {"Mozilla/5.0"}, "Origin": {"https://live.bilibili.com"}}
	for {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		var err error
		if refresh {
			*info, err = c.refreshDanmuInfo(r.ctx, *info)
		}
		if err == nil {
			if len(info.hosts) == 0 {
				return nil, errors.New("empty danmaku server list")
			}
			host, endpointErr := endpoint(info.hosts[*attempt%len(info.hosts)])
			if endpointErr != nil {
				return nil, endpointErr
			}
			(*attempt)++
			var conn *websocket.Conn
			conn, err = dialWebSocket(r.ctx, &dialer, host, headers)
			if err == nil {
				r.setConn(conn)
				conn.SetReadLimit(packet.DefaultMaxDecompressedSize)
				err = conn.SetWriteDeadline(time.Now().Add(options.WriteTimeout))
				if err == nil {
					err = conn.WriteMessage(websocket.BinaryMessage, packet.NewEnterPacket(info.uid, info.buvid, info.roomID, info.token))
				}
				if err == nil && r.ctx.Err() == nil {
					return conn, nil
				}
				r.closeConn()
			}
		}
		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		log.WithError(err).WithFields(log.Fields{"room": info.roomID, "retry_delay": delay.String(), "attempt": *attempt}).Warn("danmaku connection failed; retrying")
		if err := waitRetry(r.ctx, *delay); err != nil {
			return nil, err
		}
		*delay = nextReconnectDelay(*delay, options.ReconnectInterval)
		refresh = true
	}
}

// DialContext alone cannot cancel Gorilla's proxy CONNECT or HTTP upgrade
// reads. Track the socket from TCP connect until clientRun can take ownership.
func dialWebSocket(ctx context.Context, dialer *websocket.Dialer, host string, headers http.Header) (*websocket.Conn, error) {
	dialDone := make(chan struct{})
	defer close(dialDone)
	d := *dialer
	netDial := d.NetDialContext
	if netDial == nil {
		if legacyDial := d.NetDial; legacyDial != nil {
			netDial = func(_ context.Context, network, addr string) (net.Conn, error) { return legacyDial(network, addr) }
		} else {
			netDial = (&net.Dialer{}).DialContext
		}
	}
	d.NetDialContext = cancelableDial(ctx, dialDone, netDial)
	if d.NetDialTLSContext != nil {
		d.NetDialTLSContext = cancelableDial(ctx, dialDone, d.NetDialTLSContext)
	}
	conn, response, err := d.DialContext(ctx, host, headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if ctx.Err() != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, ctx.Err()
	}
	return conn, err
}

func cancelableDial(ctx context.Context, done <-chan struct{}, dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(dialCtx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(dialCtx, network, addr)
		if err != nil {
			return nil, err
		}
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.Close()
			case <-done:
			}
		}()
		return conn, nil
	}
}

func nextReconnectDelay(delay, initial time.Duration) time.Duration {
	limit := 30 * time.Second
	if initial > limit {
		limit = initial
	}
	if delay >= limit/2 {
		return limit
	}
	return delay * 2
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (c *Client) runLoop(r *clientRun, conn *websocket.Conn, info connectionInfo, options Options, attempt int, delay time.Duration) {
	defer c.finish(r)
	events := make(chan notification, options.EventBufferCapacity)
	dispatchDone := make(chan struct{})
	go func() {
		defer close(dispatchDone)
		for {
			select {
			case <-r.ctx.Done():
				return
			case p := <-events:
				if r.ctx.Err() != nil {
					return
				}
				if err := c.handleNotification(p.body, p.cmd); err != nil {
					log.WithError(err).Debug("invalid notification")
				}
			}
		}
	}()
	defer func() { r.cancel(); <-dispatchDone }()
	for {
		connectedAt := time.Now()
		err := c.readSession(r, conn, options, events)
		r.closeConn()
		if r.ctx.Err() != nil {
			return
		}
		if time.Since(connectedAt) >= 30*time.Second {
			delay = options.ReconnectInterval
		}
		log.WithError(err).WithFields(log.Fields{"room": info.roomID, "remote": conn.RemoteAddr().String(), "retry_delay": delay.String()}).Warn("danmaku session ended; reconnecting")
		if waitRetry(r.ctx, delay) != nil {
			return
		}
		delay = nextReconnectDelay(delay, options.ReconnectInterval)
		conn, err = c.connect(r, &info, options, &attempt, &delay, true)
		if err != nil {
			return
		}
	}
}
func (c *Client) readSession(r *clientRun, conn *websocket.Conn, options Options, events chan<- notification) error {
	ctx, cancel := context.WithCancel(r.ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(options.HeartbeatInterval)
		defer ticker.Stop()
		body := packet.NewHeartBeatPacket()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.SetWriteDeadline(time.Now().Add(options.WriteTimeout)); err != nil {
					_ = conn.Close()
					return
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, body); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	defer func() { cancel(); _ = conn.Close(); <-heartbeatDone }()
	if err := conn.SetReadDeadline(time.Now().Add(options.JoinTimeout)); err != nil {
		return err
	}
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if kind != websocket.BinaryMessage {
			continue
		}
		packets, err := packet.DecodeFrame(data)
		if err != nil {
			log.WithError(err).Debug("invalid danmaku packet")
			continue
		}
		active := false
		for _, p := range packets {
			switch p.Operation {
			case packet.RoomEnterResponse:
				if !gjson.ValidBytes(p.Body) {
					continue
				}
				code := gjson.GetBytes(p.Body, "code")
				if code.Type != gjson.Number {
					continue
				}
				if code.Int() != 0 {
					return fmt.Errorf("room authentication rejected: code=%d", code.Int())
				}
				active = true
			case packet.HeartBeatResponse:
				active = active || len(p.Body) >= 4
			case packet.Notification:
				cmd := parseCmd(p.Body)
				if cmd == "" {
					continue
				}
				active = true
				select {
				case events <- notification{body: p.Body, cmd: cmd}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		if active {
			if err := conn.SetReadDeadline(time.Now().Add(options.ReadIdleTimeout)); err != nil {
				return err
			}
		}
	}
}
