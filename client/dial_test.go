package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestStopCancelsProxyConnect(t *testing.T) {
	mockRoomAPI(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy method = %s", r.Method)
		}
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer proxy.Close()
	defer close(release)
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	original := websocket.DefaultDialer
	dialer := *original
	dialer.Proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	websocket.DefaultDialer = &dialer
	defer func() { websocket.DefaultDialer = original }()
	c := newTestClient(t, "wss://unused.example.test/sub")
	result := make(chan error, 1)
	go func() { result <- c.StartContext(testContext(t)) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("proxy CONNECT did not start")
	}
	c.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel proxy CONNECT")
	}
	waitDone(t, c.Done())
}

func TestStopDuringAPIErrorResponseReturnsCanceled(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.(http.Flusher).Flush()
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = target.Scheme, target.Host
		return original.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = original }()
	c := NewClient(42)
	defer c.Stop()
	result := make(chan error, 1)
	go func() { result <- c.Start() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("API did not return response headers")
	}
	c.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel initialization")
	}
	waitDone(t, c.Done())
}
