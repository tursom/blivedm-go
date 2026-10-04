package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/tursom/blivedm-go/api"
)

var ErrAlreadyStarted = errors.New("client is already started")

// Client 的公开配置字段应在 Start 前设置；SetCookie/SetHost 可并发调用，下次启动生效。
type Client struct {
	RoomID              int
	Uid                 int
	Buvid               string
	Cookie              string
	mu                  sync.Mutex
	hostList            []string
	options             Options
	run                 *clientRun
	done                <-chan struct{}
	handlerMu           sync.RWMutex
	eventHandlers       eventHandlers
	customEventHandlers customEventHandlers
	fullEventHandler    func(string)
}

type clientRun struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	conn   *websocket.Conn
}

func (r *clientRun) setConn(conn *websocket.Conn) {
	r.mu.Lock()
	r.conn = conn
	if conn != nil && r.ctx.Err() != nil {
		_ = conn.Close()
	}
	r.mu.Unlock()
}
func (r *clientRun) closeConn() {
	r.mu.Lock()
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
	}
	r.mu.Unlock()
}

// NewClient 创建可重复启动的客户端，默认自动重连。
func NewClient(roomID int) *Client {
	c, _ := NewClientWithOptions(roomID, Options{})
	return c
}

// NewClientWithOptions 创建使用指定超时和队列容量的客户端。
func NewClientWithOptions(roomID int, options Options) (*Client, error) {
	options, err := options.withDefaults()
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	close(done)
	return &Client{RoomID: roomID, options: options, done: done, customEventHandlers: make(customEventHandlers)}, nil
}
func (c *Client) SetCookie(cookie string) {
	c.mu.Lock()
	c.Cookie = cookie
	c.mu.Unlock()
}

// SetHost 设置服务器域名或完整 ws(s) URL，下次 Start 生效。
func (c *Client) SetHost(host string) {
	c.mu.Lock()
	c.hostList = []string{host}
	c.mu.Unlock()
}
func (c *Client) UseDefaultHost() { c.SetHost("broadcastlv.chat.bilibili.com") }

type connectionInfo struct {
	roomID int
	uid    int
	buvid  string
	cookie string
	token  string
	hosts  []string
}

func (c *Client) init(ctx context.Context, info connectionInfo) (connectionInfo, error) {
	if info.cookie != "" {
		if uid, ok := api.ExtractUIDFromCookie(info.cookie); ok {
			info.uid = uid
		} else {
			uid, err := api.GetUidContext(ctx, info.cookie)
			if err != nil {
				return info, fmt.Errorf("get account UID: %w", err)
			}
			info.uid = uid
		}
		if buvid, ok := api.ExtractBuvidFromCookie(info.cookie); ok {
			info.buvid = buvid
		}
	}
	room, err := api.GetRoomInfoContext(ctx, info.roomID)
	if err != nil {
		return info, err
	}
	if room.Code != 0 || room.Data.RoomId <= 0 {
		return info, fmt.Errorf("get room info: code=%d message=%s", room.Code, room.Message)
	}
	info.roomID = room.Data.RoomId
	if len(info.hosts) == 0 {
		danmu, err := api.GetDanmuInfoContext(ctx, info.roomID, info.cookie)
		if err != nil {
			return info, err
		}
		info.token = danmu.Data.Token
		for _, host := range danmu.Data.HostList {
			port := host.WssPort
			if port == 0 {
				port = 443
			}
			info.hosts = append(info.hosts, net.JoinHostPort(host.Host, strconv.Itoa(port)))
		}
	}
	if len(info.hosts) == 0 {
		info.hosts = []string{"broadcastlv.chat.bilibili.com"}
	}
	return info, nil
}

// Start 初始化并连接服务器。需要取消初始化/重连时使用 StartContext 或 Stop。
func (c *Client) Start() error { return c.StartContext(context.Background()) }

// StartContext 返回时已发送入房包；认证应答在后台处理。ctx 控制整个运行周期。
func (c *Client) StartContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	c.mu.Lock()
	if c.run != nil {
		c.mu.Unlock()
		return ErrAlreadyStarted
	}
	options, err := c.options.withDefaults()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &clientRun{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	c.run, c.done = r, r.done
	info := connectionInfo{roomID: c.RoomID, uid: c.Uid, buvid: c.Buvid, cookie: c.Cookie, hosts: append([]string(nil), c.hostList...)}
	c.mu.Unlock()
	go func() { <-ctx.Done(); r.closeConn() }()
	info, err = c.init(ctx, info)
	if err != nil {
		c.finish(r)
		return err
	}
	c.mu.Lock()
	c.RoomID, c.Uid, c.Buvid = info.roomID, info.uid, info.buvid
	c.mu.Unlock()
	attempt := 0
	conn, err := c.connect(r, info, options, &attempt)
	if err != nil {
		c.finish(r)
		return err
	}
	go c.runLoop(r, conn, info, options, attempt)
	return nil
}
func (c *Client) finish(r *clientRun) {
	r.cancel()
	r.closeConn()
	c.mu.Lock()
	if c.run == r {
		c.run = nil
	}
	close(r.done)
	c.mu.Unlock()
}

// Stop 取消初始化、重连及读写，重复调用安全，允许从回调中调用。
func (c *Client) Stop() {
	c.mu.Lock()
	r := c.run
	c.mu.Unlock()
	if r != nil {
		r.cancel()
		r.closeConn()
	}
}

// Done 在本次运行的网络任务和回调退出后关闭；未启动时已关闭。
func (c *Client) Done() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}

// Wait 等待本次运行停止。回调内应调用 Stop，不能调用 Wait。
func (c *Client) Wait() { <-c.Done() }
