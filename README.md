# blivedm-go

Bilibili 直播弹幕 Go 库，支持传统 JSON 和新版 Protobuf 消息。Go 1.20 或更高版本。

## 安装

```sh
go get github.com/tursom/blivedm-go
```

## 快速开始

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "os"
    "os/signal"

    "github.com/tursom/blivedm-go/client"
    "github.com/tursom/blivedm-go/message"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    c := client.NewClient(732)
    c.SetCookie(os.Getenv("BILIBILI_COOKIE"))
    c.OnDanmaku(func(d *message.Danmaku) {
        fmt.Printf("[弹幕] %s：%s\n", d.Sender.Uname, d.Content)
    })
    c.OnGift(func(g *message.Gift) {
        fmt.Printf("[礼物] %s：%s × %d\n", g.Uname, g.GiftName, g.Num)
    })
    if err := c.StartContext(ctx); err != nil {
        if !errors.Is(err, context.Canceled) {
            log.Fatal(err)
        }
        return
    }
    c.Wait()
}
```

Cookie 建议包含 `SESSDATA`、`bili_jct`、`buvid3` 和 `DedeUserID`。未登录时，上游可能隐藏用户信息或限制弹幕。不要把 Cookie 写入源码或日志。

## 协议支持

消息字段与 `danmuji-next/crates/blivedm` 参考实现同步：

| 命令 | 回调 / 模型 |
| --- | --- |
| `DANMU_MSG`（含命令后缀、`dm_v2`） | `OnDanmaku`，用户、头像、表情、勋章、回复信息 |
| `SEND_GIFT`、`SEND_GIFT_V2` | `OnGift`，包括盲盒及批次中的每个结果 |
| `SUPER_CHAT_MESSAGE` | `OnSuperChat` |
| `GUARD_BUY` | `OnGuardBuy` |
| `USER_TOAST_MSG`、`USER_TOAST_MSG_V2` | `OnUserToast` |
| `INTERACT_WORD`、`INTERACT_WORD_V2` | `OnInteractWord` |
| `ONLINE_RANK_COUNT` | `OnOnlineRankCount` |
| `ONLINE_RANK_V2` | `OnOnlineRankV2` |
| `ONLINE_RANK_V3` | `OnOnlineRankV3` |
| `LIVE`、`PREPARING` | `OnLiveStart`、`OnLiveStop` |
| 任意通知，包括未建模命令 | `OnRawEvent` 或自定义处理器 |

V2 礼物可能包含多个开盒结果；库按上游顺序逐个调用 `OnGift`，不会只保留第一个结果。V3 榜单使用单独回调，不会转换为 V2 事件。

## 生命周期与回调

- `Start()` 保留原接口；`StartContext(ctx)` 让初始化、连接、重连和运行都受同一 context 控制。返回成功表示已发送入房包，认证应答由后台处理。
- `Stop()` 可重复调用，会取消 HTTP 初始化、WebSocket 握手、重连等待并关闭 WebSocket，解除阻塞的读取。
- `Done()` / `Wait()` 用于等待网络任务及当前回调退出。停止完成后可以再次启动。运行中重复启动返回 `client.ErrAlreadyStarted`。
- **回调改为顺序执行**，网络接收通过有界队列分发，默认容量 256。队列满时施加背压，不为每条消息无限创建 goroutine。回调应及时返回；耗时工作交给调用方管理的队列。
- 可以在回调里调用 `Stop()`，但不要在回调里调用 `Wait()` 或等待 `Done()`。停止时尚未处理的队列消息会被放弃。
- 允许并发注册回调，自定义回调 panic 不影响后续回调。手动调用 `Handle` / `HandlePacket` 在调用者 goroutine 同步执行。
- 公开配置字段应在启动前设置。`SetCookie`、`SetHost` 为并发安全的配置入口，下次启动生效。

```go
c, err := client.NewClientWithOptions(732, client.Options{
    HeartbeatInterval:   30 * time.Second,
    HandshakeTimeout:    10 * time.Second,
    JoinTimeout:         10 * time.Second,
    ReadIdleTimeout:     75 * time.Second,
    WriteTimeout:       10 * time.Second,
    ReconnectInterval:   3 * time.Second,
    EventBufferCapacity: 256,
})
```

配置字段为零时使用默认值，负数返回错误。自动重连会轮换服务端返回的主机并保留 WSS 端口。`SetHost` 接受域名或完整 `ws://` / `wss://` URL。HTTP / API 错误明确返回；风控码 `±352` 和空主机列表使用参考实现的默认主机降级。

自动发现服务器时，每次重连都会重新获取 token 和主机列表；获取失败不会使用旧 token 拨号。
重试等待从 `ReconnectInterval` 开始逐次翻倍，默认是 3、6、12、24、30 秒，之后保持
30 秒（若配置的初始间隔更长，则保留该间隔）。短暂连接成功不会重置退避，连接维持
30 秒后才会重置。显式 `SetHost` 保留固定服务器模式，不调用弹幕服务器发现 API。

## 自定义与原始事件

```go
// 观察所有通知，不覆盖内置回调。cmd 保留冒号参数；data 必须视为只读。
c.OnRawEvent(func(cmd string, data []byte) {
    fmt.Printf("%s: %s\n", cmd, data)
})

// 自定义处理器优先执行，并覆盖同命令的内置解析和回调。
c.RegisterCustomEventHandler("STOP_LIVE_ROOM_LIST", func(raw string) {
    fmt.Println(raw)
})

// 移除覆盖，恢复内置处理。
c.RegisterCustomEventHandler("DANMU_MSG", nil)
```

自定义命令按冒号前的名称匹配。原始事件观察器在自定义和内置处理器之前执行，包括未知命令。

本 fork 保留 `RegisterFullEventHandler(func(string))`，供 `danmurec` 等现有调用方记录完整
JSON 通知。再次注册替换原处理器，传入 `nil` 清除；它与 `OnRawEvent` 一样不会覆盖
自定义或内置回调，并采用上面的顺序分发语义。

## 独立解析与错误处理

旧的 `Parse([]byte)` 方法仍可使用。需要处理错误时，使用 `ParseJSON([]byte) error`；V2 礼物批次使用 `message.ParseGiftsV2`。`client.HandlePacket` 也可返回解析错误。

`packet.Decode` 校验一个包；`packet.DecodeFrame` 处理多包 WebSocket 帧并递归展开 zlib / Brotli。解码限制默认为累计解压 16 MiB、8 层嵌套、65,536 个包，可通过 `packet.Decoder` 配置。旧的 `DecodePacket`、`Slice`、`Parse` 保留兼容接口，失败返回零值或 nil，不再伪造心跳响应。

HTTP 辅助函数新增对应的 `Context` 版本；HTTP 非 2xx 返回 `*api.HTTPError`，已校验的业务错误返回 `*api.APIError`。WBI 密钥缓存支持并发共享与取消等待。

## 开发验证

```sh
go test ./...
go test -race ./...
go vet ./...
go test ./packet ./message ./client -run '^$' -bench . -benchmem
go test ./packet -run '^$' -fuzz FuzzDecodeFrame -fuzztime 10s
```

测试使用本地 HTTP/WebSocket 服务及合成协议数据，不需要账号或线上直播间。

### Protobuf 维护

新版事件的字段定义在 `pb/events.proto`，生成文件为 `pb/events.pb.go`；原有 `pb/dmv2.pb.go` 保留兼容。使用已安装的 `protoc` 和 `protoc-gen-go`，在仓库根目录重新生成：

```sh
protoc --go_out=. --go_opt=paths=source_relative pb/events.proto
```
