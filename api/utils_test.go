package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func useHTTPTransport(t *testing.T, f roundTripFunc) {
	t.Helper()
	previous := httpClient
	httpClient = &http.Client{Transport: f, Timeout: time.Second}
	t.Cleanup(func() { httpClient = previous })
}

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestHTTPGetInvalidURLAndNilHeaders(t *testing.T) {
	if _, err := HttpGet("://invalid", nil); err == nil {
		t.Fatal("invalid URL accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"value":42}`)
	}))
	defer server.Close()
	body, err := HttpGet(server.URL, nil)
	if err != nil || string(body) != `{"value":42}` {
		t.Fatalf("HttpGet = %q, %v", body, err)
	}
	var result struct{ Value int }
	if err := GetJson(server.URL, &result); err != nil || result.Value != 42 {
		t.Fatalf("GetJson = %+v, %v", result, err)
	}
}

func TestHTTPStatusAndContextErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := HttpGet(server.URL, nil)
	var statusErr *HTTPError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTPError, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = HttpGetContext(ctx, server.URL, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestHTTPHeadersAreCopied(t *testing.T) {
	headers := http.Header{"X-Test": {"original"}}
	useHTTPTransport(t, func(req *http.Request) (*http.Response, error) {
		req.Header.Set("X-Test", "mutated")
		return jsonResponse(`{}`), nil
	})
	if _, err := HttpGet("https://example.invalid", &headers); err != nil {
		t.Fatal(err)
	}
	if headers.Get("X-Test") != "original" {
		t.Fatalf("request mutated caller headers: %v", headers)
	}
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error { b.closed = true; return nil }

func TestJSONResponseClosesBodyOnInvalidJSON(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader(`{"broken"`)}
	useHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		resp := jsonResponse("")
		resp.Body = body
		return resp, nil
	})
	var result any
	if err := GetJson("https://example.invalid", &result); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if !body.closed {
		t.Fatal("response body leaked")
	}
}

func TestSendDanmakuTransportFailure(t *testing.T) {
	failure := errors.New("transport unavailable")
	useHTTPTransport(t, func(*http.Request) (*http.Response, error) { return nil, failure })
	if _, err := SendDanmaku(&DanmakuRequest{RoomID: "1"}, &BiliVerify{}); !errors.Is(err, failure) {
		t.Fatalf("expected transport error, got %v", err)
	}
	if _, err := SendDanmaku(nil, nil); err == nil {
		t.Fatal("nil request accepted")
	}
}

func TestHTTPResponseSizeLimit(t *testing.T) {
	useHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(strings.Repeat("a", maxResponseBytes+1)), nil
	})
	if _, err := HttpGet("https://example.invalid", nil); err == nil {
		t.Fatal("oversize response accepted")
	}
}
