package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const wbiTestNav = `{"code":-101,"message":"not logged in","data":{"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`

func TestWBISigningCanonicalizesValues(t *testing.T) {
	u, err := url.Parse("https://example.invalid/?z=1919810&foo=1%271%284%29%21%2A&bar=514&sp=a+b%2Bc&wts=1&w_rid=stale")
	if err != nil {
		t.Fatal(err)
	}
	const mixin = "ea1db124af3c7062474693fa704f4ff8"
	if err := signWbiURL(u, mixin, 1702204169); err != nil {
		t.Fatal(err)
	}
	const canonical = "bar=514&foo=114&sp=a%20b%2Bc&wts=1702204169&z=1919810"
	digest := md5.Sum([]byte(canonical + mixin))
	want := canonical + "&w_rid=" + hex.EncodeToString(digest[:])
	if u.RawQuery != want {
		t.Fatalf("query = %q, want %q", u.RawQuery, want)
	}
	if err := signWbiURL(u, mixin, 1702204169); err != nil || u.RawQuery != want {
		t.Fatalf("re-signing changed signature: %s, %v", u.RawQuery, err)
	}
}

func TestWBICacheCoalescesConcurrentRefresh(t *testing.T) {
	var requests atomic.Int32
	useHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return jsonResponse(wbiTestNav), nil
	})
	var cache wbiCache
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			keys, err := cache.get(context.Background(), false)
			if err != nil {
				t.Errorf("get WBI keys: %v", err)
				return
			}
			if keys.Mixin != "ea1db124af3c7062474693fa704f4ff8" {
				t.Errorf("mixin = %q", keys.Mixin)
			}
		}()
	}
	close(start)
	group.Wait()
	if requests.Load() != 1 {
		t.Fatalf("performed %d key refreshes, want 1", requests.Load())
	}
}

func TestWBICacheWaitHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	useHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return jsonResponse(wbiTestNav), nil
	})
	var cache wbiCache
	leader := make(chan error, 1)
	go func() {
		_, err := cache.get(context.Background(), false)
		leader <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := cache.get(ctx, false)
	close(release)
	leaderErr := <-leader
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting caller error = %v", err)
	}
	if leaderErr != nil {
		t.Fatalf("canceling waiter affected refresh: %v", leaderErr)
	}
}

func TestWBIRejectsMalformedKeys(t *testing.T) {
	for _, keyURL := range []string{"", "https://example.invalid/x.png", "https://example.invalid/" + strings.Repeat("z", 32) + ".png"} {
		if _, err := wbiKeyFromURL(keyURL); err == nil {
			t.Errorf("accepted malformed key %q", keyURL)
		}
	}
}

func TestWBIUpdatePopulatesReceiver(t *testing.T) {
	useHTTPTransport(t, func(*http.Request) (*http.Response, error) { return jsonResponse(wbiTestNav), nil })
	useCachedWBIKeys(t)
	var keys WbiKeys
	if err := keys.Update(); err != nil {
		t.Fatal(err)
	}
	if len(keys.Img) != 32 || len(keys.Sub) != 32 || len(keys.Mixin) != 32 {
		t.Fatalf("receiver was not updated: %+v", keys)
	}
}

func useCachedWBIKeys(t *testing.T) {
	t.Helper()
	defaultWbiCache.mu.Lock()
	previous := defaultWbiCache.keys
	defaultWbiCache.keys = WbiKeys{Mixin: "ea1db124af3c7062474693fa704f4ff8", lastUpdateTime: time.Now()}
	defaultWbiCache.mu.Unlock()
	t.Cleanup(func() {
		defaultWbiCache.mu.Lock()
		defaultWbiCache.keys = previous
		defaultWbiCache.mu.Unlock()
	})
}

func TestDanmuInfoFallbackAndErrors(t *testing.T) {
	useCachedWBIKeys(t)
	for _, code := range []int{-352, 352, 0, -400} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			useHTTPTransport(t, func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Cookie") != "test-cookie" || req.URL.Query().Get("w_rid") == "" {
					t.Errorf("missing cookie or WBI signature")
				}
				data := `[]`
				if code == 0 {
					data = `{"token":"token","host_list":[]}`
				}
				return jsonResponse(fmt.Sprintf(`{"code":%d,"message":"test","data":%s}`, code, data)), nil
			})
			result, err := GetDanmuInfoContext(context.Background(), 1, "test-cookie")
			if code == -400 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Code != code {
					t.Fatalf("expected APIError, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Data.HostList) != 1 || result.Data.HostList[0].Host != "broadcastlv.chat.bilibili.com" {
				t.Fatalf("fallback host missing: %+v", result)
			}
			if code == 0 && result.Data.Token != "token" {
				t.Fatal("empty host list discarded valid token")
			}
		})
	}
}

func TestRoomAndUIDErrors(t *testing.T) {
	useHTTPTransport(t, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":-101,"message":"not logged in"}`), nil
	})
	_, roomErr := GetRoomInfoContext(context.Background(), 1)
	_, uidErr := GetUidContext(context.Background(), "cookie")
	for _, err := range []error{roomErr, uidErr} {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != -101 {
			t.Fatalf("expected APIError, got %v", err)
		}
	}
}
