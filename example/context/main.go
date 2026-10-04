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
	c.OnDanmaku(func(d *message.Danmaku) { fmt.Printf("[弹幕] %s：%s\n", d.Sender.Uname, d.Content) })
	c.OnGift(func(g *message.Gift) { fmt.Printf("[礼物] %s：%s × %d\n", g.Uname, g.GiftName, g.Num) })
	if err := c.StartContext(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Fatal(err)
		}
		return
	}
	c.Wait()
}
