package client

import (
	"fmt"
	"runtime/debug"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tursom/blivedm-go/message"
	"github.com/tursom/blivedm-go/packet"
)

type eventHandlers struct {
	danmaku         []func(*message.Danmaku)
	superChat       []func(*message.SuperChat)
	gift            []func(*message.Gift)
	guardBuy        []func(*message.GuardBuy)
	liveStart       []func(*message.LiveStart)
	liveStop        []func(*message.LiveStop)
	userToast       []func(*message.UserToast)
	interactWord    []func(*message.InteractWord)
	onlineRankCount []func(*message.OnlineRankCount)
	onlineRankV2    []func(*message.OnlineRankV2)
	onlineRankV3    []func(*message.OnlineRankV3)
	raw             []func(string, []byte)
}
type customEventHandlers map[string]func(string)

// RegisterFullEventHandler 设置全事件处理器，兼容 fork 的旧接口。
// 它不覆盖自定义或内置处理器；再次注册会替换原处理器，nil 清除。
func (c *Client) RegisterFullEventHandler(handler func(string)) {
	c.handlerMu.Lock()
	c.fullEventHandler = handler
	c.handlerMu.Unlock()
}

// RegisterCustomEventHandler 注册优先于内置解析的处理器。nil 删除该覆盖。
// 带参数的命令统一使用冒号之前的命令名。
func (c *Client) RegisterCustomEventHandler(cmd string, handler func(string)) {
	cmd, _, _ = strings.Cut(cmd, ":")
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	if c.customEventHandlers == nil {
		c.customEventHandlers = make(customEventHandlers)
	}
	if handler == nil {
		delete(c.customEventHandlers, cmd)
	} else {
		c.customEventHandlers[cmd] = handler
	}
}

// OnDanmaku 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnDanmaku(f func(*message.Danmaku)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.danmaku = append(c.eventHandlers.danmaku, f)
	c.handlerMu.Unlock()
}

// OnSuperChat 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnSuperChat(f func(*message.SuperChat)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.superChat = append(c.eventHandlers.superChat, f)
	c.handlerMu.Unlock()
}

// OnGift 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnGift(f func(*message.Gift)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.gift = append(c.eventHandlers.gift, f)
	c.handlerMu.Unlock()
}

// OnGuardBuy 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnGuardBuy(f func(*message.GuardBuy)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.guardBuy = append(c.eventHandlers.guardBuy, f)
	c.handlerMu.Unlock()
}

// OnLiveStart 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnLiveStart(f func(*message.LiveStart)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.liveStart = append(c.eventHandlers.liveStart, f)
	c.handlerMu.Unlock()
}

// OnLiveStop 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnLiveStop(f func(*message.LiveStop)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.liveStop = append(c.eventHandlers.liveStop, f)
	c.handlerMu.Unlock()
}

// OnUserToast 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnUserToast(f func(*message.UserToast)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.userToast = append(c.eventHandlers.userToast, f)
	c.handlerMu.Unlock()
}

// OnInteractWord 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnInteractWord(f func(*message.InteractWord)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.interactWord = append(c.eventHandlers.interactWord, f)
	c.handlerMu.Unlock()
}

// OnOnlineRankCount 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnOnlineRankCount(f func(*message.OnlineRankCount)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.onlineRankCount = append(c.eventHandlers.onlineRankCount, f)
	c.handlerMu.Unlock()
}

// OnOnlineRankV2 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnOnlineRankV2(f func(*message.OnlineRankV2)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.onlineRankV2 = append(c.eventHandlers.onlineRankV2, f)
	c.handlerMu.Unlock()
}

// OnOnlineRankV3 添加处理器。回调按注册顺序执行，应及时返回。
func (c *Client) OnOnlineRankV3(f func(*message.OnlineRankV3)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.onlineRankV3 = append(c.eventHandlers.onlineRankV3, f)
	c.handlerMu.Unlock()
}

// OnRawEvent 观察全部通知（包括未知命令），在自定义和内置处理器之前执行。
// cmd 保留冒号参数，data 为只读原始 JSON；此回调不覆盖内置处理器。
func (c *Client) OnRawEvent(f func(cmd string, data []byte)) {
	if f == nil {
		return
	}
	c.handlerMu.Lock()
	c.eventHandlers.raw = append(c.eventHandlers.raw, f)
	c.handlerMu.Unlock()
}

