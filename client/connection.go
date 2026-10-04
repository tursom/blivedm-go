package client

import (
	"context"
	"errors"
	"fmt"
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
func (c *Client) connect(r *clientRun, info connectionInfo, options Options, attempt *int) (*websocket.Conn, error) {
	if len(info.hosts) == 0 {
		return nil, errors.New("empty danmaku server list")
	}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = options.HandshakeTimeout
	headers := http.Header{"User-Agent": {"Mozilla/5.0"}, "Origin": {"https://live.bilibili.com"}}
	for {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		host, err := endpoint(info.hosts[*attempt%len(info.hosts)])
		if err != nil {
			return nil, err
		}
		(*attempt)++
		conn, response, err := dialer.DialContext(r.ctx, host, headers)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
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
		log.WithError(err).Debug("danmaku connection failed; retrying")
		if err := waitRetry(r.ctx, options.ReconnectInterval); err != nil {
			return nil, err
		}
	}
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
func (c *Client) runLoop(r *clientRun, conn *websocket.Conn, info connectionInfo, options Options, attempt int) {
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
		err := c.readSession(r, conn, options, events)
		r.closeConn()
		if r.ctx.Err() != nil {
			return
		}
		log.WithError(err).Debug("danmaku session ended; reconnecting")
		if waitRetry(r.ctx, options.ReconnectInterval) != nil {
			return
		}
		conn, err = c.connect(r, info, options, &attempt)
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
