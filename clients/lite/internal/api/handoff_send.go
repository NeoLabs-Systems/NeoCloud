package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// HandoffDevice is one of the account's devices a send can be addressed to.
type HandoffDevice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	Kind       string `json:"kind"`
	CanReceive bool   `json:"canReceive"`
	Online     bool   `json:"online"`
	Current    bool   `json:"current"`
}

// Target reports whether a send can go to the device now: online, accepting
// and not this computer.
func (d HandoffDevice) Target() bool { return d.Online && d.CanReceive && !d.Current }

// HandoffNewFile announces one file of a send before its bytes are uploaded.
type HandoffNewFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// HandoffDevices lists the devices of the account a send can be addressed
// to: the online ones first, then apps seen in the last week.
func (c *Client) HandoffDevices(ctx context.Context) ([]HandoffDevice, error) {
	var out struct {
		Devices []HandoffDevice `json:"devices"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/handoff/devices", nil, nil, &out)
	return out.Devices, err
}

// HandoffCreate opens a send of files to the device targetID. The answer
// lists a server-side id for every file, in the order they were announced;
// each must be uploaded with HandoffUpload.
func (c *Client) HandoffCreate(ctx context.Context, targetID string, files []HandoffNewFile) (*HandoffOffer, error) {
	var out HandoffOffer
	body := map[string]any{"targetId": targetID, "files": files}
	if err := c.doJSON(ctx, http.MethodPost, "/api/handoff/offers", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HandoffUpload streams exactly size bytes of body as one file of a send.
// The body is never buffered: it goes from body to the connection.
func (c *Client) HandoffUpload(ctx context.Context, offerID string, fileID, size int64, body io.Reader) (*HandoffOffer, error) {
	reader := c.Up.Reader(ctx, body)
	if size == 0 {
		reader = http.NoBody
	}
	req, err := c.newRequest(ctx, http.MethodPut, "/api/handoff/offers/"+url.PathEscape(offerID)+"/files/"+strconv.FormatInt(fileID, 10), nil, reader)
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out HandoffOffer
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HandoffOfferByID is a send this device made (or was sent), as it stands.
func (c *Client) HandoffOfferByID(ctx context.Context, offerID string) (*HandoffOffer, error) {
	var out HandoffOffer
	if err := c.doJSON(ctx, http.MethodGet, "/api/handoff/offers/"+url.PathEscape(offerID), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HandoffCancel cancels a send this device made; its staged files are
// deleted. Cancelling a send that already ended changes nothing.
func (c *Client) HandoffCancel(ctx context.Context, offerID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/handoff/offers/"+url.PathEscape(offerID), nil, nil, nil)
}
