package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// HandoffFile is one file of a send to this device.
type HandoffFile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// HandoffOffer is a send from another of the account's devices
// (docs/api.md, "Send to device").
type HandoffOffer struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	From   struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	} `json:"from"`
	To struct {
		Name string `json:"name"`
	} `json:"to"`
	TotalSize int64         `json:"totalSize"`
	Files     []HandoffFile `json:"files"`
}

// HandoffEvents opens the event stream that makes this device online. The
// caller reads res.Body until it ends and closes it. receive false keeps
// the device out of other devices' send lists.
func (c *Client) HandoffEvents(ctx context.Context, receive bool) (*http.Response, error) {
	query := url.Values{"receive": {map[bool]string{true: "1", false: "0"}[receive]}}
	req, err := c.newRequest(ctx, http.MethodGet, "/api/handoff/events", query, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	return c.do(req)
}

// HandoffInbox lists the sends waiting for this device.
func (c *Client) HandoffInbox(ctx context.Context) ([]HandoffOffer, error) {
	var out struct {
		Inbox []HandoffOffer `json:"inbox"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/handoff/offers", nil, nil, &out)
	return out.Inbox, err
}

// HandoffAction is "accept", "decline" or "complete" on an offer.
func (c *Client) HandoffAction(ctx context.Context, offerID, action string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/handoff/offers/"+url.PathEscape(offerID)+"/"+action, nil, nil, nil)
}

// HandoffFileContent opens a file of an accepted offer from byte offset
// on (a Range request). The caller closes the body.
func (c *Client) HandoffFileContent(ctx context.Context, offerID string, fileID, offset int64) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/handoff/offers/"+url.PathEscape(offerID)+"/files/"+strconv.FormatInt(fileID, 10), nil, nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	return c.do(req)
}
