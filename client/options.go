package client

import (
	"fmt"
	"time"
)

// Options 的零值使用默认配置，负数无效。
type Options struct {
	HandshakeTimeout    time.Duration
	JoinTimeout         time.Duration
	ReadIdleTimeout     time.Duration
	WriteTimeout        time.Duration
	HeartbeatInterval   time.Duration
	ReconnectInterval   time.Duration
	EventBufferCapacity int
}

func (o Options) withDefaults() (Options, error) {
	for _, entry := range []struct {
		name  string
		value *time.Duration
		def   time.Duration
	}{
		{"HandshakeTimeout", &o.HandshakeTimeout, 10 * time.Second},
		{"JoinTimeout", &o.JoinTimeout, 10 * time.Second},
		{"ReadIdleTimeout", &o.ReadIdleTimeout, 75 * time.Second},
		{"WriteTimeout", &o.WriteTimeout, 10 * time.Second},
		{"HeartbeatInterval", &o.HeartbeatInterval, 30 * time.Second},
		{"ReconnectInterval", &o.ReconnectInterval, 3 * time.Second},
	} {
		if *entry.value < 0 {
			return o, fmt.Errorf("%s must not be negative", entry.name)
		}
		if *entry.value == 0 {
			*entry.value = entry.def
		}
	}
	if o.EventBufferCapacity < 0 {
		return o, fmt.Errorf("EventBufferCapacity must not be negative")
	}
	if o.EventBufferCapacity == 0 {
		o.EventBufferCapacity = 256
	}
	return o, nil
}
