package handoff

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	gosync "sync"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// Transfer states, in the order a send goes through them.
const (
	StateUploading = "uploading" // the files are on their way to the server
	StateWaiting   = "waiting"   // uploaded; the other device has not answered
	StateReceiving = "receiving" // the other device accepted and is downloading
	StateDelivered = "delivered"
	StateDeclined  = "declined"
	StateExpired   = "expired"
	StateCancelled = "cancelled"
	StateFailed    = "failed"
)

// Transfer is what the person sees of one send: a snapshot of its progress.
type Transfer struct {
	ID      string `json:"id"`
	To      string `json:"to"`
	Label   string `json:"label"` // "report.pdf" or "3 files"
	Size    int64  `json:"size"`
	Done    int64  `json:"done"` // bytes uploaded so far
	State   string `json:"state"`
	Message string `json:"message,omitempty"` // why it failed
	// Active is true until the send has an outcome.
	Active bool      `json:"active"`
	At     time.Time `json:"at"`
}

// Finished reports whether state is a final outcome.
func Finished(state string) bool {
	switch state {
	case StateDelivered, StateDeclined, StateExpired, StateCancelled, StateFailed:
		return true
	}
	return false
}

// request is one send to run.
type request struct {
	device api.HandoffDevice
	items  []Item
	// noWait ends the send once the files are uploaded.
	noWait bool
	update func(Transfer)
}

const (
	cancelTimeout = 10 * time.Second
	maxLostPolls  = 20 // consecutive status checks that failed
	maxWait       = 25 * time.Hour
	progressEvery = 100 * time.Millisecond
)

// progressReader reports the bytes handed to the connection.
type progressReader struct {
	r    io.Reader
	done func(int)
}

func (p progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.done(n)
	}
	return n, err
}

// run makes the offer, streams every file from disk, then waits for the other
// device's answer. Cancelling ctx at any point cancels the offer on the
// server too. The returned transfer is the final snapshot.
func (s *Sender) run(ctx context.Context, client *api.Client, req request) Transfer {
	tr := &tracker{update: req.update, t: Transfer{
		To: req.device.Name, Label: Describe(req.items), Size: TotalSize(req.items),
		State: StateUploading, Active: true, At: time.Now(),
	}}
	tr.set(func(*Transfer) {})

	files := make([]api.HandoffNewFile, len(req.items))
	for i, item := range req.items {
		files[i] = api.HandoffNewFile{Name: item.Name, Size: item.Size}
	}
	offer, err := client.HandoffCreate(ctx, req.device.ID, files)
	if err != nil {
		return tr.end(outcomeOf(ctx, err))
	}
	if len(offer.Files) != len(req.items) {
		s.cancelOffer(client, offer.ID)
		return tr.end(StateFailed, "NeoCloud answered in a way this version does not understand. Update the app and try again.")
	}
	if err := s.upload(ctx, client, offer, req.items, tr.progress); err != nil {
		s.cancelOffer(client, offer.ID) // an unfinished send must not linger on the other device
		return tr.end(outcomeOf(ctx, err))
	}
	tr.set(func(t *Transfer) { t.Done, t.State = t.Size, StateWaiting })
	if req.noWait {
		return tr.set(func(t *Transfer) { t.Active = false })
	}
	state, err := s.await(ctx, client, offer.ID, func(state string) {
		tr.set(func(t *Transfer) { t.State = state })
	})
	if err != nil {
		if ctx.Err() != nil {
			s.cancelOffer(client, offer.ID)
		}
		return tr.end(outcomeOf(ctx, err))
	}
	return tr.end(state, "")
}

// tracker keeps the snapshot of one send. The upload reports from the
// connection's goroutine, so every change goes through the lock.
type tracker struct {
	mu       gosync.Mutex
	t        Transfer
	update   func(Transfer)
	lastSent time.Time
}

func (tr *tracker) set(change func(*Transfer)) Transfer {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	change(&tr.t)
	if tr.update != nil {
		tr.update(tr.t)
	}
	return tr.t
}

// progress counts uploaded bytes, telling the listener at most ten times a second.
func (tr *tracker) progress(n int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.t.Done += int64(n)
	if tr.update != nil && time.Since(tr.lastSent) >= progressEvery {
		tr.lastSent = time.Now()
		tr.update(tr.t)
	}
}

func (tr *tracker) end(state, message string) Transfer {
	return tr.set(func(t *Transfer) { t.State, t.Message, t.Active, t.At = state, message, false, time.Now() })
}