// Handle 同步分发一个包，兼容旧入口；回调 panic 被隔离。
// 网络接收使用独立、有界的分发队列，保持服务器消息顺序。
func (c *Client) Handle(p packet.Packet) {
	if err := c.HandlePacket(p); err != nil {
		log.WithError(err).Debug("invalid notification")
	}
}

// HandlePacket 与 Handle 相同，但将解析错误返回调用方。
func (c *Client) HandlePacket(p packet.Packet) error {
	if p.Operation != packet.Notification {
		return nil
	}
	cmd := parseCmd(p.Body)
	if cmd == "" {
		return fmt.Errorf("notification has no valid string cmd")
	}
	return c.handleNotification(p.Body, cmd)
}

// 网络路径已解析命令，复用结果以避免再次扫描整条通知。
func (c *Client) handleNotification(body []byte, cmd string) error {
	base, _, _ := strings.Cut(cmd, ":")
	c.handlerMu.RLock()
	handlers := c.eventHandlers
	custom := c.customEventHandlers[base]
	full := c.fullEventHandler
	c.handlerMu.RUnlock()
	if full != nil {
		cover(func() { full(string(body)) })
	}
	for _, fn := range handlers.raw {
		cover(func() { fn(cmd, body) })
	}
	if custom != nil {
		cover(func() { custom(string(body)) })
		return nil
	}
	switch base {
	case "DANMU_MSG":
		return dispatch(body, handlers.danmaku, (*message.Danmaku).ParseJSON)
	case "SUPER_CHAT_MESSAGE":
		return dispatch(body, handlers.superChat, (*message.SuperChat).ParseJSON)
	case "SEND_GIFT":
		return dispatch(body, handlers.gift, (*message.Gift).ParseJSON)
	case "GUARD_BUY":
		return dispatch(body, handlers.guardBuy, (*message.GuardBuy).ParseJSON)
	case "LIVE":
		return dispatch(body, handlers.liveStart, (*message.LiveStart).ParseJSON)
	case "PREPARING":
		return dispatch(body, handlers.liveStop, (*message.LiveStop).ParseJSON)
	case "USER_TOAST_MSG", "USER_TOAST_MSG_V2":
		return dispatch(body, handlers.userToast, (*message.UserToast).ParseJSON)
	case "INTERACT_WORD", "INTERACT_WORD_V2":
		return dispatch(body, handlers.interactWord, (*message.InteractWord).ParseJSON)
	case "ONLINE_RANK_COUNT":
		return dispatch(body, handlers.onlineRankCount, (*message.OnlineRankCount).ParseJSON)
	case "ONLINE_RANK_V2":
		return dispatch(body, handlers.onlineRankV2, (*message.OnlineRankV2).ParseJSON)
	case "ONLINE_RANK_V3":
		return dispatch(body, handlers.onlineRankV3, (*message.OnlineRankV3).ParseJSON)
	case "SEND_GIFT_V2":
		if len(handlers.gift) == 0 {
			return nil
		}
		gifts, err := message.ParseGiftsV2(body)
		if err != nil {
			return err
		}
		for i := range gifts {
			for _, fn := range handlers.gift {
				cover(func() { fn(&gifts[i]) })
			}
		}
	}
	return nil
}
func dispatch[T any](data []byte, handlers []func(*T), parse func(*T, []byte) error) error {
	if len(handlers) == 0 {
		return nil
	}
	value := new(T)
	if err := parse(value, data); err != nil {
		return err
	}
	for _, fn := range handlers {
		cover(func() { fn(value) })
	}
	return nil
}
func parseCmd(data []byte) string {
	if !gjson.ValidBytes(data) {
		return ""
	}
	cmd := gjson.GetBytes(data, "cmd")
	if cmd.Type != gjson.String {
		return ""
	}
	return cmd.String()
}
func cover(f func()) {
	defer func() {
		if pan := recover(); pan != nil {
			log.Errorf("event error: %v\n%s", pan, debug.Stack())
		}
	}()
	f()
}
