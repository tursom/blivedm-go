package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tursom/blivedm-go/api"
)

// ConnectionStatus is a read-only snapshot of a client's current run.
// State is idle, connecting (including initialization), authenticating,
// connected, retrying, stopped, or error (a terminal startup/run failure).
// Host is the current/last attempted WebSocket URL, without userinfo, query,
// or fragment. RetryAt is the retry wait deadline, and is zero outside retrying.
// UpdatedAt changes only when the snapshot changes, not on each heartbeat.
type ConnectionStatus struct {
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	Host      string    `json:"host,omitempty"`
	RetryAt   time.Time `json:"retryAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Status returns an independent, concurrency-safe snapshot. It does not wait
// for network I/O or handlers. A zero-value Client initially reports idle.
// As with other time.Time fields, encoding/json encodes a zero RetryAt as
// "0001-01-01T00:00:00Z" despite omitempty; callers should use State or IsZero.
func (c *Client) Status() ConnectionStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status.State == "" {
		c.setStatusLocked("idle", "", "", time.Time{})
	}
	return c.status
}

// The caller holds c.mu. No connection or handler locks are acquired here.
func (c *Client) setStatusLocked(state, message, host string, retryAt time.Time) {
	if c.status.State == state && c.status.Error == message && c.status.Host == host && c.status.RetryAt.Equal(retryAt) {
		return
	}
	c.status = ConnectionStatus{State: state, Error: message, Host: host, RetryAt: retryAt, UpdatedAt: time.Now()}
}

// Active updates cannot revive a canceled run or overwrite a subsequent run.
func (c *Client) setRunStatus(r *clientRun, state string, err error, host string, retryAt time.Time) {
	message, host := safeConnectionError(err), statusHost(host)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run == r && r.ctx.Err() == nil {
		c.setStatusLocked(state, message, host, retryAt)
	}
}

func statusHost(host string) string {
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return ""
	}
	u.User, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = nil, "", "", "", false
	return u.String()
}

var errInvalidEndpoint = errors.New("invalid WebSocket endpoint")

type authenticationError struct{ code int64 }

func (e *authenticationError) Error() string {
	return fmt.Sprintf("room authentication rejected: code=%d", e.code)
}

// Never copy arbitrary error text: URLs, HTTP status text, API messages and
// WebSocket close reasons can all contain credentials or server-echoed secrets.
func safeConnectionError(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *api.APIError
	var httpErr *api.HTTPError
	var authErr *authenticationError
	var closeErr *websocket.CloseError
	var netErr net.Error
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &apiErr):
		return fmt.Sprintf("bilibili API: code=%d", apiErr.Code)
	case errors.As(err, &httpErr):
		return fmt.Sprintf("bilibili HTTP: status=%d", httpErr.StatusCode)
	case errors.As(err, &authErr):
		return authErr.Error()
	case errors.As(err, &closeErr):
		return fmt.Sprintf("WebSocket closed: code=%d", closeErr.Code)
	case errors.Is(err, errInvalidEndpoint):
		return errInvalidEndpoint.Error()
	case errors.Is(err, context.Canceled):
		return "operation canceled"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "connection timed out"
	case errors.Is(err, websocket.ErrBadHandshake):
		return "WebSocket handshake failed"
	case errors.As(err, &syntaxErr):
		return "invalid JSON response"
	default:
		return "connection failed"
	}
}
