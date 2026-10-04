package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:137.0) Gecko/20100101 Firefox/137.0"
const maxResponseBytes = 16 << 20

// 复用连接池，并为没有传入 deadline 的调用提供超时上限。
var httpClient = &http.Client{Timeout: 15 * time.Second}

// HTTPError 表示服务端返回了非 2xx HTTP 状态。
type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string { return "bilibili HTTP: " + e.Status }

// APIError 表示 Bilibili API 返回的业务错误。
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bilibili API: code %d: %s", e.Code, e.Message)
}

func apiError(code int, message, fallback string) error {
	if code == 0 {
		return nil
	}
	if message == "" {
		message = fallback
	}
	return &APIError{Code: code, Message: message}
}

func liveHeaders(cookie string) *http.Header {
	headers := make(http.Header)
	headers.Set("User-Agent", userAgent)
	if cookie != "" {
		headers.Set("Cookie", cookie)
	}
	return &headers
}

func newGetRequest(ctx context.Context, endpoint string, headers *http.Header) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if headers != nil && *headers != nil {
		req.Header = headers.Clone()
	}
	return req, nil
}

func doRequest(req *http.Request) (*http.Response, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		// 小型错误响应读完后仍可复用连接，避免无限读取异常服务端响应。
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}
	return resp, nil
}

func decodeResponse(req *http.Request, result any) error {
	resp, err := doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	reader := &io.LimitedReader{R: resp.Body, N: maxResponseBytes + 1}
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(result); err != nil {
		return err
	}
	var extra json.RawMessage
	err = decoder.Decode(&extra)
	if reader.N == 0 {
		return fmt.Errorf("bilibili HTTP: response exceeds %d bytes", maxResponseBytes)
	}
	if err != io.EOF {
		if err == nil {
			return fmt.Errorf("bilibili HTTP: multiple JSON values in response")
		}
		return err
	}
	return nil
}

func HttpGet(endpoint string, headers *http.Header) ([]byte, error) {
	return HttpGetContext(context.Background(), endpoint, headers)
}

func HttpGetContext(ctx context.Context, endpoint string, headers *http.Header) ([]byte, error) {
	req, err := newGetRequest(ctx, endpoint, headers)
	if err != nil {
		return nil, err
	}
	resp, err := doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("bilibili HTTP: response exceeds %d bytes", maxResponseBytes)
	}
	return body, nil
}

func GetJson(endpoint string, result any) error {
	return GetJsonContext(context.Background(), endpoint, result)
}

func GetJsonContext(ctx context.Context, endpoint string, result any) error {
	return GetJsonWithHeaderContext(ctx, endpoint, nil, result)
}

func GetJsonWithHeader(endpoint string, headers *http.Header, result any) error {
	return GetJsonWithHeaderContext(context.Background(), endpoint, headers, result)
}

func GetJsonWithHeaderContext(ctx context.Context, endpoint string, headers *http.Header, result any) error {
	req, err := newGetRequest(ctx, endpoint, headers)
	if err != nil {
		return err
	}
	return decodeResponse(req, result)
}
