package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const wbiCacheTTL = time.Hour

var defaultWbiCache wbiCache

// 保持 WbiKeys 可按值复制；同步只保护方法内的读取和写入。
var wbiInstanceMu sync.Mutex

func WbiKeysSignString(u string) (string, error) {
	return WbiKeysSignStringContext(context.Background(), u)
}

func WbiKeysSignStringContext(ctx context.Context, u string) (string, error) {
	parsedURL, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	if err := WbiKeysSignContext(ctx, parsedURL); err != nil {
		return "", err
	}
	return parsedURL.String(), nil
}

func WbiKeysSign(u *url.URL) error {
	return WbiKeysSignContext(context.Background(), u)
}

func WbiKeysSignContext(ctx context.Context, u *url.URL) error {
	if u == nil {
		return errors.New("cannot sign a nil URL")
	}
	keys, err := WbiKeysGetContext(ctx)
	if err != nil {
		return err
	}
	return signWbiURL(u, keys.Mixin, time.Now().Unix())
}

// WbiKeysUpdate 无视过期时间刷新共享密钥。
func WbiKeysUpdate() error { return WbiKeysUpdateContext(context.Background()) }

func WbiKeysUpdateContext(ctx context.Context) error {
	_, err := defaultWbiCache.get(ctx, true)
	return err
}

func WbiKeysGet() (WbiKeys, error) { return WbiKeysGetContext(context.Background()) }

func WbiKeysGetContext(ctx context.Context) (WbiKeys, error) {
	return defaultWbiCache.get(ctx, false)
}

var mixinKeyEncTab = [...]int{
	46, 47, 18, 2, 53, 8, 23, 32,
	15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19,
	29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61,
	26, 17, 0, 1, 60, 51, 30, 4,
	22, 25, 54, 21, 56, 59, 6, 63,
	57, 62, 11, 36, 20, 34, 44, 52,
}

var wbiFilter = strings.NewReplacer("!", "", "'", "", "(", "", ")", "", "*", "")

// Nav 是 WBI 密钥接口的响应。
type Nav struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Ttl     int    `json:"ttl"`
	Data    struct {
		WbiImg struct {
			ImgUrl string `json:"img_url"`
			SubUrl string `json:"sub_url"`
		} `json:"wbi_img"`
	} `json:"data"`
}

// WbiKeys 可按值复制。调用方法期间不要直接修改其导出字段。
type WbiKeys struct {
	Img            string
	Sub            string
	Mixin          string
	lastUpdateTime time.Time
}

func (wk *WbiKeys) Sign(u *url.URL) error {
	return wk.SignContext(context.Background(), u)
}

func (wk *WbiKeys) SignContext(ctx context.Context, u *url.URL) error {
	if wk == nil || u == nil {
		return errors.New("cannot sign with nil keys or URL")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	wbiInstanceMu.Lock()
	keys := *wk
	wbiInstanceMu.Unlock()
	if keys.Mixin == "" || time.Since(keys.lastUpdateTime) >= wbiCacheTTL {
		var err error
		keys, err = WbiKeysGetContext(ctx)
		if err != nil {
			return err
		}
		wbiInstanceMu.Lock()
		*wk = keys
		wbiInstanceMu.Unlock()
	}
	return signWbiURL(u, keys.Mixin, time.Now().Unix())
}

func (wk *WbiKeys) Update() error { return wk.UpdateContext(context.Background()) }

func (wk *WbiKeys) UpdateContext(ctx context.Context) error {
	if wk == nil {
		return errors.New("cannot update nil WBI keys")
	}
	keys, err := defaultWbiCache.get(ctx, true)
	if err != nil {
		return err
	}
	wbiInstanceMu.Lock()
	*wk = keys
	wbiInstanceMu.Unlock()
	return nil
}

func signWbiURL(u *url.URL, mixin string, timestamp int64) error {
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("invalid WBI query: %w", err)
	}
	values.Del("w_rid")
	values.Set("wts", strconv.FormatInt(timestamp, 10))
	for key, entries := range values {
		for i, entry := range entries {
			entries[i] = wbiFilter.Replace(entry)
		}
		values[key] = entries
	}
	// WBI 使用 RFC 3986 编码：空格是 %20，而不是表单编码的 +。
	query := strings.ReplaceAll(values.Encode(), "+", "%20")
	hash := md5.Sum([]byte(query + mixin))
	u.RawQuery = query + "&w_rid=" + hex.EncodeToString(hash[:])
	return nil
}

type wbiRefresh struct {
	done chan struct{}
	keys WbiKeys
	err  error
}

type wbiCache struct {
	mu      sync.Mutex
	keys    WbiKeys
	pending *wbiRefresh
}

func (cache *wbiCache) get(ctx context.Context, force bool) (WbiKeys, error) {
	for {
		if err := ctx.Err(); err != nil {
			return WbiKeys{}, err
		}
		cache.mu.Lock()
		if !force && cache.keys.Mixin != "" && time.Since(cache.keys.lastUpdateTime) < wbiCacheTTL {
			keys := cache.keys
			cache.mu.Unlock()
			return keys, nil
		}
		if pending := cache.pending; pending != nil {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return WbiKeys{}, ctx.Err()
			case <-pending.done:
				// 发起刷新者的取消不应取消其他仍有效的调用。
				if errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded) {
					continue
				}
				return pending.keys, pending.err
			}
		}
		pending := &wbiRefresh{done: make(chan struct{})}
		cache.pending = pending
		cache.mu.Unlock()

		pending.keys, pending.err = fetchWbiKeys(ctx)
		cache.mu.Lock()
		if pending.err == nil {
			cache.keys = pending.keys
		}
		cache.pending = nil
		close(pending.done)
		cache.mu.Unlock()
		return pending.keys, pending.err
	}
}

func fetchWbiKeys(ctx context.Context) (WbiKeys, error) {
	var nav Nav
	if err := GetJsonWithHeaderContext(ctx, "https://api.bilibili.com/x/web-interface/nav", liveHeaders(""), &nav); err != nil {
		return WbiKeys{}, err
	}
	if nav.Code != 0 && nav.Code != -101 {
		return WbiKeys{}, apiError(nav.Code, nav.Message, "")
	}
	img, err := wbiKeyFromURL(nav.Data.WbiImg.ImgUrl)
	if err != nil {
		return WbiKeys{}, err
	}
	sub, err := wbiKeyFromURL(nav.Data.WbiImg.SubUrl)
	if err != nil {
		return WbiKeys{}, err
	}
	combined := img + sub
	var mixin [32]byte
	for i := range mixin {
		mixin[i] = combined[mixinKeyEncTab[i]]
	}
	return WbiKeys{Img: img, Sub: sub, Mixin: string(mixin[:]), lastUpdateTime: time.Now()}, nil
}

func wbiKeyFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid WBI key URL: %w", err)
	}
	key := strings.TrimSuffix(path.Base(u.Path), ".png")
	if len(key) != 32 {
		return "", errors.New("invalid WBI key length")
	}
	if _, err := hex.DecodeString(key); err != nil {
		return "", fmt.Errorf("invalid WBI key: %w", err)
	}
	return key, nil
}