func (s *Sender) upload(ctx context.Context, client *api.Client, offer *api.HandoffOffer, items []Item, progress func(int)) error {
	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.uploadFile(ctx, client, offer.ID, offer.Files[i].ID, item, progress); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sender) uploadFile(ctx context.Context, client *api.Client, offerID string, fileID int64, item Item, progress func(int)) error {
	file, err := os.Open(item.Path)
	if err != nil {
		return userErrorf("%s can't be read any more: %s", item.Name, reason(err))
	}
	defer file.Close()
	_, err = client.HandoffUpload(ctx, offerID, fileID, item.Size, progressReader{r: file, done: progress})
	if err != nil && ctx.Err() == nil {
		var apiErr *api.Error
		if !errors.As(err, &apiErr) {
			if info, statErr := os.Stat(item.Path); statErr == nil && info.Size() != item.Size {
				return userErrorf("%s changed while it was being sent. Try again.", item.Name)
			}
		}
	}
	return err
}

// await follows the send until it has an outcome. The event stream wakes it
// at once (Sender.Kick); polling covers a stream that is down.
func (s *Sender) await(ctx context.Context, client *api.Client, offerID string, onState func(string)) (string, error) {
	wake := s.watch(offerID)
	defer s.unwatch(offerID)
	ctx, stop := context.WithTimeout(ctx, maxWait)
	defer stop()
	lost := 0
	for {
		offer, err := client.HandoffOfferByID(ctx, offerID)
		switch {
		case err == nil:
			lost = 0
			if Finished(offer.Status) {
				return offer.Status, nil
			}
			if offer.Status == "accepted" {
				onState(StateReceiving)
			}
		case ctx.Err() != nil:
			return "", ctx.Err()
		case isRefusal(err):
			return "", err
		default:
			if lost++; lost >= maxLostPolls {
				return "", err
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-wake:
		case <-time.After(s.poll):
		}
	}
}

// isRefusal is an answer from the server that waiting longer won't change.
func isRefusal(err error) bool {
	var apiErr *api.Error
	return errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != 429
}

// cancelOffer asks the server to drop the send; it must work even when the
// send's own context is already cancelled.
func (s *Sender) cancelOffer(client *api.Client, offerID string) {
	ctx, stop := context.WithTimeout(context.Background(), cancelTimeout)
	defer stop()
	if err := client.HandoffCancel(ctx, offerID); err != nil {
		s.logf("send to device: cancelling %s: %v", offerID, err)
	}
}

// outcomeOf turns what ended a send into a state and a sentence.
func outcomeOf(ctx context.Context, err error) (string, string) {
	var user *UserError
	var apiErr *api.Error
	switch {
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return StateCancelled, ""
	case errors.As(err, &user):
		return StateFailed, user.Message
	case errors.As(err, &apiErr):
		return StateFailed, friendlySend(apiErr)
	case errors.Is(err, context.DeadlineExceeded):
		return StateExpired, ""
	}
	return StateFailed, "Could not reach NeoCloud. Check the connection and try again."
}

// friendlySend is the server's refusal in words for the person. The
// server's own messages are plain sentences for these codes.
func friendlySend(err *api.Error) string {
	switch err.Code {
	case "DEVICE_NOT_FOUND":
		return "That device is no longer available."
	case "DEVICE_NOT_RECEIVING":
		return "That device is not accepting files right now."
	case "SAME_DEVICE":
		return "Choose another device; files can't be sent to this one."
	case "TOO_MANY_OPEN_SENDS":
		return "Too many sends are waiting. Wait for them to arrive or cancel one."
	case "SEND_TOO_LARGE":
		return "That is more than can be sent at once."
	case "QUOTA_EXCEEDED":
		return "Your NeoCloud storage is full, so the files can't wait for the other device. Free up space and try again."
	case "OFFER_CLOSED":
		return "The send was cancelled before it finished."
	case "SIZE_MISMATCH":
		return "A file changed while it was being sent. Try again."
	}
	switch {
	case err.Status == 401:
		return "This computer is signed out of NeoCloud. Sign in again."
	case err.Status == 429:
		return "Too many sends in a short time. Try again in a few minutes."
	case err.Status >= 500:
		return "NeoCloud had a problem. Try again in a moment."
	}
	return fmt.Sprintf("NeoCloud could not send the files (%s).", err.Message)
}
