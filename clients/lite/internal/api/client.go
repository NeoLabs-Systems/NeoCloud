// Package api is a small HTTP client for the NeoCloud endpoints a sync
// client needs (docs/clients.md): device login, the sync tree and change
// journal, content transfer and the handful of tree operations.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to one NeoCloud server. Token may be empty for the
// unauthenticated device-login calls.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	// Up and Down throttle content bodies; nil means unlimited.
	Up   *Limiter
	Down *Limiter
}

// New returns a client with sane timeouts. Content transfers can take long,
// so only the connection phases are bounded, not the whole request.
func New(baseURL, token string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 60 * time.Second
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Transport: transport},
	}
}

// Error is a non-2xx answer. Code is the server's machine code
// (e.g. NAME_EXISTS); clients branch on it, never on Message.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("neocloud: %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("neocloud: %d %s", e.Status, e.Message)
}

// HasCode reports whether err is an API error with the given code.
func HasCode(err error, code string) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Code == code
}

// HasStatus reports whether err is an API error with the given HTTP status.
func HasStatus(err error, status int) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == status
}

func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Request, error) {
	target := c.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("User-Agent", "neocloud-lite")
	return req, nil
}

// do sends the request and turns error statuses into *Error. The caller
// owns the body of a successful response.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return res, nil
	}
	defer res.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	apiErr := &Error{Status: res.StatusCode, Message: http.StatusText(res.StatusCode)}
	var parsed struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(payload, &parsed) == nil {
		apiErr.Code = parsed.Code
		if parsed.Error != "" {
			apiErr.Message = parsed.Error
		}
	}
	return nil, apiErr
}

// doJSON sends `in` as JSON (when non-nil) and decodes the answer into `out`
// (when non-nil).
func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := c.newRequest(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, res.Body)
		return err
	}
	return json.NewDecoder(res.Body).Decode(out)
}
